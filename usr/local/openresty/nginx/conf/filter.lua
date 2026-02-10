-- ============================================
-- Security Filter for cruorg_proxy
-- Purpose: Block malicious requests before routing to AEM
-- Context-aware: Only filters AEM-bound traffic, allows WordPress paths
-- Phase: 3 of 3-phase security enhancement
-- ============================================

local ngx = ngx
local ngx_var = ngx.var
local uri = string.lower(ngx_var.uri)
local user_agent = ngx.var.http_user_agent or ""
local method = ngx.var.request_method

-- ============================================
-- CONFIGURATION
-- ============================================
local BLOCK_RESPONSE = 403
local LOG_BLOCKED = true
local MONITORING_MODE = true  -- Set to false to enable blocking

-- ============================================
-- HELPER FUNCTIONS
-- ============================================
local function block_request(reason)
    if LOG_BLOCKED then
        ngx.log(ngx.WARN, "BLOCKED: ", reason, " | URI: ", uri, " | UA: ", user_agent, " | IP: ", ngx.var.remote_addr, " | Method: ", method)
    end

    if MONITORING_MODE then
        -- In monitoring mode, log but don't block
        ngx.log(ngx.WARN, "MONITORING_MODE: Would have blocked - ", reason)
        return
    end

    ngx.status = BLOCK_RESPONSE
    ngx.header.content_type = "text/plain"
    ngx.say("Access Denied")
    ngx.exit(BLOCK_RESPONSE)
end

-- ============================================
-- DETERMINE ROUTING TARGET
-- ============================================

-- Determine if this URI routes to WordPress or AEM
-- Uses the same logic as target.lua:
-- 1. Check nginx shared memory (ngx.shared.targets) for recent routing decisions
-- 2. If not in shared memory, query Redis table (cruorg:upstreams) which contains
--    WordPress path patterns defined in Terraform (redirects.tf)
-- 3. Redis is the source of truth; shared memory is a performance optimization

local is_wordpress_path = false

-- Step 1: Check nginx shared memory for recent routing decision
local ngx_targets = ngx.shared.targets
local cached_target, err = ngx_targets:get(uri)

if cached_target and cached_target ~= "DEFAULT_PROXY_TARGET" then
    -- Found in shared memory: This URI was recently routed to a WordPress upstream
    is_wordpress_path = true
else
    -- Step 2: Not in shared memory, query Redis table (source of truth)
    local redis = require "resty.redis"
    local red = redis:new()
    local upstreams_key = os.getenv('UPSTREAMS_KEY')

    red:set_timeout(1000) -- 1 second

    local ok, err = red:connect(os.getenv('STORAGE_REDIS_HOST'), os.getenv('STORAGE_REDIS_PORT'))
    if ok then
        red:select(os.getenv('STORAGE_REDIS_DB_INDEX'))

        -- Query the WordPress path patterns table
        local arr_upstreams, err = red:hgetall(upstreams_key)
        if arr_upstreams and not err then
            local upstreams = red:array_to_hash(arr_upstreams)

            -- Check if URI matches any WordPress path pattern
            for pattern, name in pairs(upstreams) do
                local match = ngx.re.match(uri, pattern, 'i')
                if match then
                    is_wordpress_path = true
                    break
                end
            end
        end

        red:set_keepalive(0, 100)
    else
        ngx.log(ngx.WARN, "Filter: failed to connect to redis: ", err)
        -- Fallback: If Redis is unavailable, check stale shared memory entries
        local stale_target, err = ngx_targets:get_stale(uri)
        if stale_target and stale_target ~= "DEFAULT_PROXY_TARGET" then
            is_wordpress_path = true
        end
        -- If we can't determine destination, assume AEM (safer to apply filters)
    end
end

-- ============================================
-- GLOBAL FILTERS (All Requests)
-- ============================================

-- Rule 1: Block missing or suspicious user agents
-- Exception: Health checks and monitoring
if not user_agent or user_agent == "" or user_agent == "-" then
    local is_health_check = uri == "/monitor.html"
    if not is_health_check then
        block_request("Missing User-Agent")
    end
end

-- Rule 2: Block known vulnerability scanners and penetration testing tools
-- These tools are used to probe for security vulnerabilities and have no legitimate
-- reason to access production services. Blocking them prevents:
-- - Vulnerability reconnaissance (attackers mapping our infrastructure)
-- - Exploitation attempts (automated attacks against discovered vulnerabilities)
-- - Resource consumption (scanners generate high request volumes)
-- - False security alerts (reducing noise in monitoring systems)
--
-- Note: Legitimate security scanning should use authorized IPs (WAF allowlist)
local scanner_patterns = {
    "nikto",        -- Web vulnerability scanner
    "nmap",         -- Network port scanner
    "sqlmap",       -- SQL injection scanner
    "masscan",      -- Fast port scanner
    "zgrab",        -- Internet-wide scanner
    "shodan",       -- Internet-connected device scanner
    "censys",       -- Internet asset scanner
    "nessus",       -- Vulnerability scanner
    "metasploit",   -- Penetration testing framework
    "burp",         -- Web security testing tool (Burp Suite)
    "acunetix",     -- Web vulnerability scanner
    "owasp",        -- OWASP ZAP scanner
}

local ua_lower = string.lower(user_agent)
for _, pattern in ipairs(scanner_patterns) do
    if string.find(ua_lower, pattern, 1, true) then
        block_request("Scanner detected: " .. pattern)
    end
end

-- ============================================
-- AEM-SPECIFIC FILTERS (Only for AEM-bound traffic)
-- ============================================
if not is_wordpress_path then

    -- Rule 3: Block PHP file extensions for AEM
    if ngx.re.match(uri, "\\.(php|phtml|php3|php4|php5|phps)($|\\?)", "ijo") then
        block_request("PHP file request to AEM")
    end

    -- Rule 4: Block WordPress admin paths for AEM
    if ngx.re.match(uri, "^/(wp%-login|wp%-admin)", "ijo") then
        block_request("WordPress admin path to AEM")
    end

    -- Rule 5: Block xmlrpc.php for AEM
    if ngx.re.match(uri, "xmlrpc\\.php", "ijo") then
        block_request("xmlrpc.php to AEM")
    end

    -- Rule 6: Block WordPress manifest files
    if ngx.re.match(uri, "wlwmanifest\\.xml", "ijo") then
        block_request("WordPress manifest to AEM")
    end

    -- Rule 7: Block .env files (credential harvesting)
    if ngx.re.match(uri, "/\\.env", "ijo") then
        block_request(".env file request")
    end

    -- Rule 8: Block backup and config files
    if ngx.re.match(uri, "\\.(bak|backup|old|save|tmp|config|conf)$", "ijo") then
        block_request("Backup/config file request to AEM")
    end

    -- Rule 9: Block known malicious PHP files
    local malicious_files = {
        "shell\\.php",
        "cmd\\.php",
        "system123\\.php",
        "config\\.php",
        "wp%-good\\.php",
        "install\\.php",
        "setup\\.php",
        "test\\.php",
        "info\\.php",
        "admin\\.php",
        "file\\.php",
    }

    for _, pattern in ipairs(malicious_files) do
        if ngx.re.match(uri, pattern, "ijo") then
            block_request("Malicious file: " .. pattern)
        end
    end

    -- Rule 10: Block suspicious HTTP methods for AEM
    local aem_allowed_methods = {
        GET = true,
        POST = true,
        HEAD = true,
        OPTIONS = true
    }

    if not aem_allowed_methods[method] then
        block_request("Suspicious HTTP method for AEM: " .. method)
    end

    -- Rule 11: Block requests with suspicious query parameters
    local query_string = ngx.var.query_string or ""
    if query_string ~= "" then
        local suspicious_params = {
            "xdebug_session_start",
            "eval%(",
            "base64_decode",
            "phpinfo",
            "shell_exec",
            "system%(",
            "passthru",
        }

        local qs_lower = string.lower(query_string)
        for _, param in ipairs(suspicious_params) do
            if string.find(qs_lower, param, 1, true) then
                block_request("Suspicious query parameter: " .. param)
            end
        end
    end

end

-- ============================================
-- WORDPRESS-SPECIFIC FILTERS
-- ============================================
if is_wordpress_path then

    -- Rule 12: Block PHP execution in uploads directory
    -- This is critical - even WordPress sites shouldn't execute PHP from uploads
    if ngx.re.match(uri, "/wp%-content/uploads/.*\\.php", "ijo") then
        block_request("PHP execution attempt in WordPress uploads")
    end

    -- Rule 13: Block suspicious WordPress plugin/theme access patterns
    if ngx.re.match(uri, "/wp%-content/(plugins|themes)/[^/]+/\\.\\./", "ijo") then
        block_request("Directory traversal in WordPress plugins/themes")
    end

end

-- ============================================
-- END FILTERING
-- ============================================
-- If we reach here, request is allowed to proceed

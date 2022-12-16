local resty_url = require 'resty.url'
local ngx_targets = ngx.shared.targets
local args = ngx.req.get_uri_args()
-- concat scheme, host and uri to produce url
local uri = string.lower(ngx.var.thescheme .. "://" .. ngx.var.host .. ngx.var.uri)
local target = nil
local err = nil

-- Before messing around with redis, see if we have a cached target
-- Passing a url arg of purge_target will force it to go to redis
if not args['purge_target'] then
    target, err = ngx_targets:get(uri)
    if target then
        ngx.var.target = os.getenv(target)
        -- specific upstreams require a different Host header
        if target == "DEFAULT_PROXY_TARGET" then
            -- AEM requires the Host header to be the origin domain
            -- https://experienceleague.adobe.com/docs/experience-manager-cloud-service/content/implementing/content-delivery/cdn.html?lang=en
            ngx.var.proxy_host = resty_url.parse(os.getenv('DEFAULT_PROXY_TARGET')).host
        end
        return
    end
end

local redis = require "resty.redis"
local red = redis:new()

red:set_timeout(1000) -- 1 second

local ok, err = red:connect(os.getenv('STORAGE_REDIS_HOST'), os.getenv('STORAGE_REDIS_PORT'))
if ok then
    -- use db number 3
    red:select(os.getenv('STORAGE_REDIS_DB_INDEX'))

    local arr_upstreams, err = red:hgetall('upstreams')
    if arr_upstreams and not err then
        local upstreams = red:array_to_hash(arr_upstreams)

        for pattern, name in pairs(upstreams) do
            -- If the uri matches this pattern, set the the named target
            local match = ngx.re.match(uri, pattern, 'i')
            if match then
                target = name
                break
            end
        end
    end

    red:set_keepalive(0, 100)
else
    ngx.log(ngx.ERR, "failed to connect to redis: ", err)
    -- If redis is down, look for a stale key
    target, err = ngx_targets:get_stale(uri)
end

-- If the key doesn't exist, default to DEFAULT_PROXY_TARGET
if (not target) or (target == ngx.null) then
    target = "DEFAULT_PROXY_TARGET"
end

-- Store target for 1 hour
local success, err, forcible = ngx_targets:set(uri, target, 3600)

ngx.var.target = os.getenv(target)

-- specific upstreams require a different Host header
if target == "DEFAULT_PROXY_TARGET" then
    -- AEM requires the Host header to be the origin domain
    -- https://experienceleague.adobe.com/docs/experience-manager-cloud-service/content/implementing/content-delivery/cdn.html?lang=en
    ngx.var.proxy_host = resty_url.parse(os.getenv('DEFAULT_PROXY_TARGET')).host
end

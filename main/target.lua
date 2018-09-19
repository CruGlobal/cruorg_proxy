local resty_url = require 'resty.url'
local ngx_targets = ngx.shared.targets
local args = ngx.req.get_uri_args()
-- concat scheme, host and uri to produce url
local uri = string.lower(ngx.var.scheme .. "://" .. ngx.var.host .. ngx.var.uri)
local target = nil

-- In order for the upstreams feature to be enabled, this key must be set.
local upstreams_key = os.getenv('UPSTREAMS_KEY')
if not upstreams_key then
    return
end

-- Before messing around with redis, see if we have a cached target
-- Passing a url arg of purge_target will force it to go to redis
if not args['purge_target'] then
    target, err = ngx_targets:get(uri)
    if target then
        ngx.var.target = os.getenv(target)
        -- specific upstreams require a different Host header
        if target == "SOB_ADDR" then
            ngx.var.proxy_host = resty_url.parse(ngx.var.target).host
        end
        return
    end
end

local redis = require "resty.redis"
local red = redis:new()

red:set_timeout(1000) -- 1 second

local ok, err = red:connect(os.getenv('REDIS_PORT_6379_TCP_ADDR'), 6379)
if ok then
    -- use db number 3
    red:select(3)

    local arr_upstreams, err = red:hgetall(upstreams_key)
    if arr_upstreams and not err then
        upstreams = red:array_to_hash(arr_upstreams)

        for pattern, name in pairs(upstreams) do
            -- If the uri matches this pattern, set the the named target
            match = ngx.re.match(uri, pattern, 'i')
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
success, err, forcible = ngx_targets:set(uri, target, 3600)

ngx.var.target = os.getenv(target)
-- specific upstreams require a different Host header
if target == "SOB_ADDR" then
    ngx.var.proxy_host = resty_url.parse(ngx.var.target).host
end

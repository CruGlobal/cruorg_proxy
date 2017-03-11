local redis = require "resty.redis"
local red = redis:new()
local uri = string.lower(ngx.var.uri)
local re = require 're'

red:set_timeout(1000) -- 1 second

local ok, err = red:connect(os.getenv('REDIS_PORT_6379_TCP_ADDR'), 6379)
if not ok then
    ngx.log(ngx.ERR, "failed to connect to redis: ", err)
    -- If redis is down, just continue on
    return
end

-- use db number 3
red:select(3)

local target, err = red:get("redirect:" .. uri)

if (not target) or (target == ngx.null) then
    -- If the key doesn't exist, start looping over the wildcard rules.
    local redirects, err = red:hgetall("redirects:regex")
    if err or not redirects then
      red:set_keepalive(0, 100)
      ngx.log(ngx.ERR, "error: ", err)
      return
    end

    arr_rewrite = red:array_to_hash(redirects)

    for pattern, sub in pairs(arr_rewrite) do
      -- If the uri matches this rule, redirect to the target uri
      if re.find(uri, pattern) then
        new_uri = re.gsub(uri, pattern, sub)
        red:set_keepalive(0, 100)

        return ngx.redirect(new_uri, 301)
      end
    end
    return
end

red:set_keepalive(0, 100)
return ngx.redirect(target, 301)

local redis = require "resty.redis"
local red = redis:new()

red:set_timeout(1000) -- 1 second

local ok, err = red:connect('redis', 6379)
if not ok then
    ngx.log(ngx.ERR, "failed to connect to redis: ", err)
    -- If redis is down, just continue on
    return
end

-- use db number 3
red:select(3)

local target, err = red:get("vanities:" .. string.lower(ngx.var.uri))

red:set_keepalive(10000, 100)

if (not target) or (target == ngx.null) then
    -- If the key doesn't exist, there's nothing to do.
    return
end

return ngx.redirect(target, 301)

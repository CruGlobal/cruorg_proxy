local redis = require "resty.redis"
local re = require 're'
local red = redis:new()

red:set_timeout(1000) -- 1 second

local ok, err = red:connect(os.getenv('REDIS_PORT_6379_TCP_ADDR'), 6379)
if ok then
  -- use db number 3
  red:select(3)

  local levels = {re.match(string.lower(ngx.var.uri), "{'/'[a-z0-9_-]+}*")}
  local level_count = #levels
  -- ngx.log(ngx.ERR, level_count)

  for i=1,level_count do
  --  ngx.log(ngx.ERR, i)
    local level = table.concat(levels)
    target, err = red:get("upstreams:" .. level)
  --  ngx.log(ngx.ERR, level)
  --  ngx.log(ngx.ERR, target)

    if target and (target ~= ngx.null) then
       ngx.var.target = os.getenv(target)
       return
    else
      if err ~= ngx.null then
        ngx.log(ngx.ERR, err)
      end
    end
    table.remove(levels)
  end

  red:set_keepalive(0, 100)
else
  ngx.log(ngx.ERR, "failed to connect to redis: ", err)
end

-- If the key doesn't exist, default to AEM
ngx.var.target = os.getenv("AEM_ADDR")


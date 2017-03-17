local ngx_targets = ngx.shared.targets
local args = ngx.req.get_uri_args()
local uri = string.lower(ngx.var.uri)
local target = nil

-- Before messing around with redis, see if we have a cached target
-- Passing a url arg of purge_target will force it to go to redis
if not args['purge_target'] then
  target, err = ngx_targets:get(uri)
  if target then
    ngx.log(ngx.ERR, target)
    ngx.var.target = os.getenv(target)
    return
  end
end

local redis = require "resty.redis"
local re = require 're'
local red = redis:new()

red:set_timeout(1000) -- 1 second

local ok, err = red:connect(os.getenv('REDIS_PORT_6379_TCP_ADDR'), 6379)
if ok then
  -- use db number 3
  red:select(3)

  -- To avoid the need to loop through a bunch of wildcard rules, the code
  -- below will attempt to mach a your uri at each level. E.g:
  -- If there's a rule in redis for /level1/level2 => WP_ADDR
  -- And your uri is /level1/level2/level3/level4.html
  -- This loop will check the following:
  -- /level1/level2/level3/level4 => miss
  -- /level1/level2/level3 => miss
  -- /level1/level2 => hit
  local levels = {re.match(uri, "{'/'[a-z0-9_-]+}*")}
  local level_count = #levels

  for i=1,level_count do
    local level = table.concat(levels)
    target, err = red:get("upstreams:" .. level)

    if target and (target ~= ngx.null) then
       break
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
  -- If redis is down, look for a stale key
  target, err = ngx_targets:get_stale(uri)
end

-- If the key doesn't exist, default to AEM
if (not target) or (target == ngx.null) then
  target = "AEM_ADDR"
end

-- Store target for 1 hour
success, err, forcible = ngx_targets:set(uri, target, 3600)

ngx.var.target = os.getenv(target)


local ngx_redirects = ngx.shared.redirects
local args = ngx.req.get_uri_args()
local uri = string.lower(ngx.var.uri)

-- Before messing around with redis, see if we have a cached redirect target
-- Passing a url arg of purge_vanity will force it to go to redis
if not args['purge_vanity'] then
  local target, err = ngx_redirects:get(uri)
  if target then
    -- We cache the fact that a url doesn't redirect
    if target == 'none' then
      return
    end

    return ngx.redirect(target, 301)
  end
end

-- Look for a redirect target in redis
local redis = require "resty.redis"
local red = redis:new()
local re = require 're'

-- Connect to redis
red:set_timeout(1000) -- 1 second=
local ok, err = red:connect(os.getenv('REDIS_PORT_6379_TCP_ADDR'), 6379)
if not ok then
    ngx.log(ngx.ERR, "failed to connect to redis: ", err)

    -- If redis is down, look for a stale key
    local target, err = ngx_redirects:get_stale(uri)
    if target then
      return ngx.redirect(target, 301)
    end
    return
end

-- use db number 3
red:select(3)

-- Look for exact (vanity) match in redis
local target, err = red:get("redirect:" .. uri)

-- If we didn't find an exact vanity match, look for a pattern match
if (not target) or (target == ngx.null) then
  local redirects, err = red:hgetall("redirects:regex")

  -- redirects key should always exist. If it doesn, just return
  if err or not redirects then
    red:set_keepalive(0, 100)
    ngx.log(ngx.ERR, "error: ", err)
    return
  end

  arr_rewrite = red:array_to_hash(redirects)

  for pattern, sub in pairs(arr_rewrite) do
    -- If the uri matches this rule, redirect to the target uri
    local match_index = re.find(uri, pattern)
    -- we only want to match the beginning of a URL
    if match_index == 1 then
      target = re.gsub(uri, pattern, sub)
      break
    end
  end
end
red:set_keepalive(0, 100)

-- If we have a match, store and redirect
if target and target ~= ngx.null then
  -- Cache target for 1 hour
  success, err, forcible = ngx_redirects:set(uri, target, 3600)
  return ngx.redirect(target, 301)
else
  success, err, forcible = ngx_redirects:set(uri, 'none', 3600)
end

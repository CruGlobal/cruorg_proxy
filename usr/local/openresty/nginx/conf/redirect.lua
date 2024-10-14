local ngx_redirects = ngx.shared.redirects
local args = ngx.req.get_uri_args()
local uri = string.lower(ngx.var.uri)

-- In order for the vanities feature to be enabled, this key must be set.
local vanities_key = os.getenv('VANITY_KEY')
if not vanities_key then
  return
end

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

-- Connect to redis
red:set_timeout(1000) -- 1 second=
local ok, err = red:connect(os.getenv("STORAGE_REDIS_HOST"), os.getenv("STORAGE_REDIS_PORT"))
if not ok then
    ngx.log(ngx.ERR, "failed to connect to redis: ", err)

    -- If redis is down, look for a stale key
    local target, err = ngx_redirects:get_stale(uri)
    if target then
      return ngx.redirect(target, 301)
    end
    return
end

-- use db index
red:select(os.getenv("STORAGE_REDIS_DB_INDEX"))

-- Look for exact (vanity) match in redis
local target, err = red:hget(vanities_key, uri)

-- If we didn't find an exact vanity match, look for a pattern match
if (not target) or (target == ngx.null) then
  -- In order for the rewrites feature to be enabled, this key must be set.
  local rewrites_key = os.getenv('REWRITES_KEY')
  if not rewrites_key then
    return
  end

  local redirects, err = red:hgetall(rewrites_key)

  -- redirects key should always exist. If it doesn't, just return
  if err or not redirects then
    red:set_keepalive(0, 100)
    ngx.log(ngx.ERR, "error: ", err)
    return
  end

  local arr_rewrite = red:array_to_hash(redirects)
  for pattern, sub in pairs(arr_rewrite) do
    -- If the uri matches this rule, redirect to the target uri
    local new_uri, index, err = ngx.re.gsub(ngx.var.uri, pattern, sub, "i")
    if index > 0 then
      target = new_uri
      break
    end
  end
end
red:set_keepalive(0, 100)

-- If we have a match, store and redirect
if target and target ~= ngx.null then
  -- Cache target for 1 hour
  local success, err, forcible = ngx_redirects:set(uri, target, 3600)
  return ngx.redirect(target, 301)
else
  local success, err, forcible = ngx_redirects:set(uri, 'none', 3600)
end

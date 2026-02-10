# Security Filter Implementation - Phase 3

## Overview
Proxy-layer Lua security filter that provides context-aware filtering of malicious requests before routing to AEM Cloud or WordPress VIP.

**Phase**: 3 of 3 (follows WAF rules in cru-terraform repo)

## Problem Statement
Log analysis revealed 6% of traffic (1,203/20,000 requests) is malicious. While WAF rules (Phase 1-2) provide broad protection, proxy-level filtering adds:
- **Context awareness**: Different rules for AEM vs WordPress paths
- **Fine-grained control**: Specific pattern matching based on routing
- **Defense in depth**: Second layer if WAF is bypassed
- **Performance**: Early rejection before expensive routing logic

## Architecture

### 3-Layer Defense Model

```
┌─────────────────────────────────────────────────────┐
│ Layer 1: AWS WAF (CloudFront + ALB)                │
│ - Broad attack pattern blocking                     │
│ - Rate limiting                                      │
│ - Managed rule sets (PHP, SQLi, XSS)               │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│ Layer 2: OpenResty Lua Filter (THIS IMPLEMENTATION) │
│ - Context-aware (AEM vs WordPress)                  │
│ - Pattern matching based on routing                 │
│ - Malicious file detection                          │
└──────────────────┬──────────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────────┐
│ Layer 3: Backend Systems                            │
│ - AEM Cloud (if routed by DEFAULT_PROXY_TARGET)    │
│ - WordPress VIP (if matched in cruorg:upstreams)    │
└─────────────────────────────────────────────────────┘
```

## Implementation Details

### Files Changed

#### 1. `usr/local/openresty/nginx/conf/filter.lua` (NEW)
**Purpose**: Main security filtering logic

**Key Features**:
- **Runtime Configuration**: Monitoring modes controlled via environment variables
- **Redis Integration**: Queries `cruorg:upstreams` Redis hash (same as `target.lua`)
- **Context Detection**: Identifies AEM vs WordPress paths dynamically from Redis
- **Shared Memory Optimization**: Checks nginx shared memory first, then Redis
- **13 Security Rules**: Organized by scope (global, AEM-only, WordPress-only)

**Environment Variables**:
- `SECURITY_FILTER_MONITORING_MODE`: Controls all rules (default: `"true"`)
  - Set to `"false"` to enable blocking for global and AEM-specific rules
  - Any other value (including unset) keeps monitoring mode active
- `SECURITY_FILTER_MONITORING_MODE_WORDPRESS`: Controls WordPress-specific rules (default: `"true"`)
  - Set to `"false"` to enable blocking for WordPress rules (Rules 12-13)
  - Independent from main MONITORING_MODE for granular rollout
  - Any other value (including unset) keeps monitoring mode active

**Configuration Logged on Startup**:
```
Security Filter Config: MONITORING_MODE=true, MONITORING_MODE_WORDPRESS=true
```

**WordPress Path Detection** (Dynamic from Redis):
- Queries Redis `cruorg:upstreams` hash from `redirects.tf`
- Uses same logic as `target.lua` for consistency
- Caches results in nginx shared memory (`ngx.shared.targets`)
- Falls back to stale cache if Redis unavailable
- **Single source of truth**: No duplicate path maintenance needed

#### 2. `usr/local/openresty/nginx/conf/conf.d/server.conf` (MODIFIED)
**Change**: Added `access_by_lua_file /usr/local/openresty/nginx/conf/filter.lua;` at line 23

**Execution Order**:
1. `filter.lua` - Security filtering
2. `redirect.lua` - Vanity URL redirects
3. `target.lua` - Routing logic

**Why This Order**:
- Filter runs FIRST to block attacks before any routing
- Runs in `access_by_lua` phase (before rewrite phase)
- Can block request with 403 before expensive Redis lookups

#### 3. `usr/local/openresty/nginx/conf/nginx.conf` (MODIFIED)
**Change**: Added separate `blocked` log format at line 42-45

**Log Format**:
```
$remote_addr - [$time_local] "$request" $status "$http_user_agent" "$http_referer" "$http_cloudfront_viewer_country" [BLOCKED]
```

## Security Rules

### Global Rules (All Requests)

| Rule | Pattern | Example | Impact |
|------|---------|---------|--------|
| 1. Missing User-Agent | Empty UA header | `User-Agent: -` | Block (except /monitor.html) |
| 2. Scanner Detection | Known scanner tools | `nikto`, `sqlmap`, `shodan` | Block |

### AEM-Only Rules (Non-WordPress Paths)

| Rule | Pattern | Example | Impact |
|------|---------|---------|--------|
| 3. PHP Files | `*.php`, `*.phtml` | `/shell.php` | Block |
| 4. WP Admin | `/wp-login.php`, `/wp-admin/` | `/wp-admin/` | Block |
| 5. XML-RPC | `xmlrpc.php` | `/xmlrpc.php` | Block |
| 6. WP Manifest | `wlwmanifest.xml` | `/wp-includes/wlwmanifest.xml` | Block |
| 7. .env Files | `/.env*` | `/.env.production` | Block |
| 8. Backup Files | `*.bak`, `*.old`, `*.tmp` | `/config.bak` | Block |
| 9. Malicious PHP | Known shells | `/shell.php`, `/cmd.php` | Block |
| 10. HTTP Methods | DELETE, PUT, TRACE | `DELETE /content` | Block |
| 11. Suspicious Params | Debug/exploit params | `?xdebug_session_start` | Block |

### WordPress-Only Rules (WordPress Paths)

| Rule | Pattern | Example | Impact |
|------|---------|---------|--------|
| 12. PHP in Uploads | `/wp-content/uploads/*.php` | `/uploads/shell.php` | Block |
| 13. Directory Traversal | Plugin/theme path traversal | `/plugins/foo/../../../` | Block |

## Configuration

### Monitoring Mode (Phase 3a - Testing)

**Current State**: `MONITORING_MODE = true` in `filter.lua` line 15

**Behavior**:
- ✅ All rules are evaluated
- ✅ Violations are logged to error.log
- ❌ Requests are NOT blocked
- ✅ Safe for production deployment

**Log Output**:
```
2026/02/10 15:30:45 [warn] BLOCKED: PHP file request to AEM | URI: /shell.php | UA: curl/7.68.0 | IP: 203.0.113.42 | Method: GET
2026/02/10 15:30:45 [warn] MONITORING_MODE: Would have blocked - PHP file request to AEM
```

### Blocking Mode (Phase 3b - After Testing)

**To Enable Blocking**:
1. Edit `filter.lua` line 15
2. Change: `local MONITORING_MODE = true`
3. To: `local MONITORING_MODE = false`
4. Rebuild container and deploy

**Behavior**:
- ✅ All rules enforced
- ✅ Violations logged
- ✅ Requests blocked with 403 status
- ✅ Response body: "Access Denied"

## Testing Plan

### Phase 3a: Monitoring Mode (Week 3)

**Objectives**:
- Validate filter logic doesn't break legitimate traffic
- Confirm WordPress path detection works correctly
- Verify AEM-only rules don't affect WordPress
- Collect baseline data on blocked requests

**Checklist**:
- [ ] Deploy to **stage** with MONITORING_MODE=true
- [ ] Test legitimate AEM paths: `/content/*`, `/etc/designs/*`
- [ ] Test legitimate WordPress paths: `/communities/*`, `/mycampus/*`
- [ ] Test WordPress admin: Should log but allow for WP sites
- [ ] Test malicious requests to AEM: Should log violations
- [ ] Review error.log for 24 hours
- [ ] Confirm no false positives

**Test Cases**:

```bash
# Should ALLOW (AEM content)
curl -H "User-Agent: Mozilla/5.0" https://stage.cru.org/content/cru/us/en.html

# Should ALLOW (WordPress site)
curl -H "User-Agent: Mozilla/5.0" https://stage.cru.org/communities/campus-us-test

# Should ALLOW (WordPress admin on WP site)
curl -H "User-Agent: Mozilla/5.0" https://stage.cru.org/communities/test/wp-admin/

# Should LOG but ALLOW in monitoring mode (attack to AEM)
curl -H "User-Agent: Mozilla/5.0" https://stage.cru.org/shell.php

# Should LOG but ALLOW in monitoring mode (.env harvesting)
curl -H "User-Agent: Mozilla/5.0" https://stage.cru.org/.env

# Should LOG but ALLOW in monitoring mode (xmlrpc to AEM)
curl -H "User-Agent: Mozilla/5.0" https://stage.cru.org/xmlrpc.php
```

### Phase 3b: Blocking Mode (Week 4)

**Prerequisites**:
- ✅ 24+ hours of monitoring mode in stage
- ✅ Zero false positives confirmed
- ✅ WordPress site functionality verified
- ✅ AEM content delivery verified

**Rollout**:
1. **Stage**: Enable blocking mode (MONITORING_MODE=false)
2. **Monitor**: 48 hours in stage
3. **Production**: Enable blocking mode
4. **Monitor**: Ongoing

## Monitoring

### Logs to Monitor

**Error Log** (`logs/error.log`):
```bash
# Watch for blocked requests in real-time
docker exec cruorg-proxy tail -f /usr/local/openresty/nginx/logs/error.log | grep BLOCKED

# Count violations by type
docker exec cruorg-proxy grep "BLOCKED:" /usr/local/openresty/nginx/logs/error.log | \
  awk -F'BLOCKED: ' '{print $2}' | awk -F' |' '{print $1}' | sort | uniq -c
```

**Access Log** (`logs/access.log`):
```bash
# Check for 403 responses (when blocking enabled)
docker exec cruorg-proxy grep " 403 " /usr/local/openresty/nginx/logs/access.log

# Count 403s by URI
docker exec cruorg-proxy grep " 403 " /usr/local/openresty/nginx/logs/access.log | \
  awk '{print $7}' | sort | uniq -c
```

### Metrics to Track

| Metric | Source | Target |
|--------|--------|--------|
| Total requests | access.log | Baseline |
| Blocked attempts | error.log (BLOCKED) | ~1,200 per 20k (6%) |
| 403 responses | access.log | Match blocked attempts |
| False positives | Manual review | 0 |
| WordPress functionality | Testing | 100% working |
| AEM content delivery | Testing | 100% working |

## WordPress Path Management

The filter **dynamically queries Redis** to determine WordPress paths - no hardcoded patterns!

**How It Works**:
1. Checks nginx shared memory cache first (`ngx.shared.targets`)
2. If cache miss, queries Redis `cruorg:upstreams` hash
3. Compares URI against all patterns in Redis
4. If match found → WordPress path
5. If no match → AEM path

**Benefits**:
- ✅ **Single source of truth**: `redirects.tf` is the only place to manage paths
- ✅ **Automatic updates**: Changes to `redirects.tf` automatically apply to filter
- ✅ **Consistency**: Same logic as `target.lua` routing
- ✅ **No maintenance**: No need to sync hardcoded lists
- ✅ **Cache performance**: Leverages existing nginx shared memory cache

**Example WordPress Paths** (from Redis):
- Communities & Campus: `/communities/*`, `/mycampus/*`, `/gotothecampus/*`
- Events: `/cru19`, `/cru22`, `/cru25`, `/cru19kids`, `/cru22kids`
- Ministries: `/epicmovement`, `/faculty`, `/diaspora`, `/rotunda`
- Systems: `/apps`, `/brand`, `/sso/*`, `/.well-known/*`

*Full list managed in `cru-terraform/.../redirects.tf` Redis hash*

## Expected Impact

### Security Improvements

**Attack Mitigation**:
- ✅ 100% block of PHP requests to AEM (~1,127 attacks/20k requests)
- ✅ 100% block of .env harvesting (~14 attacks/20k requests)
- ✅ 100% block of xmlrpc.php to AEM (~50 attacks/20k requests)
- ✅ 100% block of malicious PHP shells (4+ attacks/20k requests)

**Total**: ~1,200 malicious requests blocked per 20,000 (6% reduction)

### Performance Impact

**Positive**:
- Earlier rejection = less CPU on routing logic
- No Redis lookups for blocked requests
- Reduced backend load (AEM/WordPress)

**Negligible**:
- Lua execution time: <1ms per request
- Pattern matching is highly optimized (compiled regex)
- No external dependencies or network calls

## Rollback Plan

### Emergency Rollback (Disable Filter)

**Option 1: Quick Disable**
```bash
# Comment out filter line in server.conf
docker exec cruorg-proxy sed -i 's/access_by_lua_file.*filter.lua/# &/' /usr/local/openresty/nginx/conf/conf.d/server.conf
docker exec cruorg-proxy nginx -s reload
```

**Option 2: Monitoring Mode**
```bash
# Change to monitoring mode (logs only, no blocking)
docker exec cruorg-proxy sed -i 's/MONITORING_MODE = false/MONITORING_MODE = true/' /usr/local/openresty/nginx/conf/filter.lua
docker exec cruorg-proxy nginx -s reload
```

**Option 3: Git Revert**
```bash
# Revert to previous version
git revert HEAD
# Rebuild and deploy container
```

## Integration with Phases 1-2

### WAF Rules (Phase 1-2) vs Lua Filter (Phase 3)

| Aspect | WAF Rules | Lua Filter |
|--------|-----------|------------|
| **Scope** | All requests at edge | All requests at proxy |
| **Context** | No routing awareness | Knows AEM vs WordPress |
| **Performance** | Blocks before ALB | Blocks before backend |
| **Cost** | $1/rule/month + $0.60/1M | No additional cost |
| **Flexibility** | Terraform changes | Code changes |
| **Best For** | Broad attack patterns | Context-specific rules |

**Why Both?**:
- WAF catches attacks at edge (cheaper, faster)
- Lua filter catches attacks that bypass WAF
- Lua filter has routing context WAF doesn't have
- Defense in depth: Two independent layers

### Combined Effectiveness

**Example Attack Flow**:
```
1. Attacker sends: GET /.env.production
   ├─> WAF: Blocks (BlockEnvFileHarvesting rule)
   └─> Never reaches proxy ✅

2. Attacker sends: GET /shell.php
   ├─> WAF: Blocks (BlockMaliciousPHPFiles rule)
   └─> Never reaches proxy ✅

3. Attacker sends: GET /wp-admin/ (to AEM path)
   ├─> WAF: Counts (BlockWPLoginAttempts in count mode)
   ├─> Reaches proxy
   └─> Lua Filter: Blocks (Rule 4: WP Admin to AEM) ✅

4. User sends: GET /communities/campus/wp-admin/ (to WordPress)
   ├─> WAF: Counts (sees wp-admin)
   ├─> Reaches proxy
   ├─> Lua Filter: Allows (WordPress path detected) ✅
   └─> Routes to WordPress VIP ✅
```

## Success Criteria

### Phase 3a (Monitoring Mode)
- [x] Filter deployed to stage
- [ ] 24+ hours of monitoring logs
- [ ] Zero false positives on legitimate traffic
- [ ] WordPress sites fully functional
- [ ] AEM content delivery unaffected
- [ ] Violations logged match expectations (~6%)

### Phase 3b (Blocking Mode)
- [ ] Successfully enabled in stage
- [ ] 48+ hours of blocking in stage
- [ ] Zero impact on legitimate traffic
- [ ] Successfully enabled in production
- [ ] 95%+ reduction in malicious requests to AEM
- [ ] Cost savings from reduced backend load

## References

**Related Changes**:
- Phase 1-2: WAF rules in `cru-terraform/applications/cruorg_proxy/{stage,prod}/webacl.tf`
- Log analysis: `cruorg_proxy/extract-2026-02-10T15_19_45.620Z.csv`
- WordPress paths: `cru-terraform/applications/cruorg_proxy/prod/redirects.tf`

**Documentation**:
- WAF docs: `cru-terraform/applications/cruorg_proxy/{stage,prod}/WAF_SECURITY_ENHANCEMENTS.md`
- OpenResty docs: https://github.com/openresty/lua-nginx-module
- Lua patterns: https://www.lua.org/manual/5.1/manual.html#5.4.1

## Maintenance

### Updating WordPress Paths

When new WordPress sites are added to `redirects.tf`:

1. Add pattern to `filter.lua` lines 60-112
2. Test in stage with monitoring mode
3. Deploy to production

### Updating Security Rules

To add new blocking patterns:

1. Add to appropriate section in `filter.lua`
2. Test in monitoring mode first
3. Document in this file
4. Deploy gradually (stage → prod)

## Questions / Issues

Contact: DevOps team
Date: 2026-02-10
Phase: 3 of 3

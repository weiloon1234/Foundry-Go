-- ARGV[1:2] describe the private versioned metadata wire envelope. tag_retention
-- (milliseconds) is prepended by the Go adapter as a constant.
local tag_prefix, version_bytes = ARGV[1], tonumber(ARGV[2])
-- Missing metadata (0) receives a fresh random version, equivalent to an
-- invalidation. Corrupt metadata (-1) is an error and is never overwritten.
local function read_tag(key)
    local kind = redis.call('TYPE', key).ok
    if kind == 'none' then return false, 0 end
    if kind ~= 'string' or redis.call('STRLEN', key) ~= #tag_prefix + version_bytes then
        return false, -1
    end
    local stored = redis.call('GET', key)
    if string.sub(stored, 1, #tag_prefix) ~= tag_prefix then return false, -1 end
    local version = string.sub(stored, #tag_prefix + 1)
    if version == string.rep('\0', version_bytes) then return false, -1 end
    return version, 1
end
-- Metadata expires after tag_retention without use, and never before a finite
-- tagged entry written under it. Expired metadata gets a fresh version, so an
-- idle expiry only turns old entries into misses; it never resurrects them.
local function keep_tag(key, needed)
    local ttl = redis.call('PTTL', key)
    if ttl == -1 or (ttl >= 0 and ttl < needed) then
        redis.call('PEXPIRE', key, math.max(needed, tag_retention))
    end
end
-- Redis 7+ checks permissions without executing a mutation. Scripts do not offer
-- rollback after a later runtime error, so multi-command writes preflight first.
if type(redis.acl_check_cmd) ~= 'function' then return {-3} end
local function can_keep_tags(first, last)
    for i = first, last do
        if not redis.acl_check_cmd('PEXPIRE', KEYS[i], '1') then return false end
    end
    return true
end

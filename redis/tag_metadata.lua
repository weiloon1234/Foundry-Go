-- ARGV[1:2] describe the private versioned metadata wire envelope.
local tag_prefix, version_bytes = ARGV[1], tonumber(ARGV[2])
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
-- Redis 7+ checks permissions without executing a mutation. Scripts do not offer
-- rollback after a later runtime error, so multi-command writes preflight first.
if type(redis.acl_check_cmd) ~= 'function' then return {-3} end

-- KEYS[1] is the stable payload address, followed by canonical metadata keys.
-- ARGV: wire prefix, version bytes, operation, payload bound, data, TTL ms,
-- fingerprint, then the expected version of each metadata key.
local key, op, bound = KEYS[1], ARGV[3], tonumber(ARGV[4])
for i = 2, #KEYS do
    local version, status = read_tag(KEYS[i])
    if status == -1 then return {-1} end
    if status == 0 or version ~= ARGV[i+6] then return {-2} end
end
local exists, matches, status = read_tagged_entry(key, ARGV[7], bound)
if status == -1 then return {-1} end
if op == 'exists' or op == 'expire' then
    if not matches then
        if exists then redis.call('DEL', key) end
        return {0}
    end
    if op == 'expire' then redis.call(unpack(entry_expiry(key, ARGV[6]))) end
    return {1}
end
if op == 'get' then
    if matches then return {1, redis.call('HGET', key, 'value')} end
    if exists then redis.call('DEL', key) end
    return {0}
end
if op == 'forget' then
    if exists then redis.call('DEL', key) end
    return {matches and 1 or 0}
end
if op == 'add' and matches then return {0} end
if op == 'increment' and matches then
    local old = redis.call('HGET', key, 'value')
    if not canonical_integer(old) then return {-1} end
    local result = redis.pcall('HINCRBY', key, 'value', ARGV[5])
    local failure = integer_error(result)
    if failure then return failure end
    return {1, redis.call('HGET', key, 'value')}
end
local expiry = entry_expiry(key, ARGV[6])
local write = {'HSET', key, 'fingerprint', ARGV[7], 'value', ARGV[5]}
if not redis.acl_check_cmd(unpack(write)) or not redis.acl_check_cmd(unpack(expiry)) then
    return redis.error_reply('NOPERM Foundry tagged cache write is not permitted')
end
redis.call(unpack(write))
redis.call(unpack(expiry))
if op == 'increment' then return {1, ARGV[5]} end
return {1}

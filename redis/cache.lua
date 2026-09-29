-- One bounded key per operation; values never pass through Lua number arithmetic.
local key, op = KEYS[1], ARGV[1]
local bound = tonumber(ARGV[2])
local exists, usable = read_plain_entry(key, bound)
if op == 'exists' then return {usable and 1 or 0} end
if op == 'expire' then
    if not usable then return {0} end
    redis.call(unpack(entry_expiry(key, ARGV[4])))
    return {1}
end
if op == 'get' then
    if not usable then return {0} end
    return {1, redis.call('GET', key)}
end
-- An unusable entry is logically absent but is still removed.
if op == 'forget' then
    if exists then redis.call('DEL', key) end
    return {usable and 1 or 0}
end
if op == 'add' and usable then return {0} end
if op == 'increment' and usable then
    local old = redis.call('GET', key)
    if not canonical_integer(old) then return {-1} end
    -- INCRBY rejects non-integers/overflow without changing the existing value.
    local result = redis.pcall('INCRBY', key, ARGV[3])
    local failure = integer_error(result)
    if failure then return failure end
    return {1, redis.call('GET', key)}
end
-- SET replaces an unusable entry of any type.
if ARGV[4] == '0' then
    redis.call('SET', key, ARGV[3])
else
    redis.call('SET', key, ARGV[3], 'PX', ARGV[4])
end
if op == 'increment' then return {1, ARGV[3]} end
return {1}

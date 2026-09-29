-- KEYS[1] is the stable payload address, followed by canonical metadata keys.
-- ARGV: wire prefix, version bytes, operation, payload bound, data, TTL ms,
-- snapshot fingerprint, then the expected version of each metadata key.
-- An empty fingerprint resolves the current snapshot instead: each following
-- argument is then a fresh version for missing metadata (as in ResolveTags),
-- so metadata resolution and the operation cost one atomic round trip.
local key, op, bound = KEYS[1], ARGV[3], tonumber(ARGV[4])
local fingerprint, resolve = ARGV[7], ARGV[7] == ''
local versions, updates = {}, {}
for i = 2, #KEYS do
    local version, status = read_tag(KEYS[i])
    if status == -1 then return {-1} end
    if resolve then
        if status == 0 then
            version = ARGV[i+6]
            updates[#updates+1] = KEYS[i]
            updates[#updates+1] = tag_prefix .. version
        end
        versions[#versions+1] = version
    elseif status == 0 or version ~= ARGV[i+6] then
        return {-2}
    end
end
if not can_keep_tags(2, #KEYS) then
    return redis.error_reply('NOPERM Foundry cache tag metadata expiry is not permitted')
end
if resolve then
    fingerprint = table.concat(versions)
    if #updates > 0 then redis.call('MSET', unpack(updates)) end
end
-- Metadata must outlive the finite entry TTL this operation sets or retains.
local needed = tag_retention / 2
if ARGV[6] ~= '0' then needed = math.max(needed, tonumber(ARGV[6])) end
local exists, matches, obsolete = read_tagged_entry(key, fingerprint, bound)
if op == 'exists' or op == 'expire' or op == 'get' or op == 'forget' then
    for i = 2, #KEYS do keep_tag(KEYS[i], needed) end
end
if op == 'exists' or op == 'expire' then
    if not matches then
        -- Reclaim only obsolete or corrupt storage; a current payload over this
        -- caller's bound is a miss another process may still read.
        if obsolete then redis.call('DEL', key) end
        return {0}
    end
    if op == 'expire' then redis.call(unpack(entry_expiry(key, ARGV[6]))) end
    return {1}
end
if op == 'get' then
    if matches then return {1, redis.call('HGET', key, 'value')} end
    if obsolete then redis.call('DEL', key) end
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
    for i = 2, #KEYS do keep_tag(KEYS[i], needed) end
    return {1, redis.call('HGET', key, 'value')}
end
local expiry = entry_expiry(key, ARGV[6])
local write = {'HSET', key, 'fingerprint', fingerprint, 'value', ARGV[5]}
if not redis.acl_check_cmd(unpack(write)) or not redis.acl_check_cmd(unpack(expiry)) or
   (exists and not redis.acl_check_cmd('DEL', key)) then
    return redis.error_reply('NOPERM Foundry tagged cache write is not permitted')
end
-- Stale, unusable or wrong-type entries are replaced as a whole.
if exists then redis.call('DEL', key) end
redis.call(unpack(write))
redis.call(unpack(expiry))
for i = 2, #KEYS do keep_tag(KEYS[i], needed) end
if op == 'increment' then return {1, ARGV[5]} end
return {1}

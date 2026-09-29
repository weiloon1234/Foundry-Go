-- Resolves metadata and reads one tagged entry atomically (one round trip).
-- KEYS[1] is the stable payload address, followed by canonical metadata keys.
-- ARGV: wire prefix, version bytes, payload bound, payload flag, then one fresh
-- version per metadata key for missing metadata. Corrupt metadata fails first.
local key, bound, payload = KEYS[1], tonumber(ARGV[3]), ARGV[4] == '1'
if not can_keep_tags(2, #KEYS) then
    return redis.error_reply('NOPERM Foundry cache tag metadata expiry is not permitted')
end
local updates, result, versions = {}, {1}, {}
for i = 2, #KEYS do
    local version, status = read_tag(KEYS[i])
    if status == -1 then return {-1} end
    if status == 0 then
        version = ARGV[i+3]
        updates[#updates+1] = KEYS[i]
        updates[#updates+1] = tag_prefix .. version
    end
    versions[#versions+1] = version
    result[#result+1] = version
end
if #updates > 0 then redis.call('MSET', unpack(updates)) end
for i = 2, #KEYS do keep_tag(KEYS[i], tag_retention / 2) end
local _, matches, obsolete = read_tagged_entry(key, table.concat(versions), bound)
if matches then
    result[#result+1] = 1
    if payload then result[#result+1] = redis.call('HGET', key, 'value') end
elseif obsolete then
    -- Obsolete or corrupt payloads are reclaimed on read; a current payload
    -- over this reader's bound is only a miss.
    redis.call('DEL', key)
end
return result

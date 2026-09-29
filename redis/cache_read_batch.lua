-- KEYS: canonical payload addresses, then one shared metadata snapshot.
-- ARGV: metadata prefix, version bytes, payload bound, data count, fingerprint,
-- expected metadata versions. Replies {1, value-or-0 per payload address}.
local bound, count = tonumber(ARGV[3]), tonumber(ARGV[4])
local tagged = #KEYS > count
if tagged then
    for i = count+1, #KEYS do
        local version, status = read_tag(KEYS[i])
        if status == -1 then return {-1} end
        if status == 0 or version ~= ARGV[5+i-count] then return {-2} end
    end
    if not can_keep_tags(count+1, #KEYS) then
        return redis.error_reply('NOPERM Foundry cache tag metadata expiry is not permitted')
    end
    for i = count+1, #KEYS do keep_tag(KEYS[i], tag_retention / 2) end
end
local result = {1}
for i = 1, count do
    local exists, matches, obsolete
    if tagged then
        exists, matches, obsolete = read_tagged_entry(KEYS[i], ARGV[5], bound)
    else
        exists, matches = read_plain_entry(KEYS[i], bound)
    end
    if matches then
        if tagged then
            result[#result+1] = redis.call('HGET', KEYS[i], 'value')
        else
            result[#result+1] = redis.call('GET', KEYS[i])
        end
    else
        -- Obsolete or corrupt tagged payloads are reclaimed on read; a current
        -- payload over this reader's bound is only a miss.
        if obsolete then redis.call('DEL', KEYS[i]) end
        result[#result+1] = 0
    end
end
return result

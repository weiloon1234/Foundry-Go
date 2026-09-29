-- KEYS: canonical payload addresses, then one shared metadata snapshot.
-- ARGV: metadata prefix, version bytes, payload bound, data count, fingerprint,
-- expected metadata versions. All input dimensions are bounded by the Go adapter.
local bound, count = tonumber(ARGV[3]), tonumber(ARGV[4])
local tagged = #KEYS > count
if tagged then
    for i = count+1, #KEYS do
        local version, status = read_tag(KEYS[i])
        if status == -1 then return {-1} end
        if status == 0 or version ~= ARGV[5+i-count] then return {-2} end
    end
end
local selected, live = {}, 0
for i = 1, count do
    local exists, matches
    if tagged then
        exists, matches = read_tagged_entry(KEYS[i], ARGV[5], bound)
    else
        exists, matches = read_plain_entry(KEYS[i], bound)
    end
    -- Unusable entries are removed and do not count as live.
    if exists then selected[#selected+1] = KEYS[i] end
    if matches then live = live+1 end
end
-- The only mutating command is one exact DEL, after every entry was inspected.
if #selected > 0 then redis.call('DEL', unpack(selected)) end
return {1, live}

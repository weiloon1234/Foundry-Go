-- Every key is validated before one atomic MSET publishes the selected changes.
local replace = ARGV[3] == '1'
local updates, result = {}, {1}
if not can_keep_tags(1, #KEYS) then
    return redis.error_reply('NOPERM Foundry cache tag metadata expiry is not permitted')
end
for i, key in ipairs(KEYS) do
    local version, status = read_tag(key)
    if status == -1 then return {-1} end
    if replace or status == 0 then
        version = ARGV[i+3]
        updates[#updates+1] = key
        updates[#updates+1] = tag_prefix .. version
    end
    result[#result+1] = version
end
if #updates > 0 then redis.call('MSET', unpack(updates)) end
for _, key in ipairs(KEYS) do keep_tag(key, tag_retention / 2) end
return result

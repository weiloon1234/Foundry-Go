-- Shared, non-mutating cache envelope checks. Payload bytes are never returned
-- for existence, expiry or batch deletion. Obsolete tagged payloads are not read.
local function read_plain_entry(key, bound)
    local kind = redis.call('TYPE', key).ok
    if kind ~= 'none' and kind ~= 'string' then return false, -1 end
    local exists = kind ~= 'none'
    if exists and redis.call('STRLEN', key) > bound then return false, -1 end
    return exists, 1
end
local function read_tagged_entry(key, fingerprint, bound)
    local kind = redis.call('TYPE', key).ok
    local exists, matches = kind ~= 'none', false
    if exists then
        if kind ~= 'hash' or redis.call('HLEN', key) ~= 2 or
           redis.call('HEXISTS', key, 'value') ~= 1 or
           redis.call('HSTRLEN', key, 'fingerprint') ~= #fingerprint then return false, false, -1 end
        matches = redis.call('HGET', key, 'fingerprint') == fingerprint
        if matches and redis.call('HSTRLEN', key, 'value') > bound then return false, false, -1 end
    end
    return exists, matches, 1
end

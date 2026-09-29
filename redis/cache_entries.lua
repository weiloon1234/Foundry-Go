-- Shared, non-mutating cache envelope checks. Payload bytes are never returned
-- for existence, expiry or batch deletion. Obsolete tagged payloads are not read.
-- An entry of the wrong type or shape, or larger than the current bound, exists
-- but is unusable: reads treat it as a miss, and writes may replace or remove it.
local function read_plain_entry(key, bound)
    local kind = redis.call('TYPE', key).ok
    if kind == 'none' then return false, false end
    if kind ~= 'string' or redis.call('STRLEN', key) > bound then return true, false end
    return true, true
end
-- matches means the stored snapshot equals fingerprint and the payload is usable.
-- obsolete means the entry is corrupt (wrong type or shape) or belongs to another
-- snapshot, so reads may reclaim it. A current payload that only exceeds this
-- caller's bound is neither: a process with a larger bound may still read it.
local function read_tagged_entry(key, fingerprint, bound)
    local kind = redis.call('TYPE', key).ok
    if kind == 'none' then return false, false, false end
    if kind ~= 'hash' or redis.call('HLEN', key) ~= 2 or
       redis.call('HEXISTS', key, 'value') ~= 1 or
       redis.call('HSTRLEN', key, 'fingerprint') ~= #fingerprint or
       redis.call('HGET', key, 'fingerprint') ~= fingerprint then return true, false, true end
    if redis.call('HSTRLEN', key, 'value') > bound then return true, false, false end
    return true, true, false
end

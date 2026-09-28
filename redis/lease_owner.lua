-- Shared bounded lease metadata reader. A live owner always has finite expiry.
local function read_lease_owner(key, size)
    local kind = redis.call('TYPE', key).ok
    if kind == 'none' then return nil, 0 end
    if kind ~= 'string' or redis.call('STRLEN', key) ~= size then return nil, -1 end
    local current = redis.call('GET', key)
    if current == string.rep(string.char(0), size) or redis.call('PTTL', key) < 0 then
        return nil, -1
    end
    return current, 1
end
local function require_fill_lease(key, owner, size)
    local current, status = read_lease_owner(key, size)
    if status == -1 then return {-1} end
    if status == 0 or current ~= owner then return {-4} end
end

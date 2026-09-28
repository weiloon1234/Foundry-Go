-- One bounded metadata value and one SET with absolute expiry; no partial
-- counter/TTL mutation can survive an error in a later command.
local capacity, window, cost = tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3])
local max_bytes, max_capacity, max_window, max_time = tonumber(ARGV[4]), tonumber(ARGV[5]), tonumber(ARGV[6]), tonumber(ARGV[7])
local function integer(text, maximum)
    if not text or not string.match(text, '^%d+$') or (#text > 1 and string.sub(text, 1, 1) == '0') then return nil end
    local value = tonumber(text)
    if not value or value > maximum or value % 1 ~= 0 then return nil end
    return value
end
local kind = redis.call('TYPE', KEYS[1]).ok
local old_capacity, old_window, old_end, used
if kind ~= 'none' then
    if kind ~= 'string' or redis.call('STRLEN', KEYS[1]) > max_bytes then return {-1} end
    local a,b,c,d = string.match(redis.call('GET', KEYS[1]), '^1:(%d+):(%d+):(%d+):(%d+)$')
    old_capacity, old_window = integer(a, max_capacity), integer(b, max_window)
    old_end, used = integer(c, max_time), integer(d, max_capacity)
    if not old_capacity or old_capacity == 0 or not old_window or old_window == 0 or not old_end or
       old_end < old_window or old_end % old_window ~= 0 or not used or used == 0 or used > old_capacity or
       redis.call('PEXPIRETIME', KEYS[1]) ~= old_end then return {-1} end
end
local now_parts = redis.call('TIME')
local now = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
if now < 0 or now > max_time - window then return {-3} end
local until_ms = now - now % window + window
if old_end and now < old_end then
    if now < old_end-old_window then return {-3} end
    if old_capacity ~= capacity or old_window ~= window then return {-2} end
    until_ms = old_end
else
    used = 0
end
local allowed = 0
if cost <= capacity-used then
    used = used+cost
    -- Redis 7+ uses effects replication, so TIME does not need a client clock.
    local wire = string.format('1:%.0f:%.0f:%.0f:%.0f', capacity, window, until_ms, used)
    redis.call('SET', KEYS[1], wire, 'PXAT', string.format('%.0f', until_ms))
    allowed = 1
end
return {allowed, capacity-used, until_ms-now}

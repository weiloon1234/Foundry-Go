-- One bounded metadata value and one SET with absolute expiry; no partial
-- counter/TTL mutation can survive an error in a later command. Peek never
-- writes; a denial writes only a policy conversion that ends later than the
-- live bucket, so carried usage is never released early by the old policy's
-- shorter window. Window arithmetic mirrors internal/ratewindow.
local capacity, window, cost = tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3])
local max_bytes, max_capacity, max_window, max_time = tonumber(ARGV[4]), tonumber(ARGV[5]), tonumber(ARGV[6]), tonumber(ARGV[7])
local offset, mode = tonumber(ARGV[8]), ARGV[9]
local function integer(text, maximum)
    if not text or not string.match(text, '^%d+$') or (#text > 1 and string.sub(text, 1, 1) == '0') then return nil end
    local value = tonumber(text)
    if not value or value > maximum or value % 1 ~= 0 then return nil end
    return value
end
local kind = redis.call('TYPE', KEYS[1]).ok
local old_capacity, old_window, old_offset, old_end, used
if kind ~= 'none' then
    if kind ~= 'string' or redis.call('STRLEN', KEYS[1]) > max_bytes then return {-1} end
    local stored = redis.call('GET', KEYS[1])
    local a,b,c,d,e = string.match(stored, '^2:(%d+):(%d+):(%d+):(%d+):(%d+)$')
    if not a then
        -- Version 1 buckets were aligned to Unix-epoch multiples (phase zero).
        a,b,d,e = string.match(stored, '^1:(%d+):(%d+):(%d+):(%d+)$')
        c = '0'
    end
    old_capacity, old_window = integer(a, max_capacity), integer(b, max_window)
    old_offset, old_end, used = integer(c, max_window), integer(d, max_time), integer(e, max_capacity)
    if not old_capacity or old_capacity == 0 or not old_window or old_window == 0 or not old_offset or
       old_offset >= old_window or not old_end or old_end < old_window or (old_end - old_offset) % old_window ~= 0 or
       not used or used == 0 or used > old_capacity or redis.call('PEXPIRETIME', KEYS[1]) ~= old_end then return {-1} end
end
local now_parts = redis.call('TIME')
local now = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local live = old_end and now < old_end
-- A backward clock step inside a live bucket is clamped to the bucket start; it
-- can neither reopen earlier quota nor fail the decision.
if live and now < old_end - old_window then now = old_end - old_window end
if now < 0 or now > max_time - window then return {-3} end
local until_ms, stored_offset
local converted = false
if live and old_capacity == capacity and old_window == window then
    until_ms, stored_offset = old_end, old_offset
else
    -- A new or converted bucket starts on this key's phase for the requested
    -- policy. Converting a live bucket keeps its admitted usage (capped).
    until_ms, stored_offset = now - (now - offset) % window + window, offset
    if live then
        converted = true
        if used > capacity then used = capacity end
    else
        used = 0
    end
end
local allowed = 0
if cost <= capacity-used then allowed = 1 end
if mode == 'take' and (allowed == 1 or converted and until_ms > old_end) then
    if allowed == 1 then used = used+cost end
    -- Redis 7+ uses effects replication, so TIME does not need a client clock.
    local wire = string.format('2:%.0f:%.0f:%.0f:%.0f:%.0f', capacity, window, stored_offset, until_ms, used)
    redis.call('SET', KEYS[1], wire, 'PXAT', string.format('%.0f', until_ms))
end
return {allowed, capacity-used, until_ms-now}

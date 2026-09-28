-- One bounded value and one final mutation per operation. Counts and lock
-- expiry cannot be partially updated. Denials never renew a window or lock.
local op = ARGV[1]
local capacity, window, duration = tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4])
local candidate, observed, outcome = ARGV[5], tonumber(ARGV[6]), tonumber(ARGV[7])
local max_capacity, max_duration, max_time = tonumber(ARGV[8]), tonumber(ARGV[9]), tonumber(ARGV[10])
local max_revision, max_bytes = tonumber(ARGV[11]), tonumber(ARGV[12])
local function integer(text, maximum)
    if not text or not string.match(text, '^%d+$') or (#text > 1 and string.sub(text,1,1) == '0') then return nil end
    local value = tonumber(text)
    if not value or value > maximum or value % 1 ~= 0 then return nil end
    return value
end
local function generation(text)
    return text and #text == 64 and string.match(text, '^[0-9a-f]+$') and text ~= string.rep('0',64)
end
local kind = redis.call('TYPE', KEYS[1]).ok
local old_capacity, old_window, old_duration, epoch, start, last, failures, revision, locked, expiry
if kind ~= 'none' then
    if kind ~= 'string' or redis.call('STRLEN', KEYS[1]) > max_bytes then return {-1} end
    local a,b,c,d,e,f,g,h,i = string.match(redis.call('GET', KEYS[1]), '^1:(%d+):(%d+):(%d+):([0-9a-f]+):(%d+):(%d+):(%d+):(%d+):(%d+)$')
    old_capacity, old_window, old_duration = integer(a,max_capacity), integer(b,max_duration), integer(c,max_duration)
    epoch, start, last = d, integer(e,max_time), integer(f,max_time)
    failures, revision, locked = integer(g,max_capacity), integer(h,max_revision), integer(i,max_time)
    if not old_capacity or old_capacity == 0 or not old_window or old_window == 0 or
       not old_duration or old_duration == 0 or not generation(epoch) or not start or
       start > max_time-max_duration or not last or last < start or last > max_time-max_duration or last >= start+old_window or
       not failures or failures > old_capacity or not revision or failures > revision or not locked then return {-1} end
    if (locked == 0 and failures == old_capacity) or (locked ~= 0 and (failures ~= old_capacity or locked ~= last+old_duration)) then return {-1} end
    expiry = locked ~= 0 and locked or start+old_window
    if redis.call('PEXPIRETIME', KEYS[1]) ~= expiry then return {-1} end
end
local now_parts = redis.call('TIME')
local now = tonumber(now_parts[1])*1000+math.floor(tonumber(now_parts[2])/1000)
if now < 0 or now > max_time-max_duration or (last and now < last) then return {-3} end
if expiry and now >= expiry then epoch = nil end
if epoch and (old_capacity ~= capacity or old_window ~= window or old_duration ~= duration) then return {-2} end
if op == 'reset' then
    if not epoch then return {0} end
    redis.call('DEL',KEYS[1]); return {1}
end
local function save()
    local wire = string.format('1:%.0f:%.0f:%.0f:%s:%.0f:%.0f:%.0f:%.0f:%.0f', capacity,window,duration,epoch,start,last,failures,revision,locked)
    expiry = locked ~= 0 and locked or start+window
    redis.call('SET',KEYS[1],wire,'PXAT',string.format('%.0f',expiry))
end
if op == 'begin' then
    if not epoch then
        epoch,start,last,failures,revision,locked = candidate,now,now,0,0,0
        save()
    end
    if locked > now then return {2,locked-now} end
    return {1,epoch,revision}
end
if not epoch or epoch ~= candidate then return {3} end
if observed > revision then return {-1} end
if locked > now then return {2,locked-now,0} end
if outcome == 2 and observed ~= revision then return {1} end
if revision == max_revision then return {-4} end
revision,last = revision+1,now
if outcome == 2 then failures=0 else failures=failures+1 end
if failures == capacity then locked=now+duration end
save()
if locked > now then return {2,locked-now,1} end
return {1}

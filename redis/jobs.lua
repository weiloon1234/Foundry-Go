-- One queue authority, layout 2. Envelopes stay opaque JSON strings: cjson must
-- never round application integers, decimals, IDs or payload timestamps. The
-- immutable envelope lives in its own hash (KEYS[10]) so heartbeats and state
-- transitions decode only the small mutable record. After the explicit
-- migrate_layout operation, layout-1 records, which embed their envelope,
-- remain readable and move it on their next write.
local config = ARGV[1]
local limits = cjson.decode(ARGV[2])
local request = cjson.decode(ARGV[3])
local legacy = ARGV[4]
local expected = {'hash', 'zset', 'zset', 'zset', 'hash', 'zset', 'hash', 'zset', 'zset', 'hash'}
local function integer(value, minimum, maximum)
    return type(value) == 'number' and value >= minimum and value <= maximum and value == math.floor(value)
end
local max_time = 253402300799999 -- final millisecond in year 9999
local max_count = 2147483647
local max_bytes = 1125899906842624
for i, kind in ipairs(expected) do
    local actual = redis.call('TYPE', KEYS[i]).ok
    if actual ~= 'none' and actual ~= kind then return {-2} end
end
local previous = redis.call('HGET', KEYS[5], 'config')
local layout = redis.call('HGET', KEYS[5], 'layout')
-- A layout-1 queue written with this process's former policy identity is
-- migrated in place only by the explicit migrate_layout operation; every other
-- operation reports the legacy layout (-10) so processes of the previous
-- release keep working until the operator migrates. Any other identity is an
-- incompatible policy (-9).
local migrate = false
if previous and previous ~= config then
    if previous ~= legacy or layout then return {-9} end
    if request.op ~= 'migrate_layout' then return {-10} end
    migrate = true
end
if previous and not migrate and layout ~= '2' then return {-2} end
if not previous then
    for i = 1, #KEYS do
        if redis.call('EXISTS', KEYS[i]) ~= 0 then return {-2} end
    end
end
local timestamp = redis.call('TIME')
local now = tonumber(timestamp[1]) * 1000 + math.floor(tonumber(timestamp[2]) / 1000)
local last = tonumber(redis.call('HGET', KEYS[5], 'time') or '0')
if not integer(last, 0, max_time) then return {-2} end
now = math.max(now, last)
-- Live records count against MaxEntries/MaxBytes; terminal records are retained
-- separately and evicted oldest-first when their own bounds are exceeded.
local live, live_bytes, retained, retained_bytes, failed
local legacy_count, legacy_bytes
if migrate then
    -- Layout 1 kept one count/bytes pair; split it after decoding the records.
    legacy_count = tonumber(redis.call('HGET', KEYS[5], 'count') or '0')
    legacy_bytes = tonumber(redis.call('HGET', KEYS[5], 'bytes') or '0')
    if not integer(legacy_count, 0, max_count) or not integer(legacy_bytes, 0, max_bytes) then return {-2} end
    live, live_bytes, retained, retained_bytes, failed = 0, 0, 0, 0, 0
else
    live = tonumber(redis.call('HGET', KEYS[5], 'live') or '0')
    live_bytes = tonumber(redis.call('HGET', KEYS[5], 'live_bytes') or '0')
    retained = tonumber(redis.call('HGET', KEYS[5], 'retained') or '0')
    retained_bytes = tonumber(redis.call('HGET', KEYS[5], 'retained_bytes') or '0')
    failed = tonumber(redis.call('HGET', KEYS[5], 'failed') or '0')
    if not integer(live, 0, max_count) or not integer(live_bytes, 0, max_bytes) or not integer(retained, 0, max_count)
        or not integer(retained_bytes, 0, max_bytes) or not integer(failed, 0, retained) then return {-2} end
end
local loaded, dirty, removed = {}, {}, {}
local groups, dirty_groups, removed_groups = {}, {}, {}
local advance
local corrupt = false
local states = {blocked=true, waiting=true, reserved=true, running=true, succeeded=true, failed=true, cancelled=true}
local function terminal(state)
    return state == 'succeeded' or state == 'failed' or state == 'cancelled'
end
local function load(id)
    if removed[id] then return nil end
    if loaded[id] then return loaded[id] end
    local encoded = redis.call('HGET', KEYS[1], id)
    if not encoded then return nil end
    if #encoded > limits.record_bytes then corrupt = true; return nil end
    local ok, record = pcall(cjson.decode, encoded)
    if not ok or type(record) ~= 'table' or record.id ~= id or (record.envelope ~= nil and type(record.envelope) ~= 'string')
        or type(record.state) ~= 'string' or not states[record.state]
        or not integer(record.maximum, 1, limits.attempts) or not integer(record.attempts, 0, record.maximum)
        or not integer(record.available, 0, max_time) or not integer(record.expiry, 0, max_time)
        or not integer(record.created, 0, max_time) or not integer(record.finished, 0, max_time)
        or type(record.owner) ~= 'string' or type(record.cancelled) ~= 'boolean'
        or not integer(record.bytes, 1, limits.bytes)
        or type(record.history) ~= 'table' or #record.history > limits.history
        or type(record.name) ~= 'string' or not integer(record.version, 1, 4294967295)
        or (record.abandoned ~= nil and not integer(record.abandoned, 0, 4294967295))
        or (record.exceptions ~= nil and not integer(record.exceptions, 0, record.maximum))
        or ((record.unique ~= nil) ~= (record.unique_until ~= nil))
        or (record.unique ~= nil and (type(record.unique) ~= 'string' or #record.unique ~= 64 or not string.match(record.unique, '^[0-9a-f]+$') or not integer(record.unique_until, 0, max_time)))
        or (record.workflow and (type(record.workflow) ~= 'string' or not integer(record.position, 0, limits.workflow_members - 1))) then
        corrupt = true; return nil
    end
    if (record.state == 'reserved' or record.state == 'running') and (#record.owner ~= limits.owner_bytes or record.expiry <= 0) then
        corrupt = true; return nil
    end
    if record.retries and not integer(record.retries, 0, limits.manual_retries) then corrupt = true; return nil end
    if record.last_retry and (type(record.last_retry) ~= 'string' or #record.last_retry ~= 64 or not string.match(record.last_retry, '^[0-9a-f]+$')) then corrupt = true; return nil end
    if ((record.retries or 0) > 0) ~= (record.last_retry ~= nil) then corrupt = true; return nil end
    loaded[id] = record
    return record
end
local function envelope_of(r)
    if r.envelope then return r.envelope end
    local text = redis.call('HGET', KEYS[10], r.id)
    if not text then corrupt = true; return '' end
    return text
end
local function load_group(id)
    if removed_groups[id] then return nil end
    if groups[id] then return groups[id] end
    local encoded = redis.call('HGET', KEYS[7], id)
    if not encoded then return nil end
    if #encoded > 65536 then corrupt = true; return nil end
    local ok, group = pcall(cjson.decode, encoded)
    if not ok or type(group) ~= 'table' or group.id ~= id or type(group.members) ~= 'table'
        or #group.members < 1 or #group.members > limits.workflow_steps or not integer(group.remaining, 0, #group.members)
        or not integer(group.finished, 0, max_time)
        or type(group.failed) ~= 'boolean' or type(group.cancelling) ~= 'boolean'
        or type(group.completion) ~= 'string' or type(group.fingerprint) ~= 'string'
        or (group.kind ~= 'chain' and group.kind ~= 'batch') or not integer(group.bytes, 1, limits.bytes)
        or (group.catch ~= nil and type(group.catch) ~= 'string') or (group.finally ~= nil and type(group.finally) ~= 'string') then corrupt = true; return nil end
    for _, member in ipairs(group.members) do
        if type(member) ~= 'string' then corrupt = true; return nil end
    end
    -- Groups written before callbacks existed have neither field.
    group.catch = group.catch or ''; group.finally = group.finally or ''
    groups[id] = group; return group
end
if migrate then
    -- Decode every retained record: its own state and bytes count, never an
    -- entry of its history. Finished workflow groups hold retained bytes.
    for _, id in ipairs(redis.call('ZRANGE', KEYS[4], 0, -1)) do
        local item = load(id)
        if not item or not terminal(item.state) then return {-2} end
        retained = retained + 1; retained_bytes = retained_bytes + item.bytes
        if item.state == 'failed' then failed = failed + 1 end
    end
    for _, id in ipairs(redis.call('ZRANGE', KEYS[8], 0, -1)) do
        local group = load_group(id)
        if not group or group.finished == 0 then return {-2} end
        retained_bytes = retained_bytes + group.bytes
    end
    live, live_bytes = legacy_count - retained, legacy_bytes - retained_bytes
    if not integer(live, 0, max_count) or not integer(live_bytes, 0, max_bytes) or not integer(retained, 0, max_count)
        or not integer(retained_bytes, 0, max_bytes) or not integer(failed, 0, retained) then return {-2} end
end
local settle
local function transition(r, state, reason)
    local was_terminal = terminal(r.state)
    local previous_state = r.state
    r.state = state
    if state ~= 'reserved' and state ~= 'running' then r.owner = ''; r.expiry = 0 end
    if terminal(state) then r.finished = now end
    if terminal(state) and not was_terminal then
        live = live - 1; live_bytes = live_bytes - r.bytes; retained = retained + 1; retained_bytes = retained_bytes + r.bytes
    elseif was_terminal and not terminal(state) then
        retained = retained - 1; retained_bytes = retained_bytes - r.bytes; live = live + 1; live_bytes = live_bytes + r.bytes
    end
    if previous_state == 'failed' and state ~= 'failed' then failed = failed - 1 end
    if state == 'failed' and previous_state ~= 'failed' then failed = failed + 1 end
    table.insert(r.history, {state=state, at=now, attempt=r.attempts, reason=reason, retry=r.retries or 0})
    if #r.history > limits.history then table.remove(r.history, 1) end
    dirty[r.id] = true
    if terminal(state) and not was_terminal and r.workflow then advance(r) end
end
-- remove drops one record and its accounting. Callers own workflow integrity.
local function remove(r)
    if terminal(r.state) then
        retained = retained - 1; retained_bytes = retained_bytes - r.bytes
        if r.state == 'failed' then failed = failed - 1 end
    else
        live = live - 1; live_bytes = live_bytes - r.bytes
    end
    removed[r.id] = true
end
local function expire(r)
    if r and (r.state == 'reserved' or r.state == 'running') and r.expiry <= now then
        local unstarted = r.state == 'reserved'
        if unstarted then r.abandoned = (r.abandoned or 0) + 1 end
        if r.cancelled then transition(r, 'cancelled', 'cancel_requested')
        elseif unstarted and r.abandoned >= math.max(r.maximum, limits.delivery_floor) then
            -- Repeated expiry before start (for example a payload that crashes
            -- its process while decoding) must not redeliver forever.
            transition(r, 'failed', 'delivery_limit')
        elseif r.attempts >= r.maximum then transition(r, 'failed', 'attempt_limit')
        else r.available = now; transition(r, 'waiting', 'lease_expired') end
    end
    local finished = r and r.finished or 0
    if r and r.workflow then
        local group = load_group(r.workflow)
        if not group then corrupt = true; return r end
        finished = group.finished
    end
    if r and terminal(r.state) and finished > 0 and finished + limits.retention <= now then
        remove(r); return nil
    end
    return r
end
-- A group settles once every member and its completion are terminal.
local function settled(group)
    if group.remaining ~= 0 then return false end
    if group.completion == '' then return true end
    local completion = load(group.completion)
    return not completion or terminal(completion.state)
end
-- settle releases or cancels the catch/finally callbacks of a settled group and
-- marks it finished once they are terminal too.
settle = function(group)
    local pending = false
    for _, callback in ipairs({{group.catch, group.failed and not group.cancelling}, {group.finally, not group.cancelling}}) do
        local id, run = callback[1], callback[2]
        if id ~= '' then
            local item = load(id)
            if not item then corrupt = true; return end
            if item.state == 'blocked' then
                if run then transition(item, 'waiting', '') else transition(item, 'cancelled', 'not_triggered') end
            end
            if not terminal(item.state) then pending = true end
        end
    end
    if not pending and group.finished == 0 then
        -- A finished group's metadata is retained, not live, work.
        group.finished = now
        live_bytes = live_bytes - group.bytes; retained_bytes = retained_bytes + group.bytes
    end
end
advance = function(r)
    local group = load_group(r.workflow)
    if not group then corrupt = true; return end
    dirty_groups[group.id] = true
    if r.id == group.catch or r.id == group.finally then
        if settled(group) then settle(group) end
        return
    end
    if r.id == group.completion then
        if group.remaining == 0 then settle(group) end
        return
    end
    group.remaining = group.remaining - 1
    if r.state ~= 'succeeded' then group.failed = true end
    if group.kind == 'chain' then
        local next_id = group.members[r.position + 2]
        if next_id then
            local child = load(next_id)
            if not child then corrupt = true; return end
            if child.state == 'blocked' then
                if group.failed or group.cancelling then transition(child, 'cancelled', 'dependency_failed')
                else transition(child, 'waiting', '') end
            end
        end
    end
    if group.remaining == 0 then
        if group.completion == '' then settle(group)
        else
            local completion = load(group.completion)
            if not completion then corrupt = true; return end
            if terminal(completion.state) then settle(group); return end
            if completion.state == 'blocked' then
                if group.failed or group.cancelling then transition(completion, 'cancelled', 'dependency_failed')
                else transition(completion, 'waiting', '') end
            end
        end
    end
end
local function group_ids(group)
    local ids = {}
    for _, id in ipairs(group.members) do table.insert(ids, id) end
    for _, id in ipairs({group.completion, group.catch, group.finally}) do
        if id ~= '' then table.insert(ids, id) end
    end
    return ids
end
local function remove_group(group, forced)
    local ids = group_ids(group)
    for _, id in ipairs(ids) do
        local r = load(id)
        if r and not terminal(r.state) then corrupt = true
        elseif r and forced then remove(r)
        elseif r then expire(r) end
    end
    removed_groups[group.id] = true; retained_bytes = retained_bytes - group.bytes
end
for _, id in ipairs(redis.call('ZRANGEBYSCORE', KEYS[8], '-inf', now - limits.retention, 'LIMIT', 0, 128)) do
    local group = load_group(id)
    if not group or group.finished == 0 then corrupt = true else remove_group(group, false) end
end
-- Bounded maintenance; direct operations also check their target's expiry.
for _, id in ipairs(redis.call('ZRANGEBYSCORE', KEYS[4], '-inf', now - limits.retention, 'LIMIT', 0, 128)) do
    if not removed[id] then
        local r = load(id)
        if not r or not terminal(r.state) then corrupt = true else expire(r) end
    end
end
for _, id in ipairs(redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', now, 'LIMIT', 0, 128)) do
    local r = load(id)
    if not r or (r.state ~= 'reserved' and r.state ~= 'running') then corrupt = true else expire(r) end
end
local expired_unique = redis.call('ZRANGEBYSCORE', KEYS[6], '-inf', now, 'LIMIT', 0, 128)
local add_unique = false
-- Until-processing windows released by a first start, removed after encoding.
local released_unique = {}
local exception_reasons = {handler_failed=true, handler_panicked=true, timed_out=true, exception_limit=true}
local result = {}
local status = 1
local op = request.op
local r = nil
if request.id then r = expire(load(request.id)) end
if op == 'workflow' then
    local existing = load_group(request.id)
    if existing and existing.finished > 0 and existing.finished + limits.retention <= now then remove_group(existing, false); existing = nil end
    if existing then
        if existing.fingerprint ~= request.fingerprint then return {-3} end
        result.inserted = false
    else
        local total = request.group_bytes
        for _, step in ipairs(request.steps) do
            if expire(load(step.id)) then return {-3} end
            if step.available > now + limits.max_delay then return {-2} end
            total = total + step.bytes
        end
        local active_groups = redis.call('HLEN', KEYS[7]) - redis.call('ZCARD', KEYS[8])
        if live + #request.steps > limits.entries or total > limits.bytes - live_bytes or active_groups >= limits.entries then return {-8} end
        local group = {id=request.id, kind=request.workflow_kind, members={}, completion=request.completion,
            catch=request.catch or '', finally=request.finally or '',
            remaining=0, finished=0, failed=false, cancelling=false, fingerprint=request.fingerprint, bytes=request.group_bytes}
        live = live + #request.steps; live_bytes = live_bytes + total
        for i, step in ipairs(request.steps) do
            local available = step.available; if available == 0 then available = now end
            local item = {id=step.id, envelope=step.envelope, name=step.name, version=step.version,
                maximum=step.maximum, attempts=0, available=available, expiry=0, created=now, finished=0,
                owner='', cancelled=false, bytes=step.bytes, history={}, workflow=group.id, position=i-1}
            loaded[item.id] = item; removed[item.id] = nil
            -- Steps come first; the completion and callbacks wait for them.
            local special = item.id == group.completion or item.id == group.catch or item.id == group.finally
            if not special then table.insert(group.members, item.id) end
            local state = 'waiting'
            if special or (group.kind == 'chain' and #group.members > 1) then state = 'blocked' end
            transition(item, state, '')
        end
        group.remaining = #group.members
        groups[group.id] = group; dirty_groups[group.id] = true; removed_groups[group.id] = nil
        result.inserted = true
    end
elseif op == 'cancel_workflow' then
    local group = load_group(request.id); result.changed = false
    if group and group.finished == 0 then
        group.cancelling = true; dirty_groups[group.id] = true; result.changed = true
        for _, id in ipairs(group_ids(group)) do
            local item = load(id)
            if not item then corrupt = true
            elseif not terminal(item.state) then
                item.cancelled = true; dirty[item.id] = true
                if item.state == 'waiting' or item.state == 'blocked' then transition(item, 'cancelled', 'cancel_requested') end
            end
        end
    end
elseif op == 'enqueue' then
    if r then
        if envelope_of(r) ~= request.envelope then return {-3} end
        result.inserted = false
    else
        if live >= limits.entries or request.bytes > limits.bytes - live_bytes then return {-8} end
        if request.unique ~= '' then
            local expires = redis.call('ZSCORE', KEYS[6], request.unique)
            if expires and tonumber(expires) > now then return {-6} end
            if redis.call('ZCARD', KEYS[6]) - #expired_unique >= limits.entries then return {-8} end
            add_unique = true
        end
        local available = request.available
        if available == 0 then available = now end
        if available > now + limits.max_delay then return {-2} end
        r = {id=request.id, envelope=request.envelope, name=request.name, version=request.version,
             maximum=request.maximum, attempts=0, available=available, expiry=0, created=now,
             finished=0, owner='', cancelled=false, bytes=request.bytes, history={}}
        if add_unique and request.unique_release then r.unique = request.unique; r.unique_until = now + request.unique_for end
        removed[r.id] = nil; loaded[r.id] = r
        live = live + 1; live_bytes = live_bytes + r.bytes
        transition(r, 'waiting', '')
        result.inserted = true
    end
elseif op == 'reserve' then
    -- Consider both indexed ready entries and leases reclaimed by this call.
    local chosen = nil
    local function consider(candidate)
        if candidate and candidate.state == 'waiting' and candidate.available <= now
            and (not chosen or candidate.available < chosen.available
                 or (candidate.available == chosen.available and candidate.id < chosen.id)) then chosen = candidate end
    end
    for _, id in ipairs(redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now, 'LIMIT', 0, 1)) do
        local candidate = load(id)
        if not candidate or candidate.state ~= 'waiting' then corrupt = true else consider(candidate) end
    end
    for id, _ in pairs(dirty) do if not removed[id] then consider(loaded[id]) end end
    if chosen then
        chosen.owner = request.owner; chosen.expiry = now + request.ttl
        transition(chosen, 'reserved', '')
        result.record = chosen
    end
elseif op == 'list' then
    local after = '-'
    if request.after ~= '' then after = '(' .. request.after end
    local candidates = redis.call('ZRANGEBYLEX', KEYS[9], after, '+', 'LIMIT', 0, limits.scan_limit)
    local matched = 0
    for i, id in ipairs(candidates) do
        local item = expire(load(id))
        if item and (not request.state or request.state == '' or request.state == item.state)
            and (not request.name or request.name == '' or (request.name == item.name and request.version == item.version)) then
            if not result.records then result.records = {} end
            table.insert(result.records, item); matched = matched + 1
        end
        if matched == request.limit or i == limits.scan_limit then
            if i < #candidates or #candidates == limits.scan_limit then result.next = id end
            break
        end
    end
elseif op == 'inspect' then
    result.record = r
elseif op == 'migrate_layout' then
    -- Idempotent: an already migrated (or new) queue reports no change.
    result.changed = migrate
elseif op == 'workflow_status' then
    local group = load_group(request.id)
    if group and group.finished > 0 and group.finished + limits.retention <= now then remove_group(group, false); group = nil end
    if group then
        local status = {id=group.id, kind=group.kind, total=0, pending=0, processed=0, succeeded=0, failed_jobs=0,
            cancelled=0, failed=group.failed, cancelling=group.cancelling, finished=group.finished}
        local ids = {}
        for _, id in ipairs(group.members) do table.insert(ids, id) end
        if group.completion ~= '' then table.insert(ids, group.completion) end
        for _, id in ipairs(ids) do
            local item = expire(load(id))
            if item then
                status.total = status.total + 1
                if item.state == 'succeeded' then status.processed = status.processed + 1; status.succeeded = status.succeeded + 1
                elseif item.state == 'failed' then status.processed = status.processed + 1; status.failed_jobs = status.failed_jobs + 1
                elseif item.state == 'cancelled' then status.processed = status.processed + 1; status.cancelled = status.cancelled + 1
                else status.pending = status.pending + 1 end
            end
        end
        result.workflow = status
    end
elseif op == 'stats' then
    result.stats = true
elseif op == 'forget' then
    result.changed = false
    if r and terminal(r.state) and not r.workflow and r.name == request.name and r.version == request.version then
        remove(r); result.changed = true
    end
elseif op == 'retry' then
    if not r or r.name ~= request.name or r.version ~= request.version then return {-7} end
    if r.last_retry == request.retry_token then result.changed = false
    else
        if r.state ~= 'failed' or r.workflow or r.cancelled or (r.retries or 0) >= request.max_retries
            or envelope_of(r) ~= request.envelope or r.created ~= request.expected_created
            or r.finished ~= request.expected_finished or r.attempts ~= request.expected_attempts
            or (r.retries or 0) ~= request.expected_retries then return {-7} end
        r.retries = (r.retries or 0) + 1; r.last_retry = request.retry_token
        r.attempts = 0; r.finished = 0; r.available = now; r.abandoned = nil; r.exceptions = nil
        transition(r, 'waiting', 'manually_retried')
        result.changed = true
    end
elseif op == 'cancel' then
    result.changed = false
    if r and not terminal(r.state) and r.name == request.name and r.version == request.version then
        r.cancelled = true; dirty[r.id] = true; result.changed = true
        if r.state == 'waiting' or r.state == 'blocked' then transition(r, 'cancelled', 'cancel_requested') end
    end
else
    local owned = r and (r.state == 'reserved' or r.state == 'running') and r.owner == request.owner and r.expiry > now
    if op == 'start' then
        if not owned then status = -4
        elseif r.cancelled then status = -5
        elseif r.state == 'running' then result.attempt = r.attempts
        elseif r.attempts >= r.maximum then transition(r, 'failed', 'attempt_limit'); status = -4
        else
            r.attempts = r.attempts + 1
            if r.unique then
                -- Release only the window this record opened, never a later job's.
                local score = redis.call('ZSCORE', KEYS[6], r.unique)
                if score and tonumber(score) == r.unique_until then table.insert(released_unique, r.unique) end
                r.unique = nil; r.unique_until = nil
            end
            transition(r, 'running', ''); result.attempt = r.attempts
        end
    elseif op == 'renew' then
        result.owned = not not owned; result.cancelled = false
        if owned then r.expiry = now + request.ttl; dirty[r.id] = true; result.cancelled = r.cancelled end
    elseif op == 'finish' then
        result.changed = not not owned
        if owned then
            -- A refunded release returns an interrupted attempt to the budget.
            if request.refund and r.state == 'running' and r.attempts > 0 then r.attempts = r.attempts - 1 end
            if not r.cancelled and exception_reasons[request.reason] and r.state == 'running' then
                r.exceptions = math.min((r.exceptions or 0) + 1, r.maximum)
            end
            local state, reason = request.state, request.reason
            if r.cancelled then state = 'cancelled'; reason = 'cancel_requested'
            elseif state == 'waiting' and r.attempts >= r.maximum then state = 'failed'; reason = 'attempt_limit' end
            if state == 'succeeded' and r.state ~= 'running' then return {-2} end
            if state == 'waiting' then r.available = now + request.delay end
            r.abandoned = nil
            transition(r, state, reason)
        end
    else return {-2} end
end
-- Retained terminal records are bounded separately from live work. Evict the
-- oldest independent terminal records, then the oldest finished workflows.
-- Unfinished work is never evicted; scanning is bounded per call.
local function over() return retained > limits.retained or retained_bytes > limits.bytes end
local scanned = 0
while over() and scanned < 512 and not corrupt do
    local ids = redis.call('ZRANGE', KEYS[4], scanned, scanned + 63)
    if #ids == 0 then break end
    for _, id in ipairs(ids) do
        if not over() then break end
        if not removed[id] then
            local item = load(id)
            -- A record reopened by this call is still indexed as terminal.
            if not item or (not terminal(item.state) and not dirty[id]) then corrupt = true; break end
            if terminal(item.state) and not item.workflow then remove(item) end
        end
    end
    scanned = scanned + 64
end
local evicted_groups = 0
while over() and evicted_groups < 16 and not corrupt do
    local ids = redis.call('ZRANGE', KEYS[8], evicted_groups, evicted_groups)
    if #ids == 0 then break end
    if not removed_groups[ids[1]] then
        local group = load_group(ids[1])
        if not group or group.finished == 0 then corrupt = true else remove_group(group, true) end
    end
    evicted_groups = evicted_groups + 1
end
if corrupt or live < 0 or live_bytes < 0 or retained < 0 or retained_bytes < 0 or failed < 0 then return {-2} end
-- Encode before the first write. Corrupt records and incompatible live config
-- fail without changing the queue; Redis scripts do not provide rollback.
local writes, envelope_writes = {}, {}
for id, _ in pairs(dirty) do
    if not removed[id] then
        local item = loaded[id]
        if item.envelope then envelope_writes[id] = item.envelope; item.envelope = nil end
        writes[id] = cjson.encode(item)
    end
end
local group_writes = {}
for id, _ in pairs(dirty_groups) do if not removed_groups[id] then group_writes[id] = cjson.encode(groups[id]) end end
local function with_envelope(item)
    if item then item.envelope = envelope_writes[item.id] or envelope_of(item) end
end
with_envelope(result.record)
for _, item in ipairs(result.records or {}) do with_envelope(item) end
if corrupt then return {-2} end
local response = nil
if not result.stats then response = cjson.encode(result) end
-- A reservation or read that changed nothing writes nothing, not even metadata.
local changed = migrate or not previous or add_unique or #expired_unique > 0 or next(dirty) ~= nil or next(removed) ~= nil
    or next(dirty_groups) ~= nil or next(removed_groups) ~= nil
for id, _ in pairs(removed) do
    redis.call('HDEL', KEYS[1], id)
    redis.call('HDEL', KEYS[10], id)
    redis.call('ZREM', KEYS[9], id)
    for i = 2, 4 do redis.call('ZREM', KEYS[i], id) end
end
for id, encoded in pairs(writes) do
    local item = loaded[id]
    redis.call('HSET', KEYS[1], id, encoded)
    if envelope_writes[id] then redis.call('HSET', KEYS[10], id, envelope_writes[id]) end
    redis.call('ZADD', KEYS[9], 0, id)
    for i = 2, 4 do redis.call('ZREM', KEYS[i], id) end
    if item.state == 'waiting' then redis.call('ZADD', KEYS[2], item.available, id)
    elseif item.state == 'reserved' or item.state == 'running' then redis.call('ZADD', KEYS[3], item.expiry, id)
    elseif terminal(item.state) then redis.call('ZADD', KEYS[4], item.finished, id) end
end
for id, _ in pairs(removed_groups) do redis.call('HDEL', KEYS[7], id); redis.call('ZREM', KEYS[8], id) end
for id, encoded in pairs(group_writes) do
    redis.call('HSET', KEYS[7], id, encoded)
    if groups[id].finished > 0 then redis.call('ZADD', KEYS[8], groups[id].finished, id) end
end
for _, digest in ipairs(expired_unique) do redis.call('ZREM', KEYS[6], digest) end
for _, digest in ipairs(released_unique) do redis.call('ZREM', KEYS[6], digest) end
if add_unique then redis.call('ZADD', KEYS[6], now + request.unique_for, request.unique) end
if changed then
    redis.call('HSET', KEYS[5], 'config', config, 'layout', '2', 'live', live, 'live_bytes', live_bytes,
        'retained', retained, 'retained_bytes', retained_bytes, 'failed', failed, 'time', now)
    if migrate then redis.call('HDEL', KEYS[5], 'count', 'bytes') end
end
if result.stats then
    local ready = redis.call('ZCARD', KEYS[2])
    local waiting = redis.call('ZCOUNT', KEYS[2], '-inf', now)
    local leased = redis.call('ZCARD', KEYS[3])
    response = cjson.encode({stats={waiting=waiting, delayed=ready - waiting, blocked=math.max(0, live - ready - leased),
        leased=leased, failed=failed, retained=retained}})
end
return {status, response}

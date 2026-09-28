-- One queue authority. Envelopes stay opaque JSON strings: cjson must never
-- round application integers, decimals, IDs or payload timestamps.
local config = ARGV[1]
local limits = cjson.decode(ARGV[2])
local request = cjson.decode(ARGV[3])
local expected = {'hash', 'zset', 'zset', 'zset', 'hash', 'zset', 'hash', 'zset', 'zset'}
local function integer(value, minimum, maximum)
    return type(value) == 'number' and value >= minimum and value <= maximum and value == math.floor(value)
end
local max_time = 253402300799999 -- final millisecond in year 9999
for i, kind in ipairs(expected) do
    local actual = redis.call('TYPE', KEYS[i]).ok
    if actual ~= 'none' and actual ~= kind then return {-2} end
end
local previous = redis.call('HGET', KEYS[5], 'config')
if previous and previous ~= config then return {-3} end
if not previous then
    for i = 1, #KEYS do
        if redis.call('EXISTS', KEYS[i]) ~= 0 then return {-2} end
    end
end
local timestamp = redis.call('TIME')
local now = tonumber(timestamp[1]) * 1000 + math.floor(tonumber(timestamp[2]) / 1000)
local count = tonumber(redis.call('HGET', KEYS[5], 'count') or '0')
local bytes = tonumber(redis.call('HGET', KEYS[5], 'bytes') or '0')
local last = tonumber(redis.call('HGET', KEYS[5], 'time') or '0')
if not integer(count, 0, limits.entries) or not integer(bytes, 0, limits.bytes) or not integer(last, 0, max_time) then return {-2} end
now = math.max(now, last)
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
    if not ok or type(record) ~= 'table' or record.id ~= id or type(record.envelope) ~= 'string'
        or type(record.state) ~= 'string' or not states[record.state]
        or not integer(record.maximum, 1, limits.attempts) or not integer(record.attempts, 0, record.maximum)
        or not integer(record.available, 0, max_time) or not integer(record.expiry, 0, max_time)
        or not integer(record.created, 0, max_time) or not integer(record.finished, 0, max_time)
        or type(record.owner) ~= 'string' or type(record.cancelled) ~= 'boolean'
        or not integer(record.bytes, 1, limits.bytes)
        or type(record.history) ~= 'table' or #record.history > limits.history
        or type(record.name) ~= 'string' or not integer(record.version, 1, 4294967295)
        or (record.workflow and (type(record.workflow) ~= 'string' or not integer(record.position, 0, limits.workflow_steps))) then
        corrupt = true; return nil
    end
    if (record.state == 'reserved' or record.state == 'running') and (#record.owner ~= limits.owner_bytes or record.expiry <= 0) then
        corrupt = true; return nil
    end
    loaded[id] = record
    return record
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
        or (group.kind ~= 'chain' and group.kind ~= 'batch') or not integer(group.bytes, 1, limits.bytes) then corrupt = true; return nil end
    for _, member in ipairs(group.members) do
        if type(member) ~= 'string' then corrupt = true; return nil end
    end
    groups[id] = group; return group
end
local function transition(r, state, reason)
    local was_terminal = terminal(r.state)
    r.state = state
    if state ~= 'reserved' and state ~= 'running' then r.owner = ''; r.expiry = 0 end
    if terminal(state) then r.finished = now end
    table.insert(r.history, {state=state, at=now, attempt=r.attempts, reason=reason})
    if #r.history > limits.history then table.remove(r.history, 1) end
    dirty[r.id] = true
    if terminal(state) and not was_terminal and r.workflow then advance(r) end
end
local function expire(r)
    if r and (r.state == 'reserved' or r.state == 'running') and r.expiry <= now then
        if r.cancelled then transition(r, 'cancelled', 'cancel_requested')
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
        count = count - 1; bytes = bytes - r.bytes; removed[r.id] = true; return nil
    end
    return r
end
advance = function(r)
    local group = load_group(r.workflow)
    if not group then corrupt = true; return end
    dirty_groups[group.id] = true
    if r.id == group.completion then
        if group.remaining == 0 then group.finished = now end
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
        if group.completion == '' then group.finished = now
        else
            local completion = load(group.completion)
            if not completion then corrupt = true; return end
            if terminal(completion.state) then group.finished = now; return end
            if completion.state == 'blocked' then
                if group.failed or group.cancelling then transition(completion, 'cancelled', 'dependency_failed')
                else transition(completion, 'waiting', '') end
            end
        end
    end
end
local function remove_group(group)
    for _, id in ipairs(group.members) do
        local r = load(id)
        if r and not terminal(r.state) then corrupt = true elseif r then expire(r) end
    end
    if group.completion ~= '' then
        local r = load(group.completion)
        if r and not terminal(r.state) then corrupt = true elseif r then expire(r) end
    end
    removed_groups[group.id] = true; bytes = bytes - group.bytes
end
for _, id in ipairs(redis.call('ZRANGEBYSCORE', KEYS[8], '-inf', now - limits.retention, 'LIMIT', 0, 128)) do
    local group = load_group(id)
    if not group or group.finished == 0 then corrupt = true else remove_group(group) end
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
local result = {}
local status = 1
local op = request.op
local r = nil
if request.id then r = expire(load(request.id)) end
if op == 'workflow' then
    local existing = load_group(request.id)
    if existing and existing.finished > 0 and existing.finished + limits.retention <= now then remove_group(existing); existing = nil end
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
        local group_count = redis.call('HLEN', KEYS[7])
        for _ in pairs(removed_groups) do group_count = group_count - 1 end
        if count + #request.steps > limits.entries or total > limits.bytes - bytes or group_count >= limits.entries then return {-3} end
        local group = {id=request.id, kind=request.workflow_kind, members={}, completion=request.completion,
            remaining=#request.steps, finished=0, failed=false, cancelling=false, fingerprint=request.fingerprint, bytes=request.group_bytes}
        if request.completion ~= '' then group.remaining = group.remaining - 1 end
        for i, step in ipairs(request.steps) do
            local available = step.available; if available == 0 then available = now end
            local item = {id=step.id, envelope=step.envelope, name=step.name, version=step.version,
                maximum=step.maximum, attempts=0, available=available, expiry=0, created=now, finished=0,
                owner='', cancelled=false, bytes=step.bytes, history={}, workflow=group.id, position=i-1}
            loaded[item.id] = item; removed[item.id] = nil
            if item.id ~= group.completion then table.insert(group.members, item.id) end
            local state = 'waiting'
            if (group.kind == 'chain' and i > 1) or item.id == group.completion then state = 'blocked' end
            transition(item, state, '')
        end
        groups[group.id] = group; dirty_groups[group.id] = true; removed_groups[group.id] = nil
        count = count + #request.steps; bytes = bytes + total; result.inserted = true
    end
elseif op == 'cancel_workflow' then
    local group = load_group(request.id); result.changed = false
    if group and group.finished == 0 then
        group.cancelling = true; dirty_groups[group.id] = true; result.changed = true
        local ids = {}; for _, id in ipairs(group.members) do table.insert(ids, id) end
        if group.completion ~= '' then table.insert(ids, group.completion) end
        for _, id in ipairs(ids) do
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
        if r.envelope ~= request.envelope then return {-3} end
        result.inserted = false
    else
        if count >= limits.entries or request.bytes > limits.bytes - bytes then return {-3} end
        if request.unique ~= '' then
            local expires = redis.call('ZSCORE', KEYS[6], request.unique)
            if expires and tonumber(expires) > now then return {-6} end
            if redis.call('ZCARD', KEYS[6]) - #expired_unique >= limits.entries then return {-3} end
            add_unique = true
        end
        local available = request.available
        if available == 0 then available = now end
        if available > now + limits.max_delay then return {-2} end
        r = {id=request.id, envelope=request.envelope, name=request.name, version=request.version,
             maximum=request.maximum, attempts=0, available=available, expiry=0, created=now,
             finished=0, owner='', cancelled=false, bytes=request.bytes, history={}}
        removed[r.id] = nil; loaded[r.id] = r
        count = count + 1; bytes = bytes + r.bytes
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
        else r.attempts = r.attempts + 1; transition(r, 'running', ''); result.attempt = r.attempts end
    elseif op == 'renew' then
        result.owned = not not owned; result.cancelled = false
        if owned then r.expiry = now + request.ttl; dirty[r.id] = true; result.cancelled = r.cancelled end
    elseif op == 'finish' then
        result.changed = not not owned
        if owned then
            local state, reason = request.state, request.reason
            if r.cancelled then state = 'cancelled'; reason = 'cancel_requested'
            elseif state == 'waiting' and r.attempts >= r.maximum then state = 'failed'; reason = 'attempt_limit' end
            if state == 'succeeded' and r.state ~= 'running' then return {-2} end
            if state == 'waiting' then r.available = now + request.delay end
            transition(r, state, reason)
        end
    else return {-2} end
end
if corrupt or count < 0 or bytes < 0 then return {-2} end
-- Encode before the first write. Corrupt records and incompatible live config
-- fail without changing the queue; Redis scripts do not provide rollback.
local writes = {}
for id, _ in pairs(dirty) do if not removed[id] then writes[id] = cjson.encode(loaded[id]) end end
local group_writes = {}
for id, _ in pairs(dirty_groups) do if not removed_groups[id] then group_writes[id] = cjson.encode(groups[id]) end end
local response = cjson.encode(result)
for id, _ in pairs(removed) do
    redis.call('HDEL', KEYS[1], id)
    redis.call('ZREM', KEYS[9], id)
    for i = 2, 4 do redis.call('ZREM', KEYS[i], id) end
end
for id, encoded in pairs(writes) do
    local item = loaded[id]
    redis.call('HSET', KEYS[1], id, encoded)
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
if add_unique then redis.call('ZADD', KEYS[6], now + request.unique_for, request.unique) end
redis.call('HSET', KEYS[5], 'config', config, 'count', count, 'bytes', bytes, 'time', now)
return {status, response}

-- One queue authority, layout 3. Envelopes stay opaque JSON strings: cjson must
-- never round application integers, decimals, IDs or payload timestamps. The
-- immutable envelope lives in its own hash (KEYS[10]) so heartbeats and state
-- transitions decode only the small mutable record. Records migrated from
-- layout 1 through v0.13.0 may still embed envelopes until their next write.
-- Layout 3 fences writers that do not understand independent quarantine.
local config = ARGV[1]
local limits = cjson.decode(ARGV[2])
local request = cjson.decode(ARGV[3])
local legacy = ARGV[4]
local expected = {'hash', 'zset', 'zset', 'zset', 'hash', 'zset', 'hash', 'zset', 'zset', 'hash', 'zset'}
local function integer(value, minimum, maximum)
    -- cjson can preserve -0, which Go's unsigned integer decoder rejects.
    return type(value) == 'number' and value >= minimum and value <= maximum and value == math.floor(value)
        and (value ~= 0 or 1 / value > 0)
end
-- Redis cjson emits exponent notation at 1e14; Go integer decoding rejects it.
-- The Go boundary owns this common ceiling for all encoded integers.
local max_time = limits.integer_max
local max_count = 2147483647
local max_bytes = limits.integer_max
local function valid_numbers(value)
    if type(value) == 'number' then return integer(value, 0, limits.integer_max) end
    if type(value) == 'table' then
        for _, item in pairs(value) do if not valid_numbers(item) then return false end end
    end
    return true
end
if not valid_numbers(request) then return {-2} end
if request.op == 'inspect_layout' then
    local function inspect_result(state, eligible, blocker)
        return {1, cjson.encode({inspection={layout=state, eligible=eligible, blocker=blocker}})}
    end
    for i, kind in ipairs(expected) do
        local actual = redis.call('TYPE', KEYS[i]).ok
        if actual ~= 'none' and actual ~= kind then
            return inspect_result('unknown', false, 'invalid_queue_state')
        end
    end
    local previous = redis.call('HGET', KEYS[5], 'config')
    local layout = redis.call('HGET', KEYS[5], 'layout')
    if not previous then
        for i = 1, #KEYS do
            if redis.call('EXISTS', KEYS[i]) ~= 0 then
                return inspect_result('unknown', false, 'invalid_queue_state')
            end
        end
        local timestamp = redis.call('TIME')
        local now = tonumber(timestamp[1]) * 1000 + math.floor(tonumber(timestamp[2]) / 1000)
        if not integer(now, 0, max_time) then
            return inspect_result('unknown', false, 'invalid_queue_state')
        end
        return inspect_result('absent', true, '')
    end
    local legacy_layout_1 = (previous == legacy and (not layout or layout == '1'))
        or (previous == config and layout == '1')
    if previous ~= config and not legacy_layout_1 then
        return inspect_result('unknown', false, 'queue_policy_mismatch')
    end
    if not legacy_layout_1 then
        if not layout or layout == '' then
            return inspect_result('unknown', false, 'invalid_queue_state')
        end
        if layout ~= '2' and layout ~= '3' then
            return inspect_result('unknown', false, 'unsupported_layout')
        end
    end
    local timestamp = redis.call('TIME')
    local now = tonumber(timestamp[1]) * 1000 + math.floor(tonumber(timestamp[2]) / 1000)
    local last = tonumber(redis.call('HGET', KEYS[5], 'time') or '0')
    if not integer(last, 0, max_time) or not integer(math.max(now, last), 0, max_time) then
        return inspect_result('unknown', false, 'invalid_queue_state')
    end
    local live = tonumber(redis.call('HGET', KEYS[5], 'live') or '0')
    local live_bytes = tonumber(redis.call('HGET', KEYS[5], 'live_bytes') or '0')
    local retained = tonumber(redis.call('HGET', KEYS[5], 'retained') or '0')
    local retained_bytes = tonumber(redis.call('HGET', KEYS[5], 'retained_bytes') or '0')
    local failed = tonumber(redis.call('HGET', KEYS[5], 'failed') or '0')
    if not integer(live, 0, max_count) or not integer(live_bytes, 0, max_bytes) or not integer(retained, 0, max_count)
        or not integer(retained_bytes, 0, max_bytes) or not integer(failed, 0, retained) then
        return inspect_result('unknown', false, 'invalid_queue_state')
    end
    if legacy_layout_1 then
        return inspect_result('1', false, 'layout_1_requires_upgrade')
    end
    if layout == '2' and redis.call('ZCARD', KEYS[11]) ~= 0 then
        return inspect_result('unknown', false, 'invalid_queue_state')
    end
    return inspect_result(layout, true, '')
end
for i, kind in ipairs(expected) do
    local actual = redis.call('TYPE', KEYS[i]).ok
    if actual ~= 'none' and actual ~= kind then return {-2} end
end
local previous = redis.call('HGET', KEYS[5], 'config')
local layout = redis.call('HGET', KEYS[5], 'layout')
-- Layout 1 must first migrate with v0.13.0. Layout 2 changes only its
-- metadata marker after a coordinated cutover; ordinary calls never migrate.
if previous and previous ~= config then
    if previous == legacy and not layout then return {-10} end
    return {-9}
end
if previous then
    if layout ~= '2' and layout ~= '3' then return {-2} end
    if layout == '2' and request.op ~= 'migrate_layout' then return {-10} end
end
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
if not integer(now, 0, max_time) then return {-2} end
-- Live records count against MaxEntries/MaxBytes; terminal records are retained
-- separately and evicted oldest-first when their own bounds are exceeded.
local live = tonumber(redis.call('HGET', KEYS[5], 'live') or '0')
local live_bytes = tonumber(redis.call('HGET', KEYS[5], 'live_bytes') or '0')
local retained = tonumber(redis.call('HGET', KEYS[5], 'retained') or '0')
local retained_bytes = tonumber(redis.call('HGET', KEYS[5], 'retained_bytes') or '0')
local failed = tonumber(redis.call('HGET', KEYS[5], 'failed') or '0')
if not integer(live, 0, max_count) or not integer(live_bytes, 0, max_bytes) or not integer(retained, 0, max_count)
    or not integer(retained_bytes, 0, max_bytes) or not integer(failed, 0, retained) then return {-2} end
if request.op == 'migrate_layout' then
    if layout == '2' and redis.call('ZCARD', KEYS[11]) ~= 0 then return {-2} end
    local response = cjson.encode({changed=layout == '2'})
    if layout == '2' then redis.call('HSET', KEYS[5], 'layout', '3')
    elseif not previous then
        redis.call('HSET', KEYS[5], 'config', config, 'layout', '3', 'live', 0, 'live_bytes', 0,
            'retained', 0, 'retained_bytes', 0, 'failed', 0, 'time', now)
    end
    return {1, response}
end
local loaded, dirty, removed = {}, {}, {}
local groups, dirty_groups, removed_groups = {}, {}, {}
local quarantined = {}
local detached_finished = {}
local advance
local corrupt = false
local states = {blocked=true, waiting=true, reserved=true, running=true, succeeded=true, failed=true, cancelled=true}
local function terminal(state)
    return state == 'succeeded' or state == 'failed' or state == 'cancelled'
end
local function identifier(id)
    if type(id) ~= 'string' or #id ~= 36 or string.sub(id, 9, 9) ~= '-'
        or string.sub(id, 14, 14) ~= '-' or string.sub(id, 19, 19) ~= '-'
        or string.sub(id, 24, 24) ~= '-' then return false end
    local compact = string.gsub(id, '-', '')
    return #compact == 32 and string.match(compact, '^[0-9a-f]+$') ~= nil
        and compact ~= string.rep('0', 32)
end
local function is_quarantined(id)
    if quarantined[id] then return true end
    local score = redis.call('ZSCORE', KEYS[11], id)
    if score and (not identifier(id) or not integer(tonumber(score), 0, max_time)) then corrupt = true end
    return score ~= false
end
local function stage_quarantine(id)
    if dirty[id] or removed[id] then corrupt = true; return end
    quarantined[id] = true
end
local function dense_array(items, maximum)
    if type(items) ~= 'table' or #items < 1 or #items > maximum then return false end
    local count = 0
    for key, _ in pairs(items) do
        if not integer(key, 1, #items) then return false end
        count = count + 1
    end
    return count == #items
end
local record_fields = {id=true, envelope=true, state=true, maximum=true, attempts=true,
    available=true, expiry=true, created=true, finished=true, owner=true, cancelled=true,
    bytes=true, history=true, name=true, version=true, abandoned=true, exceptions=true,
    unique=true, unique_until=true, position=true, workflow=true, retries=true, last_retry=true}
local history_fields = {state=true, reason=true, at=true, attempt=true, retry=true}
local function canonical_fields(object, allowed)
    if type(object) ~= 'table' then return false end
    for key, _ in pairs(object) do if not allowed[key] then return false end end
    return true
end
local function validate_history(history)
    -- Persisted records always have an initial transition. Empty Lua tables
    -- would encode as objects, not the arrays required by the Go decoder.
    if not dense_array(history, limits.history) then return false end
    for _, entry in ipairs(history) do
        if not canonical_fields(entry, history_fields) or type(entry.state) ~= 'string' or not states[entry.state]
            or type(entry.reason) ~= 'string' or not integer(entry.at, 0, max_time)
            or not integer(entry.attempt, 0, 4294967295)
            or (entry.retry ~= nil and not integer(entry.retry, 0, limits.manual_retries)) then return false end
    end
    return true
end
local function has_envelope(record)
    if record.envelope ~= nil then return type(record.envelope) == 'string' and #record.envelope > 0 end
    return redis.call('HSTRLEN', KEYS[10], record.id) > 0
end
local invalid_records, workflow_observed = {}, {}
local function read_record(id)
    if removed[id] then return nil, false, false end
    if loaded[id] then return loaded[id], not has_envelope(loaded[id]), loaded[id].workflow ~= nil end
    if invalid_records[id] then return nil, true, workflow_observed[id] end
    local size = redis.call('HSTRLEN', KEYS[1], id)
    if size > limits.record_bytes then invalid_records[id] = true; return nil, true, false end
    local encoded = redis.call('HGET', KEYS[1], id)
    if not encoded then return nil, false, false end
    local ok, record = pcall(cjson.decode, encoded)
    local workflow = ok and type(record) == 'table' and record.workflow ~= nil
    workflow_observed[id] = workflow
    invalid_records[id] = true
    if not ok or not canonical_fields(record, record_fields) or not identifier(id) or record.id ~= id
        or (record.envelope ~= nil and type(record.envelope) ~= 'string')
        or type(record.state) ~= 'string' or not states[record.state]
        or not integer(record.maximum, 1, limits.attempts) or not integer(record.attempts, 0, record.maximum)
        or not integer(record.available, 0, max_time) or not integer(record.expiry, 0, max_time)
        or not integer(record.created, 0, max_time) or not integer(record.finished, 0, max_time)
        or type(record.owner) ~= 'string' or type(record.cancelled) ~= 'boolean'
        or not integer(record.bytes, 1, limits.bytes) or not validate_history(record.history)
        or type(record.name) ~= 'string' or not integer(record.version, 1, 4294967295)
        or (record.abandoned ~= nil and not integer(record.abandoned, 0, 4294967295))
        or (record.exceptions ~= nil and not integer(record.exceptions, 0, record.maximum))
        or ((record.unique ~= nil) ~= (record.unique_until ~= nil))
        or (record.unique ~= nil and (type(record.unique) ~= 'string' or #record.unique ~= 64 or not string.match(record.unique, '^[0-9a-f]+$') or not integer(record.unique_until, 0, max_time)))
        or (record.position ~= nil and not workflow)
        or (workflow and (not identifier(record.workflow) or not integer(record.position, 0, limits.workflow_members - 1))) then
        return nil, true, workflow
    end
    if (record.state == 'reserved' or record.state == 'running') and (#record.owner ~= limits.owner_bytes or record.expiry <= 0) then
        return nil, true, workflow
    end
    if record.retries ~= nil and not integer(record.retries, 0, limits.manual_retries) then return nil, true, workflow end
    if record.last_retry ~= nil and (type(record.last_retry) ~= 'string' or #record.last_retry ~= 64 or not string.match(record.last_retry, '^[0-9a-f]+$')) then return nil, true, workflow end
    if ((record.retries or 0) > 0) ~= (record.last_retry ~= nil) or not has_envelope(record) then return nil, true, workflow end
    invalid_records[id] = nil
    loaded[id] = record
    return record, false, workflow
end
local function load(id)
    if removed[id] then return nil end
    if is_quarantined(id) then corrupt = true; return nil end
    local record, invalid = read_record(id)
    if invalid then corrupt = true; return nil end
    if not record then
        if redis.call('HEXISTS', KEYS[10], id) ~= 0 then corrupt = true end
        for _, index in ipairs({2, 3, 4, 9}) do
            if redis.call('ZSCORE', KEYS[index], id) then corrupt = true end
        end
    end
    return record
end
local function envelope_of(r)
    if r.envelope then return r.envelope end
    local text = redis.call('HGET', KEYS[10], r.id)
    if not text or #text == 0 then corrupt = true; return '' end
    return text
end
local function group_ids(group)
    local ids = {}
    for _, id in ipairs(group.members) do table.insert(ids, id) end
    for _, id in ipairs({group.completion, group.catch, group.finally}) do
        if id ~= '' then table.insert(ids, id) end
    end
    return ids
end
local group_positions, catalog_groups, new_groups = {}, {}, {}
local max_group_bytes = 65536
local function validate_group(group, id)
    if type(group) ~= 'table' or not identifier(id) or group.id ~= id
        or not dense_array(group.members, limits.workflow_steps) or not integer(group.remaining, 0, #group.members)
        or not integer(group.finished, 0, max_time)
        or type(group.failed) ~= 'boolean' or type(group.cancelling) ~= 'boolean'
        or type(group.completion) ~= 'string' or type(group.fingerprint) ~= 'string'
        or #group.fingerprint ~= 64 or not string.match(group.fingerprint, '^[0-9a-f]+$')
        or (group.kind ~= 'chain' and group.kind ~= 'batch') or not integer(group.bytes, 1, limits.bytes)
        or (group.catch ~= nil and type(group.catch) ~= 'string') or (group.finally ~= nil and type(group.finally) ~= 'string') then return false end
    group.catch = group.catch or ''; group.finally = group.finally or ''
    local positions = {}
    for i, member in ipairs(group_ids(group)) do
        if not identifier(member) or positions[member] ~= nil then return false end
        positions[member] = i - 1
    end
    group_positions[id] = positions
    return true
end
local function read_group(id, persisted)
    if persisted and catalog_groups[id] then return catalog_groups[id] end
    if not persisted and groups[id] then return groups[id] end
    if redis.call('HSTRLEN', KEYS[7], id) > max_group_bytes then return nil end
    local encoded = redis.call('HGET', KEYS[7], id)
    if not encoded then return nil end
    local ok, group = pcall(cjson.decode, encoded)
    if not ok or not validate_group(group, id) then return nil end
    catalog_groups[id] = group
    if not groups[id] then groups[id] = group end
    return group
end
local function load_group(id)
    if removed_groups[id] and not new_groups[id] then return nil end
    local group = read_group(id)
    if not group and redis.call('HEXISTS', KEYS[7], id) ~= 0 then corrupt = true end
    return group
end
local function load_member(group, id)
    local record = load(id)
    if not group_positions[group.id] and not validate_group(group, group.id) then corrupt = true; return nil end
    if not record or record.workflow ~= group.id or group_positions[group.id][id] == nil
        or record.position ~= group_positions[group.id][id] then corrupt = true; return nil end
    return record
end
-- A negative membership result is usable only after a complete bounded
-- examination. Groups staged for deletion still contribute their references.
local proof, proof_members, proof_count, proof_bytes = nil, {}, 0, 0
local function add_proof_group(group, size)
    proof_count = proof_count + 1; proof_bytes = proof_bytes + size
    if proof_count > limits.proof_groups or size > max_group_bytes or proof_bytes > limits.proof_bytes then
        proof = false; return
    end
    for _, id in ipairs(group_ids(group)) do proof_members[id] = true end
end
local function prove_independent(id, workflow)
    if workflow or not identifier(id) then return false end
    if proof == nil then
        proof = true
        local count = redis.call('HLEN', KEYS[7])
        if count > limits.proof_groups then proof = false; return false end
        local ids = redis.call('HKEYS', KEYS[7])
        if #ids ~= count then proof = false; return false end
        for _, group_id in ipairs(ids) do
            local size = redis.call('HSTRLEN', KEYS[7], group_id)
            if not identifier(group_id) or size == 0 or size > max_group_bytes or proof_bytes + size > limits.proof_bytes then
                proof = false; return false
            end
            local group = read_group(group_id, true)
            if not group then proof = false; return false end
            add_proof_group(group, size)
        end
        for _, group in pairs(new_groups) do
            add_proof_group(group, #cjson.encode(group))
            if not proof then return false end
        end
    end
    return proof and not proof_members[id]
end
local function load_indexed(id, index, score)
    if removed[id] then return nil end
    if is_quarantined(id) then
        if index ~= 9 then stage_quarantine(id) end
        return nil
    end
    local record, invalid, workflow = read_record(id)
    local misplaced = false
    if record and not dirty[id] then
        score = tonumber(score or redis.call('ZSCORE', KEYS[index], id))
        misplaced = (index == 2 and (record.state ~= 'waiting' or score ~= record.available))
            or (index == 3 and ((record.state ~= 'reserved' and record.state ~= 'running') or score ~= record.expiry))
            or (index == 4 and (not terminal(record.state) or score ~= record.finished))
            or (index == 9 and score ~= 0)
    end
    if not record or invalid or misplaced then
        if prove_independent(id, workflow) then stage_quarantine(id) else corrupt = true end
        return nil
    end
    if index == 4 and record.workflow then
        local group = load_group(record.workflow)
        if not group or not load_member(group, id) then corrupt = true; return nil end
        -- Layout-2 terminal members (and pre-release layout-3 entries) detach
        -- in bounded staged batches. Only their group may delete them.
        detached_finished[id] = true
        return nil
    end
    return record
end
if request.op == 'quarantined' then
    if not integer(request.limit, 1, limits.list_limit) or (request.after ~= '' and not identifier(request.after)) then return {-2} end
    local after = '-'
    if request.after ~= '' then after = '(' .. request.after end
    local ids = redis.call('ZRANGEBYLEX', KEYS[9], after, '+', 'LIMIT', 0, limits.scan_limit)
    local page, entries = {}, {}
    for i, id in ipairs(ids) do
        if not identifier(id) or tonumber(redis.call('ZSCORE', KEYS[9], id)) ~= 0 then return {-2} end
        local at = redis.call('ZSCORE', KEYS[11], id)
        if at then
            at = tonumber(at)
            if not integer(at, 0, max_time) then return {-2} end
            table.insert(entries, {id=id, at=at})
        end
        if #entries == request.limit or i == limits.scan_limit then
            if i < #ids or #ids == limits.scan_limit then page.next = id end
            break
        end
    end
    if #entries > 0 then page.quarantined = entries end
    return {1, cjson.encode(page)}
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
-- Only a preflighted group or a complete independence proof owns deletion.
local function remove(r)
    if terminal(r.state) then
        retained = retained - 1; retained_bytes = retained_bytes - r.bytes
        if r.state == 'failed' then failed = failed - 1 end
    else
        live = live - 1; live_bytes = live_bytes - r.bytes
    end
    removed[r.id] = true
end
local function remove_independent(r)
    if not prove_independent(r.id, r.workflow ~= nil) then corrupt = true; return end
    remove(r)
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
    if r and r.workflow then
        local group = load_group(r.workflow)
        if not group or not load_member(group, r.id) then corrupt = true end
    elseif r and terminal(r.state) and r.finished > 0 and r.finished + limits.retention <= now then
        remove_independent(r); return nil
    end
    return r
end
local function visible(r)
    if r and r.workflow and terminal(r.state) then
        local group = load_group(r.workflow)
        if not group then corrupt = true; return nil end
        if group.finished > 0 and group.finished + limits.retention <= now then return nil end
    end
    return r
end
-- A group settles once every member and its completion are terminal.
local function settled(group)
    if group.remaining ~= 0 then return false end
    if group.completion == '' then return true end
    local completion = load_member(group, group.completion)
    return completion ~= nil and terminal(completion.state)
end
-- settle releases or cancels the catch/finally callbacks of a settled group and
-- marks it finished once they are terminal too.
settle = function(group)
    local pending = false
    for _, callback in ipairs({{group.catch, group.failed and not group.cancelling}, {group.finally, not group.cancelling}}) do
        local id, run = callback[1], callback[2]
        if id ~= '' then
            local item = load_member(group, id)
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
    if not group or not load_member(group, r.id) then corrupt = true; return end
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
            local child = load_member(group, next_id)
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
            local completion = load_member(group, group.completion)
            if not completion then corrupt = true; return end
            if terminal(completion.state) then settle(group); return end
            if completion.state == 'blocked' then
                if group.failed or group.cancelling then transition(completion, 'cancelled', 'dependency_failed')
                else transition(completion, 'waiting', '') end
            end
        end
    end
end
local function remove_group(group, forced)
    if removed_groups[group.id] == group then return end
    if group.finished == 0 or (not forced and group.finished + limits.retention > now) then corrupt = true; return end
    local members = {}
    for _, id in ipairs(group_ids(group)) do
        local item = load_member(group, id)
        if not item or not terminal(item.state) then corrupt = true; return end
        table.insert(members, item)
    end
    for _, item in ipairs(members) do remove(item) end
    removed_groups[group.id] = group; retained_bytes = retained_bytes - group.bytes
end
for _, id in ipairs(redis.call('ZRANGEBYSCORE', KEYS[8], '-inf', now - limits.retention, 'LIMIT', 0, 128)) do
    local group = load_group(id)
    if not group or group.finished == 0 then corrupt = true else remove_group(group, false) end
end
-- Bounded maintenance; direct operations also check their target's expiry.
for _, id in ipairs(redis.call('ZRANGEBYSCORE', KEYS[4], '-inf', now - limits.retention, 'LIMIT', 0, 128)) do
    if not removed[id] then
        local r = load_indexed(id, 4)
        if r then expire(r) end
    end
end
for _, id in ipairs(redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', now, 'LIMIT', 0, 128)) do
    local r = load_indexed(id, 3)
    if r then expire(r) end
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
if request.id and op ~= 'workflow' and op ~= 'cancel_workflow' and op ~= 'workflow_status' then r = expire(load(request.id)) end
if corrupt then return {-2} end
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
            if corrupt then return {-2} end
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
        groups[group.id] = group; dirty_groups[group.id] = true; group_positions[group.id] = nil
        new_groups[group.id] = group
        if proof then add_proof_group(group, #cjson.encode(group)) end
        result.inserted = true
    end
elseif op == 'cancel_workflow' then
    local group = load_group(request.id); result.changed = false
    if group and group.finished == 0 then
        group.cancelling = true; dirty_groups[group.id] = true; result.changed = true
        for _, id in ipairs(group_ids(group)) do
            local item = load_member(group, id)
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
        if not candidate or candidate.state ~= 'waiting' or candidate.available > now then return end
        if candidate.workflow then
            local group = load_group(candidate.workflow)
            if not group or not load_member(group, candidate.id) then corrupt = true; return end
        end
        if not chosen or candidate.available < chosen.available
            or (candidate.available == chosen.available and candidate.id < chosen.id) then chosen = candidate end
    end
    local ready = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', now, 'WITHSCORES', 'LIMIT', 0, limits.scan_limit)
    local ordered = #ready < limits.scan_limit * 2
    for i = 1, #ready, 2 do
        local id = ready[i]
        local candidate = load_indexed(id, 2, ready[i + 1])
        if candidate and not dirty[id] then consider(candidate); ordered = true; break end
    end
    -- A full window without an unchanged valid candidate proves no ordering
    -- against unseen ready work. Commit maintenance but reserve nothing.
    if ordered then
        for id, _ in pairs(dirty) do if not removed[id] then consider(loaded[id]) end end
    end
    if chosen and not corrupt then
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
        local item = visible(expire(load_indexed(id, 9)))
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
    result.record = visible(r)
elseif op == 'workflow_status' then
    local group = load_group(request.id)
    if group and group.finished > 0 and group.finished + limits.retention <= now then remove_group(group, false); group = nil end
    if group then
        local status = {id=group.id, kind=group.kind, total=0, pending=0, processed=0, succeeded=0, failed_jobs=0,
            cancelled=0, failed=group.failed, cancelling=group.cancelling, finished=group.finished}
        for _, id in ipairs(group_ids(group)) do
            local item = expire(load_member(group, id))
            if item and id ~= group.catch and id ~= group.finally then
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
    if r and terminal(r.state) and r.name == request.name and r.version == request.version then
        remove_independent(r); result.changed = true
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
            local item = load_indexed(id, 4)
            if item and terminal(item.state) then remove_independent(item) end
        end
    end
    scanned = scanned + 64
end
local evicted_groups = 0
while over() and evicted_groups < 16 and not corrupt do
    local ids = redis.call('ZRANGE', KEYS[8], evicted_groups, evicted_groups)
    if #ids == 0 then break end
    if not removed_groups[ids[1]] and not new_groups[ids[1]] then
        local group = load_group(ids[1])
        if not group or group.finished == 0 then corrupt = true else remove_group(group, true) end
    end
    evicted_groups = evicted_groups + 1
end
if corrupt or not integer(live, 0, max_count) or not integer(live_bytes, 0, max_bytes)
    or not integer(retained, 0, max_count) or not integer(retained_bytes, 0, max_bytes)
    or not integer(failed, 0, retained) then return {-2} end
if add_unique and not integer(now + request.unique_for, 0, max_time) then return {-2} end
-- Encode before the first write. Corrupt records and incompatible live config
-- fail without changing the queue; Redis scripts do not provide rollback.
local writes, envelope_writes = {}, {}
for id, _ in pairs(dirty) do
    if not removed[id] then
        local item = loaded[id]
        if not valid_numbers(item) then return {-2} end
        if item.envelope then envelope_writes[id] = item.envelope; item.envelope = nil end
        writes[id] = cjson.encode(item)
    end
end
local group_writes = {}
for id, _ in pairs(dirty_groups) do
    if removed_groups[id] ~= groups[id] then
        if not valid_numbers(groups[id]) then return {-2} end
        group_writes[id] = cjson.encode(groups[id])
    end
end
local function with_envelope(item)
    if item then item.envelope = envelope_writes[item.id] or envelope_of(item) end
end
with_envelope(result.record)
for _, item in ipairs(result.records or {}) do with_envelope(item) end
if corrupt then return {-2} end
if result.stats then
    local ready = redis.call('ZCARD', KEYS[2])
    local waiting = redis.call('ZCOUNT', KEYS[2], '-inf', now)
    local leased = redis.call('ZCARD', KEYS[3])
    local quarantine_count = redis.call('ZCARD', KEYS[11])
    local affected = {}
    for id, _ in pairs(removed) do affected[id] = true end
    for id, _ in pairs(dirty) do affected[id] = true end
    for id, _ in pairs(quarantined) do affected[id] = true end
    for id, _ in pairs(affected) do
        local score = redis.call('ZSCORE', KEYS[2], id)
        if score then
            score = tonumber(score)
            if not integer(score, 0, max_time) then return {-2} end
            ready = ready - 1
            if score <= now then waiting = waiting - 1 end
        end
        if redis.call('ZSCORE', KEYS[3], id) then leased = leased - 1 end
        if quarantined[id] and not redis.call('ZSCORE', KEYS[11], id) then quarantine_count = quarantine_count + 1 end
        if dirty[id] and not removed[id] then
            local item = loaded[id]
            if item.state == 'waiting' then
                ready = ready + 1
                if item.available <= now then waiting = waiting + 1 end
            elseif item.state == 'reserved' or item.state == 'running' then leased = leased + 1 end
        end
    end
    result = {stats={waiting=waiting, delayed=ready - waiting, blocked=math.max(0, live - ready - leased),
        leased=leased, failed=failed, retained=retained, quarantined=quarantine_count}}
end
if not valid_numbers(result) then return {-2} end
local response = cjson.encode(result)
-- A reservation or read that changed nothing writes nothing, not even metadata.
local changed = not previous or add_unique or #expired_unique > 0 or next(dirty) ~= nil or next(removed) ~= nil
    or next(dirty_groups) ~= nil or next(removed_groups) ~= nil or next(quarantined) ~= nil or next(detached_finished) ~= nil
for id, _ in pairs(detached_finished) do redis.call('ZREM', KEYS[4], id) end
for id, _ in pairs(removed) do
    redis.call('HDEL', KEYS[1], id)
    redis.call('HDEL', KEYS[10], id)
    redis.call('ZREM', KEYS[9], id)
    for i = 2, 4 do redis.call('ZREM', KEYS[i], id) end
end
for id, _ in pairs(quarantined) do
    redis.call('ZADD', KEYS[11], 'NX', now, id)
    redis.call('ZADD', KEYS[9], 0, id)
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
    elseif terminal(item.state) and not item.workflow then redis.call('ZADD', KEYS[4], item.finished, id) end
end
for id, _ in pairs(removed_groups) do redis.call('HDEL', KEYS[7], id); redis.call('ZREM', KEYS[8], id) end
for id, encoded in pairs(group_writes) do
    redis.call('HSET', KEYS[7], id, encoded)
    redis.call('ZREM', KEYS[8], id)
    if groups[id].finished > 0 then redis.call('ZADD', KEYS[8], groups[id].finished, id) end
end
for _, digest in ipairs(expired_unique) do redis.call('ZREM', KEYS[6], digest) end
for _, digest in ipairs(released_unique) do redis.call('ZREM', KEYS[6], digest) end
if add_unique then redis.call('ZADD', KEYS[6], now + request.unique_for, request.unique) end
if changed then
    redis.call('HSET', KEYS[5], 'config', config, 'layout', '3', 'live', live, 'live_bytes', live_bytes,
        'retained', retained, 'retained_bytes', retained_bytes, 'failed', failed, 'time', now)
end
return {status, response}

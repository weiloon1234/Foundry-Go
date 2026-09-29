-- One explicitly namespaced standalone Redis authority. Application JSON stays
-- opaque text; cjson never rounds payload integers, IDs or decimal values.
local base, policy = ARGV[1], ARGV[2]
local limits, request = cjson.decode(ARGV[3]), cjson.decode(ARGV[4])
local metadata, connections = KEYS[1], KEYS[2]
local bad = false
local max_time, max_revision = 253402300799999, 9007199254740991
local function integer(n, lo, hi)
    return type(n) == 'number' and n >= lo and n <= hi and n == math.floor(n)
end
local function digest(text)
    return type(text) == 'string' and #text == 64 and not text:find('[^0-9a-f]')
end
local function identity(text)
    return type(text) == 'string' and #text == 36 and text:sub(9,9) == '-' and text:sub(14,14) == '-' and text:sub(19,19) == '-' and text:sub(24,24) == '-' and #text:gsub('%-', '') == 32 and not text:gsub('%-', ''):find('[^0-9a-f]')
end
local function kind(key, expected)
    local actual = redis.call('TYPE', key).ok
    if actual ~= 'none' and actual ~= expected then bad = true; return false end
    return true
end
local function unsigned(text, maximum)
    if type(text) ~= 'string' or #text > 16 or not text:match('^%d+$') then bad = true; return 0 end
    local number = tonumber(text)
    if not integer(number, 0, maximum) then bad = true; return 0 end
    return number
end
if not kind(metadata, 'hash') or not kind(connections, 'zset') then return {-1} end
if redis.call('HLEN', metadata) > 4 or redis.call('ZCARD', connections) > limits.connections then return {-1} end
if redis.call('HSTRLEN',metadata,'policy')>64 or redis.call('HSTRLEN',metadata,'limits')>2048 or redis.call('HSTRLEN',metadata,'time')>16 or redis.call('HSTRLEN',metadata,'revision')>16 then return {-1} end
local previous = redis.call('HGET', metadata, 'policy')
if previous and (redis.call('HLEN',metadata)~=4 or not redis.call('HGET',metadata,'time') or not redis.call('HGET',metadata,'revision')) then return {-1} end
if previous and (previous ~= policy or redis.call('HGET', metadata, 'limits') ~= ARGV[3]) then return {-2} end
if not previous and (redis.call('EXISTS', metadata) ~= 0 or redis.call('EXISTS', connections) ~= 0) then return {-1} end
local clock = redis.call('TIME')
local now = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local last = unsigned(redis.call('HGET', metadata, 'time') or '0', max_time)
local revision = unsigned(redis.call('HGET', metadata, 'revision') or '0', max_revision)
if bad then return {-1} end
if not integer(now, 0, max_time - limits.retention) then return {-5} end
-- Authority time never moves backwards: a server clock stepped back by at most
-- the retention window is clamped to the last committed time instead of
-- failing every operation. A larger gap is corrupt or unrecoverable state.
if now < last then
    if last - now > limits.retention then return {-5} end
    now = last
end
local expiry = now + limits.ttl
local changed = not previous
-- Metadata and expiry refresh are skipped while nothing changed and the last
-- commit is recent, so frequent lease renewals avoid rewriting one shared key.
local refresh = math.min(1000, math.floor(limits.ttl / 4))
local function commit_metadata()
    if not changed and now - last < refresh then return end
    redis.call('HSET', metadata, 'policy', policy, 'limits', ARGV[3], 'time', string.format('%.0f', now), 'revision', string.format('%.0f', revision))
    redis.call('PEXPIRE', metadata, limits.retention)
    redis.call('PEXPIRE', connections, 2 * limits.ttl)
end
local function next_revision()
    if revision >= max_revision then return nil end
    revision = revision + 1
    changed = true
    return revision
end
local function connection_key(id) return base .. ':connection:' .. id end
local function subject_key(id) return base .. ':subject:' .. id end
local function presence_keys(scope) return base .. ':presence:' .. scope .. ':leases', base .. ':presence:' .. scope .. ':data' end
local function live(id)
    local score = redis.call('ZSCORE', connections, id)
    if not score then return false end
    local deadline = tonumber(score)
    if not integer(deadline, 0, max_time) then bad = true; return false end
    return deadline > now
end
local function member_record(encoded)
    if type(encoded) ~= 'string' or #encoded > 6 * limits.member_bytes + 512 then bad = true; return nil end
    local ok, record = pcall(cjson.decode, encoded)
    if not ok or type(record) ~= 'table' or type(record.subject) ~= 'string' or (record.subject ~= '' and not digest(record.subject))
        or type(record.presence) ~= 'boolean' or type(record.data) ~= 'string' or #record.data > limits.member_bytes
        or (record.presence and (record.subject == '' or record.data == '')) or (not record.presence and record.data ~= '') then bad = true; return nil end
    return record
end
local function load_connection(id)
    local key = connection_key(id)
    if not kind(key, 'hash') then return nil end
    local length = redis.call('HLEN', key)
    if length == 0 then return nil end
    if length > limits.subscriptions + 1 or redis.call('PTTL', key) < 0 then bad = true; return nil end
    if redis.call('HSTRLEN',key,'owner') ~= 36 then bad=true;return nil end
    local owner = redis.call('HGET', key, 'owner')
    if not identity(owner) then bad = true; return nil end
    local records, fields = {}, redis.call('HKEYS', key)
    for _, field in ipairs(fields) do
        if field ~= 'owner' then
            if not digest(field) or redis.call('HSTRLEN',key,field)>6*limits.member_bytes+512 then bad=true;return nil end
            local record = member_record(redis.call('HGET',key,field))
            if not record then return nil end
            records[field] = record
        end
    end
    return {key=key, owner=owner, records=records, count=length-1}
end
local function prune_subject(subject)
    local key = subject_key(subject)
    if not kind(key, 'zset') or redis.call('ZCARD', key) > limits.connections then bad = true; return nil end
    local ids = redis.call('ZRANGE', key, 0, -1, 'WITHSCORES')
    for i = 1, #ids, 2 do
        if not identity(ids[i]) or not integer(tonumber(ids[i+1]), 0, max_time) then bad = true; return nil end
        if tonumber(ids[i+1]) <= now or not live(ids[i]) then redis.call('ZREM', key, ids[i]) end
    end
    return key
end
-- Lua string comparison follows the server's collation locale; identity order
-- must be bytewise so every Redis selects the same representative record.
local function byte_less(a, b)
    for i = 1, math.min(#a, #b) do
        local x, y = a:byte(i), b:byte(i)
        if x ~= y then return x < y end
    end
    return #a < #b
end
local function load_presence(scope)
    local leases, data = presence_keys(scope)
    if not kind(leases, 'zset') or not kind(data, 'hash') then return nil end
    local count = redis.call('ZCARD', leases)
    if count > limits.connections or redis.call('HLEN', data) ~= count then bad = true; return nil end
    local rows, members = redis.call('ZRANGE', leases, 0, -1, 'WITHSCORES'), {}
    local records = {}
    local subject_count, pruned = 0, false
    for i = 1, #rows, 2 do
        local id, deadline = rows[i], tonumber(rows[i+1])
        if not identity(id) or not integer(deadline, 0, max_time) then bad = true; return nil end
        if deadline <= now or not live(id) then
            redis.call('ZREM', leases, id); redis.call('HDEL', data, id)
            pruned = true
        else
            if redis.call('HSTRLEN', data, id) > 6 * limits.member_bytes + 512 then bad = true; return nil end
            local record = member_record(redis.call('HGET', data, id))
            if not record or not record.presence then bad = true; return nil end
            records[id]=record
            local member = members[record.subject]
            if not member then
                subject_count = subject_count + 1
                if subject_count > limits.members then bad = true; return nil end
                member = {id=record.subject, data=record.data, connections=0, selected=id}
                members[record.subject] = member
            end
            member.connections = member.connections + 1
            if byte_less(member.selected, id) then member.data=record.data; member.selected=id end
        end
    end
    return {leases=leases, data=data, members=members, count=subject_count,records=records,pruned=pruned}
end
-- Every presence change (join, leave, close, restored or expired entries)
-- advances the revision; an unchanged read reuses it and rewrites no state.
local function presence_snapshot(group, changed_scope)
    -- Members are returned unordered; the adapter sorts them by bytes.
    local members = {}
    for _, member in pairs(group.members) do
        table.insert(members, {id=member.id, data=member.data, connections=member.connections})
    end
    local current = revision
    if changed_scope or group.pruned or revision == 0 then
        current = next_revision()
        if not current then return nil end
    end
    commit_metadata()
    -- Empty arrays are explicit: cjson encodes an empty Lua table as an object.
    local text = #members == 0 and '[]' or cjson.encode(members)
    return '{"revision":' .. string.format('%.0f', current) .. ',"members":' .. text .. '}'
end
local function remove_presence(scope, id)
    local leases, data = presence_keys(scope)
    redis.call('ZREM', leases, id); redis.call('HDEL', data, id)
end
local function history_keys(channel)
    local prefix = base .. ':history:' .. channel
    return prefix .. ':order', prefix .. ':expiry', prefix .. ':data', prefix .. ':metadata'
end
local function history_command()
    if request.replay_messages == 0 then commit_metadata(); return {0, '{"frames":[]}'} end
    local order, deadlines, data, info = history_keys(request.channel)
    if not kind(order, 'zset') or not kind(deadlines, 'zset') or not kind(data, 'hash') or not kind(info, 'hash') then return {-1} end
    local count = redis.call('ZCARD', order)
    if count > request.replay_messages or redis.call('ZCARD', deadlines) ~= count or redis.call('HLEN', data) ~= count or redis.call('HLEN', info) > 3 then return {-1} end
    if redis.call('HSTRLEN',info,'policy')>64 or redis.call('HSTRLEN',info,'settings')>128 or redis.call('HSTRLEN',info,'bytes')>16 then return {-1} end
    local previous_policy = redis.call('HGET', info, 'policy')
    local settings = tostring(request.replay_messages) .. ':' .. tostring(request.replay_bytes) .. ':' .. tostring(request.replay_ttl)
    if previous_policy and (redis.call('HLEN',info)~=3 or not redis.call('HGET',info,'bytes')) then return {-1} end
    if previous_policy and (previous_policy ~= policy or redis.call('HGET', info, 'settings') ~= settings) then return {-2} end
    if not previous_policy and (count ~= 0 or redis.call('EXISTS', info) ~= 0) then return {-1} end
    local size = unsigned(redis.call('HGET', info, 'bytes') or '0', request.replay_bytes)
    if bad then return {-1} end
    local ids, calculated, expired = redis.call('ZRANGE', order, 0, -1, 'WITHSCORES'), 0, {}
    for i = 1, #ids, 2 do
        local id = ids[i]
        local deadline = tonumber(redis.call('ZSCORE', deadlines, id))
        local bytes = redis.call('HSTRLEN', data, id)
        if not identity(id) or not integer(tonumber(ids[i+1]), 1, max_revision) or not integer(deadline, 0, max_time) or bytes < 1 or bytes > limits.frame_bytes then return {-1} end
        calculated = calculated + bytes
        if calculated > request.replay_bytes then return {-1} end
        if deadline <= now then table.insert(expired, id) end
    end
    if calculated ~= size then return {-1} end
    local function remove(id)
        size = size - redis.call('HSTRLEN', data, id)
        redis.call('ZREM', order, id);redis.call('ZREM', deadlines, id);redis.call('HDEL', data, id)
        count = count - 1
    end
    for _, id in ipairs(expired) do remove(id) end
    if request.op == 'append' then
        local prior = redis.call('HGET', data, request.message)
        if prior then
            if prior ~= request.frame then return {-1} end
        else
            if revision >= max_revision then return {-5} end
            while count >= request.replay_messages or size + #request.frame > request.replay_bytes do
                local oldest = redis.call('ZRANGE', order, 0, 0)[1]
                if not oldest then return {-1} end
                remove(oldest)
            end
            local sequence = next_revision()
            redis.call('ZADD', order, sequence, request.message)
            redis.call('ZADD', deadlines, now+request.replay_ttl, request.message)
            redis.call('HSET', data, request.message, request.frame)
            count=count+1;size=size+#request.frame
        end
    end
    if count > 0 then
        redis.call('HSET', info, 'policy', policy, 'settings', settings, 'bytes', size)
        for _, key in ipairs({order,deadlines,data,info}) do redis.call('PEXPIRE', key, request.replay_ttl) end
    else
        redis.call('DEL', info)
    end
    commit_metadata()
    if request.op == 'append' then return {0} end
    local result = {}
    for _, id in ipairs(redis.call('ZRANGE', order, 0, -1)) do table.insert(result, redis.call('HGET', data, id)) end
    return {0, '{"frames":' .. (#result == 0 and '[]' or cjson.encode(result)) .. '}'}
end

-- Only expired scores are pruned; there is no reset/namespace wipe operation.
redis.call('ZREMRANGEBYSCORE', connections, '-inf', now)
if request.op == 'check' then commit_metadata();return {0} end
if request.op == 'append' or request.op == 'history' then return history_command() end
if request.op == 'members' then
    local group = load_presence(request.scope)
    if bad then return {-1} end
    local snapshot = presence_snapshot(group, false)
    if not snapshot then return {-5} end
    return {0,snapshot}
end
local record = load_connection(request.connection)
if bad then return {-1} end
if request.op == 'open' then
    if record or live(request.connection) then return {-4} end
    if redis.call('ZCARD', connections) >= limits.connections then return {-3} end
    redis.call('HSET', connection_key(request.connection), 'owner', request.instance)
    redis.call('PEXPIRE', connection_key(request.connection), limits.ttl)
    redis.call('ZADD', connections, expiry, request.connection)
    -- The first open may create the connections set; always refresh expiry.
    changed = true
    commit_metadata();return {0}
end
local present = live(request.connection)
if bad then return {-1} end
if not record or not present then
    if request.op == 'close' or request.op == 'leave' then commit_metadata();return {0} end
    return {-4}
end
if record.owner ~= request.instance then return {-4} end
if request.op == 'touch' then
    -- Renewal touches only this connection's own entries instead of loading
    -- every member of each presence scope. A missing entry (for example an
    -- evicted key) is restored from the connection record.
    local renewals, restored = {}, false
    for scope, member in pairs(record.records) do
        if member.subject ~= '' then
            local key = subject_key(member.subject)
            if not kind(key, 'zset') then return {-1} end
            table.insert(renewals, key)
        end
        if member.presence then
            local leases, data = presence_keys(scope)
            if not kind(leases, 'zset') or not kind(data, 'hash') then return {-1} end
            local stored = redis.call('HGET', data, request.connection)
            if stored then
                local found = member_record(stored)
                if bad or found.subject ~= member.subject or found.data ~= member.data then return {-1} end
            else
                redis.call('HSET', data, request.connection, cjson.encode({subject=member.subject,presence=member.presence,data=member.data}))
                restored = true
            end
            table.insert(renewals, leases)
            redis.call('PEXPIRE', data, 2*limits.ttl)
        end
    end
    for _, key in ipairs(renewals) do redis.call('ZADD', key, expiry, request.connection);redis.call('PEXPIRE', key, 2*limits.ttl) end
    redis.call('PEXPIRE', record.key, limits.ttl);redis.call('ZADD', connections, expiry, request.connection)
    if restored then next_revision() end
    commit_metadata();return {0}
end
local groups, subjects = {}, {}
-- Validate every referenced structure before mutating a live connection.
for scope, member in pairs(record.records) do
    if member.subject ~= '' then
        subjects[member.subject]=prune_subject(member.subject)
        if not bad and not redis.call('ZSCORE',subjects[member.subject],request.connection) then bad=true end
    end
    if member.presence then
        groups[scope]=load_presence(scope)
        local found=groups[scope] and groups[scope].records[request.connection]
        if not found or found.subject~=member.subject or found.data~=member.data then bad=true end
    end
end
if bad then return {-1} end
if request.op == 'close' then
    if next(groups) and revision >= max_revision then return {-5} end
    for _, key in pairs(subjects) do redis.call('ZREM', key, request.connection) end
    for scope, _ in pairs(groups) do remove_presence(scope,request.connection) end
    if next(groups) then next_revision() end
    redis.call('DEL', record.key);redis.call('ZREM', connections,request.connection)
    commit_metadata();return {0}
end
if request.op == 'leave' then
    local prior = record.records[request.scope]
    if prior then
        if prior.presence then
            if revision >= max_revision then return {-5} end
            remove_presence(request.scope,request.connection)
            next_revision()
        end
        if prior.subject ~= '' then
            local remaining = false
            for scope, member in pairs(record.records) do if scope ~= request.scope and member.subject == prior.subject then remaining=true;break end end
            if not remaining then redis.call('ZREM', subjects[prior.subject], request.connection) end
        end
        redis.call('HDEL', record.key, request.scope)
    end
    commit_metadata();return {0}
end
if request.op == 'join' then
    local prior = record.records[request.scope]
    -- A retained record from a failed leave is a per-connection mismatch, not a
    -- namespace policy conflict: the hub releases it and joins again.
    if prior and (prior.subject ~= request.subject or prior.presence ~= request.presence or prior.data ~= request.data) then return {-6} end
    if not prior and record.count >= limits.subscriptions then return {-3} end
    local subject
    if request.subject ~= '' then
        subject=prune_subject(request.subject)
        if bad then return {-1} end
        if not redis.call('ZSCORE', subject, request.connection) and redis.call('ZCARD', subject) >= limits.subjects then return {-3} end
    end
    local group
    if request.presence then
        group=load_presence(request.scope)
        if bad then return {-1} end
        if not group.members[request.subject] and group.count >= limits.members then return {-3} end
        if revision >= max_revision then return {-5} end
    end
    local encoded = cjson.encode({subject=request.subject,presence=request.presence,data=request.data})
    redis.call('HSET',record.key,request.scope,encoded)
    -- Admission uses the existing connection deadline, not a fresh lease. Only
    -- heartbeat renews ownership; a delayed join cannot extend a dead connection.
    local deadline=tonumber(redis.call('ZSCORE',connections,request.connection))
    if subject then redis.call('ZADD',subject,deadline,request.connection);redis.call('PEXPIRE',subject,2*limits.ttl) end
    if group then
        redis.call('ZADD',group.leases,deadline,request.connection);redis.call('HSET',group.data,request.connection,encoded)
        redis.call('PEXPIRE',group.leases,2*limits.ttl);redis.call('PEXPIRE',group.data,2*limits.ttl)
        group=load_presence(request.scope)
        if bad then return {-1} end
        local snapshot=presence_snapshot(group, true)
        if not snapshot then return {-5} end
        return {0,snapshot}
    end
    commit_metadata();return {0}
end
return {-1}

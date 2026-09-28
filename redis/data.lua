-- One typed data key. Validation reads precede the sole mutation command.
local key, op, expected = KEYS[1], ARGV[1], ARGV[2]
local kind = redis.call('TYPE',key).ok
if kind ~= 'none' and kind ~= expected then return {-1} end
if op == 'exists' then return {kind == 'none' and 0 or 1} end
if op == 'expire' then
 if kind == 'none' then return {0} end
 redis.call(unpack(entry_expiry(key,ARGV[7])))
 return {1}
end
local capacity, value_bound = tonumber(ARGV[3]), tonumber(ARGV[5])
local count_command = expected == 'hash' and 'HLEN' or 'SCARD'
local count = redis.call(count_command,key)
if count > capacity then return {-1} end
if op == 'count' then return {1,count} end
if expected == 'hash' then
 local field,value = ARGV[7],ARGV[8]
 if op == 'hset' then
  if count == capacity and redis.call('HEXISTS',key,field) == 0 then return {-1} end
  return {redis.call('HSET',key,field,value)}
 end
 if redis.call('HSTRLEN',key,field) > value_bound then return {-1} end
 if op == 'hdel' then return {redis.call('HDEL',key,field)} end
 local value = redis.call('HGET',key,field)
 if not value then return {0} end
 return {1,value}
end
local member = ARGV[7]
if op == 'sadd' then
 if count == capacity and redis.call('SISMEMBER',key,member) == 0 then return {-1} end
 return {redis.call('SADD',key,member)}
end
if op == 'srem' then return {redis.call('SREM',key,member)} end
if op == 'contains' then return {redis.call('SISMEMBER',key,member)} end
-- SCARD bounds element count before enumeration. SMEMBERS allocates on the
-- server before lengths can be inspected; externally oversized members are not
-- a hard server-memory bound. They are rejected before a payload reply is sent.
local members = redis.call('SMEMBERS',key)
local remaining = tonumber(ARGV[6])
for _,value in ipairs(members) do
 if #value > value_bound or #value == 0 or #value > remaining then return {-1} end
 remaining = remaining - #value
end
table.sort(members)
local result = {1}
for _,value in ipairs(members) do result[#result+1] = value end
return result

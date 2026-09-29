-- Sorted sets: canonical JSON members with string scores (never Lua numbers,
-- which would truncate integer replies). Growth is bounded before mutation.
local count = redis.call('ZCARD',key)
if count > capacity then return {-1} end
local function scored(items)
 local result, remaining = {1}, reply_bound
 for i = 1, #items, 2 do
  local member = items[i]
  if #member == 0 or #member > value_bound or #member > remaining then return {-1} end
  remaining = remaining - #member
  result[#result+1] = member
  result[#result+1] = items[i+1]
 end
 return result
end
if op == 'zrange' then
 local start = tonumber(ARGV[7])
 local command = {'ZRANGE', key, start, start + tonumber(ARGV[8]) - 1}
 if ARGV[9] == 'desc' then command[#command+1] = 'REV' end
 command[#command+1] = 'WITHSCORES'
 return scored(redis.call(unpack(command)))
end
if op == 'zrangebyscore' then
 local command = {'ZRANGE', key, ARGV[7], ARGV[8], 'BYSCORE'}
 if ARGV[11] == 'desc' then
  command = {'ZRANGE', key, ARGV[8], ARGV[7], 'BYSCORE', 'REV'}
 end
 for _,part in ipairs({'LIMIT', ARGV[9], ARGV[10], 'WITHSCORES'}) do command[#command+1] = part end
 return scored(redis.call(unpack(command)))
end
if op == 'zcount' then return {1, redis.call('ZCOUNT', key, ARGV[7], ARGV[8])} end
local member = ARGV[7]
if op == 'zadd' or op == 'zincrby' then
 local exists = redis.call('ZSCORE',key,member)
 if not exists and count >= capacity then return {-1} end
 if op == 'zadd' then
  redis.call('ZADD',key,ARGV[8],member)
  return {exists and 0 or 1}
 end
 local result = redis.pcall('ZINCRBY',key,ARGV[8],member)
 local failure = numeric_error(result, {'NaN', 'not a number'})
 if failure then return failure end
 return {1, result}
end
if op == 'zrem' then return {redis.call('ZREM',key,member)} end
if op == 'zscore' then
 local score = redis.call('ZSCORE',key,member)
 if not score then return {0} end
 return {1, score}
end
if op == 'zrank' then
 local rank = redis.call(ARGV[8] == 'desc' and 'ZREVRANK' or 'ZRANK', key, member)
 if not rank then return {0} end
 return {1, rank}
end
return {-1}

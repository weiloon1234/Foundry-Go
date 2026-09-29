-- Hash batch reads and exact integer fields. Sizes are checked before payloads
-- are transferred; the only mutation (HINCRBY) follows every validation read.
local count = redis.call('HLEN',key)
if count > capacity then return {-1} end
if op == 'hmget' then
 local fields, remaining = {}, reply_bound
 for i = 7, #ARGV do
  local size = redis.call('HSTRLEN',key,ARGV[i])
  if size > value_bound or size > remaining then return {-1} end
  remaining = remaining - size
  fields[#fields+1] = ARGV[i]
 end
 local result = {1}
 for _,value in ipairs(redis.call('HMGET',key,unpack(fields))) do
  if value then
   result[#result+1] = 1
   result[#result+1] = value
  else
   result[#result+1] = 0
  end
 end
 return result
end
if op == 'hgetall' then
 local fields, remaining = redis.call('HKEYS',key), reply_bound
 for _,field in ipairs(fields) do
  local size = redis.call('HSTRLEN',key,field)
  if #field == 0 or #field > field_bound or size == 0 or size > value_bound or size > remaining then return {-1} end
  remaining = remaining - size
 end
 -- Unordered: Lua string order follows the server's locale (strcoll), so the
 -- Go adapter sorts by bytes instead.
 local result = {1}
 for _,field in ipairs(fields) do
  result[#result+1] = field
  result[#result+1] = redis.call('HGET',key,field)
 end
 return result
end
if op == 'hincrby' then
 local field, delta = ARGV[7], ARGV[8]
 if redis.call('HEXISTS',key,field) == 1 then
  if redis.call('HSTRLEN',key,field) > value_bound then return {-1} end
  local old = redis.call('HGET',key,field)
  if old ~= '0' and not string.match(old, '^%-?[1-9][0-9]*$') then return {-2} end
 elseif count >= capacity then
  return {-1}
 end
 local result = redis.pcall('HINCRBY',key,field,delta)
 local failure = numeric_error(result, {'overflow', 'not an integer'})
 if failure then return failure end
 return {1, redis.call('HGET',key,field)}
end
return {-1}

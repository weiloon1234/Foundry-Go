-- Bounded lists. A push checks every value and the resulting length before one
-- insert; a pop checks the element's size before removing it.
local count = redis.call('LLEN',key)
if count > capacity then return {-1} end
local head = ARGV[7] == 'front'
if op == 'push' then
 local values = {}
 for i = 8, #ARGV do
  if #ARGV[i] == 0 or #ARGV[i] > value_bound then return {-1} end
  values[#values+1] = ARGV[i]
 end
 if #values == 0 or count + #values > capacity then return {-1} end
 return {1, redis.call(head and 'LPUSH' or 'RPUSH', key, unpack(values))}
end
if op == 'pop' then
 if count == 0 then return {0} end
 local value = redis.call('LINDEX', key, head and 0 or -1)
 if #value == 0 or #value > value_bound then return {-1} end
 redis.call(head and 'LPOP' or 'RPOP', key)
 return {1, value}
end
if op == 'range' then
 local result, remaining = {1}, reply_bound
 for _,value in ipairs(redis.call('LRANGE', key, ARGV[7], ARGV[8])) do
  if #value == 0 or #value > value_bound or #value > remaining then return {-1} end
  remaining = remaining - #value
  result[#result+1] = value
 end
 return result
end
if op == 'trim' then
 if count > 0 then redis.call('LTRIM', key, ARGV[7], ARGV[8]) end
 return {1}
end
return {-1}

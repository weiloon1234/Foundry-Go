-- Typed data envelope shared by structure scripts. ARGV[1..6] are operation,
-- declared kind, entries, field bytes, value bytes and reply bytes; operation
-- arguments follow. A key of another Redis type is rejected before any read.
local key, op, expected = KEYS[1], ARGV[1], ARGV[2]
local capacity, field_bound = tonumber(ARGV[3]), tonumber(ARGV[4])
local value_bound, reply_bound = tonumber(ARGV[5]), tonumber(ARGV[6])
local kind = redis.call('TYPE',key).ok
if kind ~= 'none' and kind ~= expected then return {-1} end
-- Integer replies carry codes only; payload and score values stay strings.
local function numeric_error(result, overflow)
 if type(result) ~= 'table' or not result.err then return nil end
 for _,message in ipairs(overflow) do
  if string.find(result.err, message, 1, true) then return {-2} end
 end
 return redis.error_reply(result.err)
end

-- Shared integer wire checks; Redis performs the signed 64-bit arithmetic.
local function canonical_integer(value)
    return value == '0' or string.match(value, '^%-?[1-9][0-9]*$') ~= nil
end
local function integer_error(result)
    if type(result) ~= 'table' or not result.err then return nil end
    if result.err == 'ERR increment or decrement would overflow' or
       result.err == 'ERR value is not an integer or out of range' or
       result.err == 'ERR hash value is not an integer' then return {-1} end
    return redis.error_reply(result.err)
end

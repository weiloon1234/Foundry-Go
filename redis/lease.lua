-- One exact key; fixed-size owners; a single mutation after all checks.
local key, op, owner, ttl, size = KEYS[1], ARGV[1], ARGV[2], ARGV[3], tonumber(ARGV[4])
local current, status = read_lease_owner(key, size)
if status == -1 then return -1 end
if status == 1 then
    if op == 'acquire' or current ~= owner then return 0 end
elseif op ~= 'acquire' then
    return 0
end
if op == 'acquire' then
    redis.call('SET', key, owner, 'NX', 'PX', ttl)
elseif op == 'renew' then
    redis.call('PEXPIRE', key, ttl)
elseif op == 'release' then
    redis.call('DEL', key)
elseif op == 'transfer' then
    -- Single-use token restore: the next owner replaces the current one.
    if #ARGV[5] ~= size or ARGV[5] == string.rep(string.char(0), size) then return -1 end
    redis.call('SET', key, ARGV[5], 'XX', 'PX', ttl)
else
    return -1
end
return 1

-- Cache arguments stay unchanged; two trailing arguments identify the lease.
local failure = require_fill_lease(KEYS[2], ARGV[5], tonumber(ARGV[6]))
if failure then return failure end

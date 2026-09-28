-- Check ownership before any metadata/payload mutation, then use the shared tag
-- compiler inputs. The final key and two final arguments belong to the lease.
local failure = require_fill_lease(KEYS[#KEYS], ARGV[#ARGV-1], tonumber(ARGV[#ARGV]))
if failure then return failure end
table.remove(KEYS)
table.remove(ARGV)
table.remove(ARGV)

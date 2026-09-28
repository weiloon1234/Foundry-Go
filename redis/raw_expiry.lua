-- One mutation after the missing-key check; expiry never replaces contents.
if redis.call('EXISTS',KEYS[1]) == 0 then return 0 end
redis.call(unpack(entry_expiry(KEYS[1],ARGV[1])))
return 1

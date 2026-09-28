-- The Go adapter supplies canonical addresses and one declared kind per key.
-- Check every key before one DEL; a wrong type cannot cause partial deletion.
for i,key in ipairs(KEYS) do
 local kind = redis.call('TYPE',key).ok
 if kind ~= 'none' and kind ~= ARGV[i] then return {-1} end
end
return {1,redis.call('DEL',unpack(KEYS))}

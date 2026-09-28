-- Shared finite/persistent expiry command selection.
local function entry_expiry(key, milliseconds)
    if milliseconds == '0' then return {'PERSIST', key} end
    return {'PEXPIRE', key, milliseconds}
end

package lock

// compareStateAndMutateScript atomically mutates a Lock only when it is
// still held by the expected PR *and* still in the expected state.
//
// The state check is what lets the transition table stay in Go. The caller
// reads the Lock, computes the edge, and writes back under the state it
// read; a concurrent Operation that moved the Lock in between is reported
// rather than overwritten, and the caller re-reads and recomputes.
//
// KEYS[1] = "lock:{projectKey}"
// ARGV[1] = expected pr_number (string)
// ARGV[2] = expected state, exactly as stored ("" for a Lock written
//
//	before the field existed)
//
// ARGV[3] = "set" or "del"
// ARGV[4] = new LockData JSON (only used when ARGV[3] == "set")
//
// Returns 1 on success, 0 if no Lock exists, -1 if held by a different PR,
// -2 if the state moved under the caller.
const compareStateAndMutateScript = `
local existing = redis.call('GET', KEYS[1])
if not existing then
    return 0
end
local data = cjson.decode(existing)
if tostring(data.pr_number) ~= ARGV[1] then
    return -1
end
local current = data.state
if current == nil then
    current = ''
end
if current ~= ARGV[2] then
    return -2
end
if ARGV[3] == 'del' then
    redis.call('DEL', KEYS[1])
else
    redis.call('SET', KEYS[1], ARGV[4])
end
return 1
`

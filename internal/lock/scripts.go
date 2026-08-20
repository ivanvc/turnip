package lock

// acquireScript atomically acquires a Lock, creating it if absent or
// succeeding idempotently if already held by the requesting PR.
//
// KEYS[1] = "lock:{projectKey}"
// ARGV[1] = requesting pr_number (string)
// ARGV[2] = new LockData JSON (used only when creating)
//
// Returns 1 on success (created or idempotent re-acquire), 0 if held by a
// different PR.
const acquireScript = `
local existing = redis.call('GET', KEYS[1])
if not existing then
    redis.call('SET', KEYS[1], ARGV[2])
    return 1
end
local data = cjson.decode(existing)
if tostring(data.pr_number) == ARGV[1] then
    return 1
end
return 0
`

// compareAndMutateScript atomically mutates (sets or deletes) a Lock only
// when it is currently held by the expected PR.
//
// KEYS[1] = "lock:{projectKey}"
// ARGV[1] = expected pr_number (string)
// ARGV[2] = "set" or "del"
// ARGV[3] = new LockData JSON (only used when ARGV[2] == "set")
//
// Returns 1 on success, 0 if no lock exists, -1 if held by a different PR.
const compareAndMutateScript = `
local existing = redis.call('GET', KEYS[1])
if not existing then
    return 0
end
local data = cjson.decode(existing)
if tostring(data.pr_number) ~= ARGV[1] then
    return -1
end
if ARGV[2] == 'del' then
    redis.call('DEL', KEYS[1])
else
    redis.call('SET', KEYS[1], ARGV[3])
end
return 1
`

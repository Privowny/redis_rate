package redis_rate

import "github.com/go-redis/redis/v8"

// Copyright (c) 2017 Pavel Pravosud
// https://github.com/rwz/redis-gcra/blob/master/vendor/perform_gcra_ratelimit.lua
var allowN = redis.NewScript(`
-- this script has side-effects, so it requires replicate commands mode
-- redis-cli --ldb --eval allown.lua mykey , 3 3 120 1
redis.replicate_commands()
local rate_limit_key = KEYS[1]
local burst = ARGV[1]
local rate = ARGV[2]
local period = ARGV[3]
local cost = tonumber(ARGV[4])
local emission_interval = period / rate
local increment = emission_interval * cost
local burst_offset = emission_interval * burst
-- redis returns time as an array containing two integers: seconds of the epoch
-- time (10 digits) and microseconds (6 digits). for convenience we need to
-- convert them to a floating point number. the resulting number is 16 digits,
-- bordering on the limits of a 64-bit double-precision floating point number.
-- adjust the epoch to be relative to Jan 1, 2017 00:00:00 GMT to avoid floating
-- point problems. this approach is good until "now" is 2,483,228,799 (Wed, 09
-- Sep 2048 01:46:39 GMT), when the adjusted value is 16 digits.
local jan_1_2017 = 1483228800
local now = redis.call("TIME")
now = (now[1] - jan_1_2017) + (now[2] / 1000000)
local tat = redis.call("GET", rate_limit_key)
if not tat then
  tat = now
else
  tat = tonumber(tat)
end
tat = math.max(tat, now)

-- redis.debug("emission_interval",emission_interval)
-- redis.debug("increment",increment)
-- redis.debug("burst_offset",burst_offset)
-- redis.debug("tat",tostring(tat))

local new_tat = tat + increment
local allow_at = new_tat - burst_offset
local diff = now - allow_at
local remaining = diff / emission_interval

-- redis.debug("new_tat", tostring(new_tat))
-- redis.debug("allow_at",tostring(allow_at))
-- redis.debug("diff",diff)
-- redis.debug("remaining",remaining)

if remaining < 0 then
  local reset_after = tat - now
  local retry_after = diff * -1

  -- redis.debug("---A")
  -- redis.debug("allowed",0)
  -- redis.debug("remaining",0)
  -- redis.debug("retry_after",retry_after)
  -- redis.debug("reset_after",reset_after)

  return {
    0, -- allowed
    0, -- remaining
    tostring(retry_after),
    tostring(reset_after),
  }
end

local reset_after = new_tat - now
if reset_after > 0 then
  redis.call("SET", rate_limit_key, new_tat, "EX", math.ceil(reset_after))
end

local retry_after = emission_interval * (1 - remaining)
if retry_after < 0 then
  retry_after = 0
end

-- redis.debug("---B")
-- redis.debug("allowed",cost)
-- redis.debug("remaining",remaining)
-- redis.debug("retry_after",tostring(retry_after))
-- redis.debug("reset_after",tostring(reset_after))

return {cost, remaining, tostring(retry_after), tostring(reset_after)}
`)

var allowAtMost = redis.NewScript(`
-- this script has side-effects, so it requires replicate commands mode
redis.replicate_commands()

local rate_limit_key = KEYS[1]
local burst = ARGV[1]
local rate = ARGV[2]
local period = ARGV[3]
local cost = tonumber(ARGV[4])

local emission_interval = period / rate
local burst_offset = emission_interval * burst

-- redis returns time as an array containing two integers: seconds of the epoch
-- time (10 digits) and microseconds (6 digits). for convenience we need to
-- convert them to a floating point number. the resulting number is 16 digits,
-- bordering on the limits of a 64-bit double-precision floating point number.
-- adjust the epoch to be relative to Jan 1, 2017 00:00:00 GMT to avoid floating
-- point problems. this approach is good until "now" is 2,483,228,799 (Wed, 09
-- Sep 2048 01:46:39 GMT), when the adjusted value is 16 digits.
local jan_1_2017 = 1483228800
local now = redis.call("TIME")
now = (now[1] - jan_1_2017) + (now[2] / 1000000)

local tat = redis.call("GET", rate_limit_key)

if not tat then
  tat = now
else
  tat = tonumber(tat)
end

tat = math.max(tat, now)

local diff = now - (tat - burst_offset)
local remaining = diff / emission_interval

if remaining < 1 then
  local reset_after = tat - now
  local retry_after = emission_interval - diff
  return {
    0, -- allowed
    0, -- remaining
    tostring(retry_after),
    tostring(reset_after),
  }
end

if remaining < cost then
  cost = remaining
  remaining = 0
else
  remaining = remaining - cost
end

local increment = emission_interval * cost
local new_tat = tat + increment

local reset_after = new_tat - now
if reset_after > 0 then
  redis.call("SET", rate_limit_key, new_tat, "EX", math.ceil(reset_after))
end

return {
  cost,
  remaining,
  tostring(-1),
  tostring(reset_after),
}
`)

-- Tomislav-RetCtx: wrk2 workload for the Blueprint Social Network Wrk2API with DeathStarBench's
-- request mix (socialNetwork/wrk2/scripts/social-network/mixed-workload.lua):
--   read home timeline 60%, read user timeline 30%, compose post 10%
-- Each request is built exactly as the single-type scripts build it (compose-post.lua,
-- read-home-timeline.lua, read-user-timeline.lua): same parameters, same Blueprint query-string
-- form, same RANDOM_SEED + thread-id seeding as compose-post.lua.  max_user_index (env) is the
-- seeded social graph's size (962 for the DSB graph the runner loads).
local socket = require("socket")

local thread_counter = 0
function setup(thread)
  thread_counter = thread_counter + 1
  thread:set("id", thread_counter)
end

function init(args)
  local seed_thread_id = 0
  if args and type(args) == "table" and #args > 0 then
    seed_thread_id = tonumber(args[1]) or 0
  end
  if seed_thread_id == 0 and wrk and wrk.thread then
    seed_thread_id = wrk.thread:get("id") or 0
  end
  if seed_thread_id == 0 then
    seed_thread_id = 1
    io.stderr:write("init: WARNING: fallback to thread_id=1\n")
  end
  local base_seed = tonumber(os.getenv("RANDOM_SEED"))
  if base_seed then
    math.randomseed(base_seed + seed_thread_id)
  else
    math.randomseed(socket.gettime() * 1000 + seed_thread_id)
  end
  math.random(); math.random(); math.random()
end

local charset = {'q', 'w', 'e', 'r', 't', 'y', 'u', 'i', 'o', 'p', 'a', 's',
  'd', 'f', 'g', 'h', 'j', 'k', 'l', 'z', 'x', 'c', 'v', 'b', 'n', 'm', 'Q',
  'W', 'E', 'R', 'T', 'Y', 'U', 'I', 'O', 'P', 'A', 'S', 'D', 'F', 'G', 'H',
  'J', 'K', 'L', 'Z', 'X', 'C', 'V', 'B', 'N', 'M', '1', '2', '3', '4', '5',
  '6', '7', '8', '9', '0'}
local decset_nozero = {'1', '2', '3', '4', '5', '6', '7', '8', '9'}
local decset = {'1', '2', '3', '4', '5', '6', '7', '8', '9', '0'}
local max_user_index = tonumber(os.getenv("max_user_index")) or 962

local function stringRandom(length)
  local out = {}
  for i = 1, length do
    out[i] = charset[math.random(1, #charset)]
  end
  return table.concat(out)
end

local function decRandomNoLeadingZero(length)
  local out = {decset_nozero[math.random(1, #decset_nozero)]}
  for i = 2, length do
    out[i] = decset[math.random(1, #decset)]
  end
  return table.concat(out)
end

local function urlencode(str)
  if str == nil then
    return ""
  end
  str = string.gsub(str, "\n", "\r\n")
  str = string.gsub(str, "([^%w%-%.%_%~])", function(c)
    return string.format("%%%02X", string.byte(c))
  end)
  return str
end

local function compose_post()
  local user_index = math.random(1, max_user_index - 1)
  local username = "username_" .. tostring(user_index)
  local user_id = user_index
  local text = stringRandom(256)
  local num_user_mentions = math.random(0, 5)
  local num_urls = math.random(0, 5)
  local num_media = math.random(0, 4)
  for i = 0, num_user_mentions, 1 do
    local user_mention_id
    while (true) do
      user_mention_id = math.random(0, max_user_index - 1)
      if user_index ~= user_mention_id then
        break
      end
    end
    text = text .. " @username_" .. tostring(user_mention_id)
  end
  for i = 0, num_urls, 1 do
    text = text .. " http://" .. stringRandom(64)
  end
  local media_ids_json = "["
  local media_types_json = "["
  for i = 0, num_media, 1 do
    local media_id = decRandomNoLeadingZero(18)
    if i > 0 then
      media_ids_json = media_ids_json .. ", "
      media_types_json = media_types_json .. ", "
    end
    media_ids_json = media_ids_json .. media_id
    media_types_json = media_types_json .. "\"png\""
  end
  media_ids_json = media_ids_json .. "]"
  media_types_json = media_types_json .. "]"
  local query = "username=" .. urlencode(username) ..
      "&userId=" .. urlencode(tostring(user_id)) ..
      "&text=" .. urlencode(text) ..
      "&post_type=" .. urlencode("0") ..
      "&media_ids=" .. urlencode(media_ids_json) ..
      "&media_types=" .. urlencode(media_types_json)
  local headers = {}
  headers["Content-Type"] = "application/x-www-form-urlencoded"
  return wrk.format("POST", "/ComposePost?" .. query, headers, "")
end

local function read_timeline(handler)
  local user_id = math.random(1, max_user_index - 1)
  local start = math.random(0, 100)
  local stop = start + 10
  local query = "userId=" .. urlencode(tostring(user_id)) ..
      "&start=" .. urlencode(tostring(start)) ..
      "&stop=" .. urlencode(tostring(stop))
  local headers = {}
  headers["Content-Type"] = "application/x-www-form-urlencoded"
  return wrk.format("GET", "/" .. handler .. "?" .. query, headers, nil)
end

request = function()
  local coin = math.random()
  if coin < 0.6 then
    return read_timeline("ReadHomeTimeline")
  elseif coin < 0.9 then
    return read_timeline("ReadUserTimeline")
  else
    return compose_post()
  end
end

response = function(status, headers, body)
  if status < 200 or status >= 400 then
    local preview = body
    if body and #body > 500 then
      preview = body:sub(1, 500) .. "... (truncated)"
    end
    io.stderr:write(string.format("ERROR: Status %d - Body: %s\n", status, preview or "(empty)"))
  end
end

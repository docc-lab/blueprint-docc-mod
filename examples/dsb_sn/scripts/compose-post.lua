local socket = require("socket")

-- Global counter to assign unique IDs to each thread
-- Note: This is shared across all threads, so each thread gets a unique sequential ID
local thread_counter = 0

function setup(thread)
  -- Increment the global counter and assign unique ID to this thread
  thread_counter = thread_counter + 1
  thread:set("id", thread_counter)
  
  io.stderr:write(string.format("setup: assigned thread_id=%d\n", thread_counter))
end

function init(args)
  -- Get thread ID from args if passed (highest priority)
  local seed_thread_id = 0
  if args and type(args) == "table" and #args > 0 then
    seed_thread_id = tonumber(args[1]) or 0
  end

  -- If no thread_id from args, retrieve from thread-local storage (set in setup)
  if seed_thread_id == 0 then
    if wrk and wrk.thread then
      seed_thread_id = wrk.thread:get("id") or 0
    end
  end
  
  -- Ensure we have a valid thread_id (should be set if setup ran)
  if seed_thread_id == 0 then
    seed_thread_id = 1  -- Fallback to 1 if somehow both args and thread storage failed
    io.stderr:write(string.format("init: WARNING: fallback to thread_id=1\n"))
  end
  
  local base_seed = tonumber(os.getenv("RANDOM_SEED"))
  io.stderr:write(string.format("init: final thread_id=%d, base_seed=%s\n", seed_thread_id, base_seed))
  if base_seed then
    math.randomseed(base_seed + seed_thread_id)
    wrk.thread:set("seed", base_seed + seed_thread_id)
  else
    math.randomseed(socket.gettime()*1000 + seed_thread_id)
  end
  -- Warm up the random number generator
  math.random(); math.random(); math.random()
end

local charset = {'q', 'w', 'e', 'r', 't', 'y', 'u', 'i', 'o', 'p', 'a', 's',
  'd', 'f', 'g', 'h', 'j', 'k', 'l', 'z', 'x', 'c', 'v', 'b', 'n', 'm', 'Q',
  'W', 'E', 'R', 'T', 'Y', 'U', 'I', 'O', 'P', 'A', 'S', 'D', 'F', 'G', 'H',
  'J', 'K', 'L', 'Z', 'X', 'C', 'V', 'B', 'N', 'M', '1', '2', '3', '4', '5',
  '6', '7', '8', '9', '0'}

local decset = {'1', '2', '3', '4', '5', '6', '7', '8', '9', '0'}
local decset_nozero = {'1', '2', '3', '4', '5', '6', '7', '8', '9'}  -- Digits 1-9 only (no leading zero)

-- load env vars
local max_user_index = tonumber(os.getenv("max_user_index")) or 962

-- Store the last request's JSON for debugging
local last_media_ids_json = nil
local last_media_types_json = nil

local function stringRandom(length)
  if length > 0 then
    return stringRandom(length - 1) .. charset[math.random(1, #charset)]
  else
    return ""
  end
end

local function decRandom(length)
  if length > 0 then
    return decRandom(length - 1) .. decset[math.random(1, #decset)]
  else
    return ""
  end
end

-- Generate a random decimal number of given length, ensuring it doesn't start with 0
-- (required for valid JSON numbers)
local function decRandomNoLeadingZero(length)
  if length <= 0 then
    return ""
  end
  -- First digit must be 1-9, rest can be 0-9
  local firstDigit = decset_nozero[math.random(1, #decset_nozero)]  -- Pick from 1-9 only
  if length == 1 then
    return firstDigit
  else
    return firstDigit .. decRandom(length - 1)
  end
end

-- Simple URL encoding function
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

request = function()
  -- wrk_thread_id = wrk.thread:get("id")
  -- wrk_seed = wrk.thread:get("seed")
  -- io.stderr:write(string.format("request: wrk_thread_id=%d, wrk_seed=%d\n", wrk_thread_id, wrk_seed))
  -- user_id must be > 0 (server validation rejects userId == 0)
  -- Ensure user_index is at least 1 to keep username and user_id consistent
  local user_index = math.random(1, max_user_index - 1)
  local username = "username_" .. tostring(user_index)
  local user_id = user_index
  local text = stringRandom(256)
  local num_user_mentions = math.random(0, 5)
  local num_urls = math.random(0, 5)
  local num_media = math.random(0, 4)
  local media_ids_json = "["
  local media_types_json = "["

  -- Add user mentions to text
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

  -- Add URLs to text
  for i = 0, num_urls, 1 do
    text = text .. " http://" .. stringRandom(64)
  end

  -- Generate media IDs and types as JSON arrays
  -- Build arrays with proper JSON formatting (numbers unquoted, strings quoted)
  for i = 0, num_media, 1 do
    -- Use decRandomNoLeadingZero to ensure valid JSON numbers (no leading zeros)
    local media_id = decRandomNoLeadingZero(18)
    if i > 0 then
      media_ids_json = media_ids_json .. ", "
      media_types_json = media_types_json .. ", "
    end
    -- media_id is already a string from decRandom, use it directly as JSON number
    media_ids_json = media_ids_json .. media_id
    media_types_json = media_types_json .. "\"png\""
  end
  media_ids_json = media_ids_json .. "]"
  media_types_json = media_types_json .. "]"

  -- Store for debugging in response handler
  last_media_ids_json = media_ids_json
  last_media_types_json = media_types_json

  -- Build query string with JSON-encoded parameters for non-string types
  -- Blueprint expects query parameters, not form data
  local query = "username=" .. urlencode(username) ..
      "&userId=" .. urlencode(tostring(user_id)) ..
      "&text=" .. urlencode(text) ..
      "&post_type=" .. urlencode("0") ..
      "&media_ids=" .. urlencode(media_ids_json) ..
      "&media_types=" .. urlencode(media_types_json)

  local method = "POST"
  local path = "/ComposePost?" .. query
  local headers = {}
  headers["Content-Type"] = "application/x-www-form-urlencoded"

  return wrk.format(method, path, headers, "")
end

response = function(status, headers, body)
  -- Log errors for non-2xx/3xx responses
  if status < 200 or status >= 400 then
    -- Truncate body if it's too long (first 500 chars)
    local body_preview = body
    if body and #body > 500 then
      body_preview = body:sub(1, 500) .. "... (truncated)"
    end
    -- Include the JSON that was sent for debugging
    local debug_info = ""
    if last_media_ids_json then
      debug_info = string.format(" | media_ids: %s | media_types: %s", last_media_ids_json, last_media_types_json)
    end
    io.stderr:write(string.format("ERROR: Status %d - Body: %s%s\n", status, body_preview or "(empty)", debug_info))
  end
  -- Clear stored values after logging
  last_media_ids_json = nil
  last_media_types_json = nil
end

local socket = require("socket")
math.randomseed(socket.gettime()*1000)
math.random(); math.random(); math.random()

-- load env vars
local max_user_index = tonumber(os.getenv("max_user_index")) or 962

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
  -- user_id must be > 0 (server validation rejects userId == 0)
  -- Ensure user_index is at least 1
  local user_index = math.random(1, max_user_index - 1)
  local user_id = user_index
  local start = math.random(0, 100)
  local stop = start + 10

  -- Build query string with JSON-encoded parameters for non-string types
  -- Blueprint expects query parameters, not form data
  -- All parameters are int64, so they need to be JSON-encoded
  local query = "userId=" .. urlencode(tostring(user_id)) ..
      "&start=" .. urlencode(tostring(start)) ..
      "&stop=" .. urlencode(tostring(stop))

  local method = "GET"
  local path = "/ReadHomeTimeline?" .. query
  local headers = {}
  headers["Content-Type"] = "application/x-www-form-urlencoded"

  return wrk.format(method, path, headers, nil)
end

response = function(status, headers, body)
  -- Log errors for non-2xx/3xx responses
  if status < 200 or status >= 400 then
    -- Truncate body if it's too long (first 500 chars)
    local body_preview = body
    if body and #body > 500 then
      body_preview = body:sub(1, 500) .. "... (truncated)"
    end
    io.stderr:write(string.format("ERROR: Status %d - Body: %s\n", status, body_preview or "(empty)"))
  end
end


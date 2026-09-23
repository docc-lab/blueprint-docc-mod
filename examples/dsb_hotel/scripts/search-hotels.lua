-- Tomislav-RetCtx: wrk2 workload for the Blueprint HotelReservation frontend: SearchHandler only,
-- the request type with the largest call depth and the most RPCs
--   frontend -> search -> {geo, rate}; frontend -> reservation; frontend -> profile
-- (5 RPCs, 11 spans), the HotelReservation counterpart of Social Network's compose-post.lua.
--
-- Parameters follow DeathStarBench's own search request (hotelReservation/wrk2/scripts/
-- hotel-reservation/mixed-workload_type_1.lua, search_hotel): check-in 2015-04-09..23, check-out
-- after check-in up to 2015-04-24, lat 38.0235 +/- 0.2405, lon -122.095 -0.157/+0.168.
-- Blueprint's HTTP server reads every argument from the query string by its Go parameter name;
-- non-string arguments are JSON-decoded (lat/lon are JSON numbers). customerName and locale are
-- left empty exactly as DSB sends them (the handler defaults locale to "en").
--
-- Seeding and thread-id handling are the same as compose-post.lua: RANDOM_SEED + thread id.
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

local function day(d)
  if d <= 9 then
    return "2015-04-0" .. tostring(d)
  end
  return "2015-04-" .. tostring(d)
end

request = function()
  local in_date = math.random(9, 23)
  local out_date = math.random(in_date + 1, 24)
  local lat = 38.0235 + (math.random(0, 481) - 240.5) / 1000.0
  local lon = -122.095 + (math.random(0, 325) - 157.0) / 1000.0
  local path = "/SearchHandler?inDate=" .. day(in_date) .. "&outDate=" .. day(out_date) ..
    "&lat=" .. string.format("%.4f", lat) .. "&lon=" .. string.format("%.4f", lon)
  return wrk.format("GET", path, {}, nil)
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

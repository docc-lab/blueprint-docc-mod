-- Tomislav-RetCtx: wrk2 workload for the Blueprint HotelReservation frontend with DeathStarBench's
-- request mix (hotelReservation/wrk2/scripts/hotel-reservation/mixed-workload_type_1.lua):
--   search 60%, recommend 39%, user login 0.5%, reserve 0.5%
-- on Blueprint's query-string handlers (every argument by its Go parameter name; numbers as JSON):
--   GET  /SearchHandler?inDate&outDate&lat&lon                    frontend -> search -> {geo, rate}; -> reservation; -> profile
--   GET  /RecommendHandler?require&lat&lon                         frontend -> recommendation; -> profile
--   POST /UserHandler?username&password                            frontend -> user
--   POST /ReservationHandler?inDate&outDate&hotelId&customerName&username&password&roomNumber
--                                                                  frontend -> user; -> reservation
-- Parameters are DSB's (search-hotels.lua has the search ones).  Reservations and logins must not
-- fail (a handler error is an HTTP 500, which the campaign rejects), so the user and hotel ranges
-- come from the environment and must match what is seeded:
--   max_user_index   highest Cornell_<i> index, default 500 (the constructor's users 0..500;
--                    seed_hotels.js USERS adds more); password = the index repeated ten times
--   max_hotel_id     highest hotel id, default 80 (seed_hotels.js HOTELS)
-- Reserve books one room for 1..5 nights at a uniformly drawn hotel; with the stock 200-300 rooms
-- per hotel and 0.5% of requests, a 30 s point at 20k req/s books ~3000 room-nights spread over
-- all hotels and nights, far below capacity even without the seeded dataset.
-- Seeding and thread-id handling are the same as compose-post.lua / search-hotels.lua: RANDOM_SEED + thread id.
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

local max_user_index = tonumber(os.getenv("max_user_index")) or 500
local max_hotel_id = tonumber(os.getenv("max_hotel_id")) or 80

local function day(d)
  if d <= 9 then
    return "2015-04-0" .. tostring(d)
  end
  return "2015-04-" .. tostring(d)
end

local function coordinates()
  local lat = 38.0235 + (math.random(0, 481) - 240.5) / 1000.0
  local lon = -122.095 + (math.random(0, 325) - 157.0) / 1000.0
  return string.format("%.4f", lat), string.format("%.4f", lon)
end

local function user()
  local id = math.random(0, max_user_index)
  local password = ""
  for i = 1, 10 do
    password = password .. tostring(id)
  end
  return "Cornell_" .. tostring(id), password
end

local function search_hotel()
  local in_date = math.random(9, 23)
  local out_date = math.random(in_date + 1, 24)
  local lat, lon = coordinates()
  local path = "/SearchHandler?inDate=" .. day(in_date) .. "&outDate=" .. day(out_date) ..
    "&lat=" .. lat .. "&lon=" .. lon
  return wrk.format("GET", path, {}, nil)
end

local function recommend()
  local coin = math.random()
  local require_param = "price"
  if coin < 0.33 then
    require_param = "dis"
  elseif coin < 0.66 then
    require_param = "rate"
  end
  local lat, lon = coordinates()
  local path = "/RecommendHandler?require=" .. require_param .. "&lat=" .. lat .. "&lon=" .. lon
  return wrk.format("GET", path, {}, nil)
end

local function user_login()
  local username, password = user()
  local path = "/UserHandler?username=" .. username .. "&password=" .. password
  return wrk.format("POST", path, {}, "")
end

local function reserve()
  local in_date = math.random(9, 23)
  local out_date = math.min(in_date + math.random(1, 5), 24)
  local hotel_id = tostring(math.random(1, max_hotel_id))
  local username, password = user()
  local path = "/ReservationHandler?inDate=" .. day(in_date) .. "&outDate=" .. day(out_date) ..
    "&hotelId=" .. hotel_id .. "&customerName=" .. username .. "&username=" .. username ..
    "&password=" .. password .. "&roomNumber=1"
  return wrk.format("POST", path, {}, "")
end

request = function()
  local coin = math.random()
  if coin < 0.6 then
    return search_hotel()
  elseif coin < 0.99 then
    return recommend()
  elseif coin < 0.995 then
    return user_login()
  else
    return reserve()
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

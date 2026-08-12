-- payload.lua : drive the PayloadBench EdgeService.Call(reqSize, resSize) endpoint.
--
-- The Blueprint http plugin exposes the method as:
--     GET /Call?reqSize=<int>&resSize=<int>
-- (200 => OK, 500 => not-OK). This script draws reqSize (forward-path A->B) and
-- resSize (return-path B->A) per request from independently-configurable
-- distributions, and can optionally shape the inter-arrival process. Everything
-- is configured via environment variables so a single script covers fixed-size
-- sweeps and mixed/realistic loads.
--
-- ---------------------------------------------------------------------------
-- SIZE distributions.  Prefix REQ_ = forward payload, RES_ = return payload.
--   <P>_DIST = fixed | uniform | normal | exp | zipf     (default: fixed)
--     fixed    : <P>_SIZE                       bytes (const)
--     uniform  : <P>_MIN <P>_MAX                bytes ~ U[min,max]
--     normal   : <P>_MEAN <P>_STD               bytes ~ N(mean,std), clamped >=0 (and <=MAX if set)
--     exp      : <P>_MEAN                        bytes ~ Exp(mean), clamped (and <=MAX if set)
--     zipf     : <P>_MIN <P>_MAX <P>_N <P>_S     rank k in 1..N with P(k) ~ 1/k^S,
--                                                mapped linearly onto [MIN,MAX] (heavy-tail)
--   <P>_MIN / <P>_MAX also act as clamps for normal/exp when set.
--
-- ARRIVAL process.  Prefix ARR_.
--   ARR_DIST = constant | exp | uniform | lognormal      (default: constant)
--     constant  : delay() is NOT overridden -> use wrk2 -R (coordinated-omission corrected).
--     exp       : Poisson arrivals. Per-connection mean interval (ms) =
--                 ARR_MEAN_MS, or derived as ARR_CONNS*1000/ARR_RATE if ARR_MEAN_MS unset.
--     uniform   : ARR_MIN_MS ARR_MAX_MS          per-connection think time (ms)
--     lognormal : ARR_MU ARR_SIGMA               ms, exp(N(mu,sigma)) -> bursty
--   NOTE: any stochastic ARR_DIST switches pacing to plain-wrk closed-loop
--   delay() semantics; wrk2's constant-rate CO correction no longer applies, so
--   -R is effectively unused in that mode. Use ARR_DIST=constant + -R for clean
--   CO-corrected constant-rate runs (the default).
--
-- Example (fixed 4 KiB forward, 64 KiB return, constant rate via -R):
--   REQ_DIST=fixed REQ_SIZE=4096 RES_DIST=fixed RES_SIZE=65536 \
--     wrk -t8 -c128 -d30s -R5000 -s payload.lua http://EDGE:PORT
--
-- Example (lognormal forward sizes, Poisson arrivals @ 3000 rps over 128 conns):
--   REQ_DIST=exp REQ_MEAN=8192 REQ_MAX=1048576 RES_DIST=fixed RES_SIZE=1024 \
--   ARR_DIST=exp ARR_RATE=3000 ARR_CONNS=128 \
--     wrk -t8 -c128 -d30s -s payload.lua http://EDGE:PORT
-- ---------------------------------------------------------------------------

local function strenv(k, d) local v = os.getenv(k); if v ~= nil and v ~= "" then return v end return d end
local function numenv(k, d) local v = os.getenv(k); return v and tonumber(v) or d end

-- Standard-normal draw (Box-Muller).
local function randn()
	local u1 = math.random()
	if u1 <= 0 then u1 = 1e-12 end
	local u2 = math.random()
	return math.sqrt(-2 * math.log(u1)) * math.cos(2 * math.pi * u2)
end

-- Build a byte-size sampler for prefix P ("REQ" or "RES").
local function build_size_sampler(p)
	local dist = strenv(p .. "_DIST", "fixed")
	local mn = numenv(p .. "_MIN", 0)
	local mx = numenv(p .. "_MAX", -1) -- -1 == uncapped
	local function clamp(x)
		if x < mn then x = mn end
		if mx >= 0 and x > mx then x = mx end
		if x < 0 then x = 0 end
		return math.floor(x + 0.5)
	end
	if dist == "fixed" then
		local s = numenv(p .. "_SIZE", numenv(p .. "_MEAN", 0))
		return function() return math.floor(s + 0.5) end
	elseif dist == "uniform" then
		return function() return clamp(mn + math.random() * (mx - mn)) end
	elseif dist == "normal" then
		local mean = numenv(p .. "_MEAN", 0)
		local std = numenv(p .. "_STD", 0)
		return function() return clamp(mean + std * randn()) end
	elseif dist == "exp" then
		local mean = numenv(p .. "_MEAN", 0)
		return function() return clamp(-mean * math.log(1 - math.random())) end
	elseif dist == "bimodal" then
		-- Heavy-tailed with a REALISTIC mean: mostly <P>_SMALL, occasionally
		-- <P>_LARGE with probability <P>_P_LARGE.
		--
		-- This is the shape that makes the "does payload-size variance inflate
		-- latency variance?" question testable at realistic sizes. Size CV can be
		-- large while the mean stays small -- e.g. 95% x 1KiB + 5% x 64KiB has
		-- mean 4.25 KB (realistic for RPC traffic) but CV_size = 3.3, and the tail
		-- requests cost ~3x the service time of the small ones. By contrast a
		-- uniform +-20% spread around 1 KB leaves CV_size = 0.115 and moves
		-- service time <1%, which is why it measured nothing.
		--
		-- It is also the realistic shape: many small calls, rare large aggregate
		-- response, rather than a symmetric jitter around a central size.
		local plarge = numenv(p .. "_P_LARGE", 0.05)
		local small = numenv(p .. "_SMALL", 1024)
		local large = numenv(p .. "_LARGE", 65536)
		return function()
			if math.random() < plarge then return math.floor(large + 0.5) end
			return math.floor(small + 0.5)
		end
	elseif dist == "zipf" then
		local N = math.floor(numenv(p .. "_N", 100))
		local s = numenv(p .. "_S", 1.0)
		local cum, tot = {}, 0
		for k = 1, N do tot = tot + 1 / (k ^ s); cum[k] = tot end
		return function()
			local r = math.random() * tot
			local lo, hi = 1, N
			while lo < hi do
				local mid = math.floor((lo + hi) / 2)
				if cum[mid] < r then lo = mid + 1 else hi = mid end
			end
			local frac = (N > 1) and (lo - 1) / (N - 1) or 0
			local hi_b = (mx >= 0) and mx or mn
			return clamp(mn + frac * (hi_b - mn))
		end
	else
		error("unknown " .. p .. "_DIST: " .. dist)
	end
end

-- Build an inter-arrival (ms) sampler for stochastic ARR_DIST, or nil for constant.
local function build_arr_sampler()
	local d = strenv("ARR_DIST", "constant")
	if d == "constant" then
		return nil
	elseif d == "exp" then
		local mean = numenv("ARR_MEAN_MS", -1)
		if mean < 0 then
			local rate = numenv("ARR_RATE", 0)
			local conns = numenv("ARR_CONNS", 1)
			mean = (rate > 0) and (conns * 1000.0 / rate) or 0
		end
		return function() return -mean * math.log(1 - math.random()) end
	elseif d == "uniform" then
		local lo = numenv("ARR_MIN_MS", 0)
		local hi = numenv("ARR_MAX_MS", 0)
		return function() return lo + math.random() * (hi - lo) end
	elseif d == "lognormal" then
		local mu = numenv("ARR_MU", 0)
		local sigma = numenv("ARR_SIGMA", 0)
		return function() return math.exp(mu + sigma * randn()) end
	else
		error("unknown ARR_DIST: " .. d)
	end
end

-- Per-thread id, for decorrelated RNG seeds across threads.
local thread_counter = 0
function setup(thread)
	thread:set("tid", thread_counter)
	thread_counter = thread_counter + 1
end

local req_sampler, res_sampler
function init(args)
	math.randomseed(os.time() * 1000 + (tid or 0) * 7919)
	for _ = 1, 8 do math.random() end -- warm up
	req_sampler = build_size_sampler("REQ")
	res_sampler = build_size_sampler("RES")
	local arr = build_arr_sampler()
	if arr ~= nil then
		-- Only override delay() when a stochastic arrival process is requested,
		-- so the default (constant) path stays pure wrk2 -R pacing.
		delay = function() return arr() end
	end
end

function request()
	local req = req_sampler()
	local res = res_sampler()
	return wrk.format("GET", "/Call?reqSize=" .. req .. "&resSize=" .. res)
end


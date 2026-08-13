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
--
-- <P>_BASE (default 0) is added to EVERY draw. This exists so a bridge's baggage
-- distribution can ride on top of a fixed base payload: the base is the
-- application's own request size, the sampled part is the bridge metadata. Doing
-- it here (rather than folding the base into each sampler) keeps the bridge
-- distributions stated exactly as measured, in their own units.
local function build_size_sampler(p)
	local base = numenv(p .. "_BASE", 0)
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
	elseif dist == "pb" then
		-- P-BRIDGE baggage, checkpoint distance 6: constant 24 B (from simulated
		-- traces). Provided as a named dist so bridge arms read the same way.
		return function() return 24 end
	elseif dist == "cgpb" then
		-- CG-BRIDGE / CGPRB baggage, cpd 6, measured over 474,967,338 non-root
		-- calls in the Uber trace dataset. Exactly two values:
		--     P(24 B) = 89.30382072%   P(33 B) = 10.69617928%
		-- mean 24.963 B, sd 2.78 B, CV 0.11 -- i.e. essentially a constant, so
		-- this arm tests the same thing as pb at a marginally larger mean.
		return function()
			if math.random() < 0.1069617928 then return 33 end
			return 24
		end
	elseif dist == "sb" then
		-- S-BRIDGE baggage, cpd 6, full-width request-baggage distribution over
		-- 474,967,338 non-root calls (Uber trace dataset), as a bucketed
		-- histogram; a bucket is chosen by its probability, then a byte count is
		-- drawn UNIFORMLY inside it.
		--
		-- FIT vs the published summary (mean 34.547, p50 25, p99 114, p99.9 619,
		-- p99.99 3079, max 126484):
		--   tail is near-exact  -- p99 117, p99.9 644, p99.99 3084
		--   mean 32.95 (-4.6%), p50 30 (+5 B)
		-- The real data clusters on particular values inside the low buckets
		-- rather than spreading evenly, which uniform-within-bucket cannot
		-- reproduce. The tail governs the size-VARIANCE question this arm exists
		-- to test, and it is accurate there; the mean error is 1.6 B, i.e. 0.8%
		-- of the smallest base payload (200 B) and far below what this rig can
		-- resolve (measured: 24 B constant is unresolvable at <1.6%).
		--
		-- This is the ONLY bridge whose size genuinely varies: CV_size = 2.86,
		-- sd 94 B, with a p99.99 of ~3 KB -- 15x the p10 base request. That is
		-- the shape previously shown to inflate latency variance independently
		-- of the mean (notes/payload_size_variance_DOES_inflate_latency.md).
		local B = {
			{0,15,32.20425612},      {16,23,2.06216938},     {24,31,17.75406523},
			{32,47,30.96361776},     {48,63,14.65548543},    {64,95,1.05064530},
			{96,127,0.45652297},     {128,191,0.37805673},   {192,255,0.15228163},
			{256,383,0.12723212},    {384,511,0.06568873},   {512,767,0.05800799},
			{768,1023,0.02280052},   {1024,1535,0.02268809}, {1536,2047,0.00833005},
			{2048,3071,0.00811256},  {3072,4095,0.00341771}, {4096,6143,0.00269155},
			{6144,8191,0.00159569},  {8192,12287,0.00157506},{12288,16383,0.00044803},
			{16384,24575,0.00024107},{24576,32767,0.00003958},{32768,49151,0.00002274},
			{49152,65535,0.00000568},{65536,98303,0.00000168},{98304,131071,0.00000063},
		}
		local cum, tot = {}, 0
		for i = 1, #B do tot = tot + B[i][3]; cum[i] = tot end
		return function()
			local r = math.random() * tot
			local lo, hi = 1, #B
			while lo < hi do
				local mid = math.floor((lo + hi) / 2)
				if cum[mid] < r then lo = mid + 1 else hi = mid end
			end
			local b = B[lo]
			return b[1] + math.floor(math.random() * (b[2] - b[1] + 1))
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

-- Wrap build_size_sampler so <P>_BASE is applied uniformly to every dist.
local build_raw_sampler = build_size_sampler
build_size_sampler = function(p)
	local base = numenv(p .. "_BASE", 0)
	local f = build_raw_sampler(p)
	if base == 0 then return f end
	return function() return f() + base end
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


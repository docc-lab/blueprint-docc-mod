# HotelReservation: a larger dataset and a request mix without changing the application

Tomislav-RetCtx, 2026-10-29.  Background: the September campaigns drove HotelReservation with
SearchHandler only (`examples/dsb_hotel/scripts/search-hotels.lua`) against the constructors'
stock data: 80 hotels on a diagonal line in San Francisco, rate plans for 27 of them, one
reservation row, 501 users.  The workload's query box (lat 38.0235 +/- 0.2405, lon -122.095
-0.157/+0.168) barely touches those hotels: about half of all searches found nothing within 10 km,
22 distinct hotels were ever shown, and the real-work app cost only 42% of the no-work capacity
(Social Network: 82%).  The standing rule: the application is never modified to suit a campaign.

## What exists now

**Seeding before the services start** (`examples/dsb_hotel/scripts/seed_hotels.js`,
`utils/dsb_hotel_seed.py`).  The constructors insert their stock rows unconditionally, and geo,
recommendation and user read their collection once at construction, so extra data has to be in
MongoDB before a service starts, and nothing may restart afterwards (a restart re-inserts the
stock rows).  A derived campaign gets, per case:

- a ConfigMap with the mongo-shell script and a driver (`seed.sh`),
- a Job running the deployment's own pinned `mongo:4.4` image that waits for each of the six
  databases to answer and runs the script against it,
- a `wait-seed` init container on each of the six database-backed services that polls for the
  `seed_marker` document the script leaves in its database.

One `kubectl apply` still brings everything up in the right order; `ready()` ignores the finished
Job's pod, teardown deletes Jobs too.  The script is deterministic in (seed, hotel id): hotel i has
the same coordinates, rate, price and capacity in every database.  It writes the documents in the
field names the services query (the mongo-go-driver's default lowercasing of the workflow
structs), creates the indexes DeathStarBench's own services create (rate `hotelid`, profile `id`,
reservation `hotelid+indate+outdate`, number `hotelid`; `indexes=0` leaves the collections
unindexed, the deliberate "slow miss" knob), and records what it inserted per collection in the
marker; a second run against a seeded database inserts nothing.

What it seeds (`hotels=H,users=U,reservations=R`):

| collection | stock (constructors) | seeded | total |
|---|---|---|---|
| geo, profile, recommendation, number | ids 1..80 | ids 81..H, uniform over the query box | H |
| rate plans (inventory) | 27 (ids 1, 2, 3, every third of 7..80) | one RACK plan for every other id in 1..H | H |
| reservation rows | 1 | per hotel 1..H and night 2015-04-09..23: Poisson(R) rows of one room | 1 + Job's count |
| users | Cornell_0..500 | Cornell_501..U, password = index x10, sha256 hex | U + 1 |

Profiles get about 1 KB of description text and a full address.  `run_dsb_sn_nw.check_initialized`
expects stock + the marker's counts exactly, asserts the Job succeeded and no pod restarted, probes
a search at hotel 1 and sweeps the latitude bins as before (the geo index's race on first touch).

**Search coverage** (simulation over the seeded points, haversine, 10 km radius, 5 results):

| hotels | searches finding nothing | 5th-nearest hotel, median / max |
|---|---|---|
| stock 80 | 50% | 9.9 km / 28 km |
| 300 | 0% | 3.3 km / 6.3 km |
| 1000 | 0% | 1.6 km / 3.9 km |
| 2000 | 0% | 1.1 km / 2.6 km |
| 5000 | 0% | 0.7 km / 1.8 km |

**Request mixes** (`--workload`).  Hotel `mixed` = DeathStarBench's 60% search / 39% recommend /
0.5% login / 0.5% reserve on Blueprint's query-string handlers
(`examples/dsb_hotel/scripts/mixed-workload.lua`; spans per request 11 / 5 / 3 / 5); logins and
reservations draw from the seeded ranges (`max_user_index`, `max_hotel_id` from the seed spec) so
no request fails (a handler error is an HTTP 500, which the campaign rejects).  Social Network
`mixed` = DSB's 60% home timeline / 30% user timeline / 10% compose
(`examples/dsb_sn/scripts/mixed-workload.lua`), each request built exactly as the single-type
scripts build it.  The plan records the workload block; `run_wrk` runs it for warm-up, ramp and
knee windows alike.

## How to run

    utils/derive_dsb_sn_nw.py --source <hotel real-work source root> ... \
        --hotel-seed hotels=2000,users=5000,reservations=20 --workload mixed

`--hotel-seed` applies to the real-work hotel app only (the zero-work variant touches no
database); `--workload` to hotel (`search`, `mixed`) and Social Network (`compose`, `mixed`).
Both are recorded in plan.json and every case.json; `init-check.json` of every deployment records
the markers and the counts.

## Verified, and not

- `seed_hotels.js` and the Job's driver script were run against a local `mongo:4.4` container for
  all six databases (counts, field names, indexes, idempotence, the password hash against Python's
  sha256); `expected_counts` reconciles with the markers.  400 hotels with 30k reservation rows
  seed in 8 s.
- Unit tests: `utils/test_dsb_hotel_seed.py` (spec, manifest installation, counts, workloads,
  readiness), the existing runner suites unchanged.
- Not run on the cluster: it is idle and its registry is empty (fresh since October 9), so a
  deployment needs the images rebuilt and pushed first (`prepare_dsb_hotel.py`, then derive).  The
  first cluster run should be a smoke of one kind with `--hotel-seed` and `--workload mixed`.

## Levers not taken (data-only, available later)

Cache behaviour (hotspot draws in the Lua, memcached `-m` via `dsb_apps.cache_args`), the query
box or the date window of the Lua, the number of rate plans per hotel (one per hotel keeps search
results free of duplicates: `SearchService.Nearby` lists a hotel once per plan).

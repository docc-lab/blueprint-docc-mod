#!/usr/bin/env python3
"""Tomislav-RetCtx: per-application constants for the DSB campaign tooling.

The prepare / build / derive / run / analyze scripts were written for Social Network. Every
place where they assumed SN now reads this table through the campaign's `app` field (recorded
in plan.json and every case.json; absent = 'sn', so existing SN roots behave exactly as before).

  sn     Social Network, ComposePost (23 spans, 13 traced services).
  hotel  HotelReservation, SearchHandler (11 spans, 8 traced services): the request type with
         the largest call depth and the most RPCs --
           frontend -> search -> {geo, rate}; frontend -> reservation; frontend -> profile
         = 5 RPCs, 4 levels of span depth (root server span at depth 0, geo/rate servers at 4).
         Recommend is 2 RPCs / depth 2, Reservation 2 / 2, User 1 / 2.
"""
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
DSB_SN = REPO / 'examples/dsb_sn'
DSB_HOTEL = REPO / 'examples/dsb_hotel'
NODE0 = '10.10.1.1'

# Hotel seeding, counted from the service constructors (examples/dsb_hotel/workflow/
# hotelreservation/*Service.go, init*DB): 6 fixed hotels + ids 7..80; rate plans only for the
# 3 fixed ones plus every id in 7..80 divisible by 3 (24); users Cornell_0..Cornell_500.
# A service that restarted re-runs its initializer and duplicates rows, so these are EXACT
# expectations, checked after every deploy before any load is offered.
HOTEL_SEED = {
    'geo-db': [('geo-db', 'geo', 80)],
    'rate-db': [('rate-db', 'inventory', 27)],
    'profile-db': [('profile-db', 'hotels', 80)],
    'recomd-db': [('recommendation-db', 'recommendation', 80)],
    'reserv-db': [('reservation-db', 'reservation', 1), ('reservation-db', 'number', 80)],
    'user-db': [('user-db', 'user', 501)],
}

APPS = {
    'sn': {
        'name': 'DSB Social Network',
        'namespace': 'dsb-sn',
        'entry_prefix': 'wrk2api-',          # k8s name prefix of the HTTP entry service
        'entry_trace_service': 'wrk2api',    # substring of its Jaeger service name
        'nodeport': 23229,
        'lua': DSB_SN / 'scripts/compose-post.lua',
        'lua_env': {'max_user_index': '962'},
        'request': 'ComposePost',
        'spans_per_request': 23,
        'app_services': 13,
        'deployments': 25,                   # Blueprint deployments incl. jaeger + ES
        'backends': 12,                      # 5 mongo, 5 redis, jaeger, elasticsearch
        'discovery': {'cpd_min': 2, 'cpd_max': 6, 'reverse_policy': 'inverse_depth'},
        'source_dir': 'examples/dsb_sn',
        'seed': None,
        'cache_args': None,
    },
    'hotel': {
        'name': 'DSB HotelReservation',
        'namespace': 'dsb-hotel',
        'entry_prefix': 'frontend-service-',
        'entry_trace_service': 'frontend_service',
        'nodeport': 23230,
        'lua': DSB_HOTEL / 'scripts/search-hotels.lua',
        'lua_env': {},
        'request': 'SearchHandler',
        'spans_per_request': 11,
        'app_services': 8,
        'deployments': 19,                   # 8 services, 6 mongo, 3 memcached, jaeger, ES
        'backends': 11,
        # Paper section 5.2: CPD randomized between 2 and 4 for HotelReservation (the minimum
        # and maximum effective checkpoint distance of its topology).
        # Reverse routing: depth_cubic (user 2026-09-23; ported from the bridges repo).
        'discovery': {'cpd_min': 2, 'cpd_max': 4, 'reverse_policy': 'depth_cubic'},
        'source_dir': 'examples/dsb_hotel',
        'seed': HOTEL_SEED,
        # Tomislav-RetCtx (user 2026-09-23: the app must not fail requests): memcached 1.6's
        # default limit is 1024 connections. Blueprint's client (runtime/plugins/memcached,
        # gomemcache MaxIdleConns 1000, no cap on open connections) left 995 connections
        # established on reserv-cache at idle, so any concurrency burst got "ERROR Too many
        # open connections" (35,136 HTTP 500s at 22k req/s in the nt capacity probe). The
        # server limit is raised; threads (4 = the cache's cores) and memory stay at defaults.
        'cache_args': ['-c', '65536'],
    },
}


# Tomislav-RetCtx: zero-work HotelReservation (examples/dsb_hotel/workflow/hotelnw): same deployment,
# workload, spans per request and checkpoint policy as 'hotel'; the services touch no database, so
# nothing is seeded and check_initialized has nothing to verify.
APPS['hotelnw'] = dict(APPS['hotel'], seed={})

def app_name(record):
    """The app of a plan.json / case.json dict (older SN roots carry no field)."""
    return (record or {}).get('app', 'sn')


def app_of(record):
    return APPS[app_name(record)]


def entry_url(app):
    return f"http://{NODE0}:{app['nodeport']}"


def set_cache_args(documents, app):
    """Tomislav-RetCtx: apply the app's memcached server arguments to every cache Deployment
    (idempotent). Returns the number of cache containers set; 0 when the app has none."""
    args = app.get('cache_args')
    if not args:
        return 0
    count = 0
    for doc in documents:
        if doc and doc.get('kind') == 'Deployment' and '-cache-' in doc['metadata']['name']:
            for container in doc['spec']['template']['spec']['containers']:
                assert 'memcached' in container['image'], container['image']
                container['args'] = list(args)
                count += 1
    return count

// Tomislav-RetCtx: pre-seed one HotelReservation database with a larger, consistent dataset,
// BEFORE its service starts.  Runs in the legacy mongo shell (the mongo:4.4 image of the
// deployments), once per database:
//
//   mongo --host <db host> --quiet --eval "var TARGET='geo', HOTELS=2000, USERS=5000" seed_hotels.js
//
// The application is untouched: its service constructors still insert their stock rows (hotels
// 1..80, users Cornell_0..500, rate plans for hotels 1, 2, 3 and every third id from 7..80, one
// reservation, HotelNumber 1..80) when they start, beside what is seeded here, and geo /
// recommendation / user load their collections once at construction -- which is why this has to
// run first (utils/dsb_hotel_seed.py adds a Job that runs it and init containers that make the
// services wait for the marker it leaves).  Documents use the field names the services query,
// i.e. the mongo-go-driver's default mapping of the workflow structs (lowercased field names,
// examples/dsb_hotel/workflow/hotelreservation/DataModels.go).
//
// Parameters (--eval, every one optional):
//   TARGET              geo | rate | profile | recommendation | reservation | user   (required)
//   HOTELS              highest hotel id; ids 81..HOTELS are new (default 80: nothing new)
//   USERS               highest user index; Cornell_501..Cornell_USERS are new (default 500)
//   RES_ROWS_PER_NIGHT  mean per-night reservation rows per hotel, every hotel 1..HOTELS and every
//                       night 2015-04-09..23 (default 0: none)
//   SEED                generator seed (default 1); everything is deterministic in (SEED, hotel id)
//   INDEXES             1 (default) creates the indexes DeathStarBench's own services create
//                       (rate hotelid, profile id, reservation hotelid+indate+outdate, number hotelid)
//   LAT_MIN/LAT_MAX, LON_MIN/LON_MAX   where the new hotels go (default: the workload's query box,
//                       search-hotels.lua: lat 38.0235 +/- 0.2405, lon -122.095 -0.157/+0.168)
//   DESCRIPTION_BYTES   profile description length (default 1000)
//
// Every hotel id gets the same coordinates, rate and price in every database (per-id generator).
// A `seed_marker` collection (document _id "seed") records what was inserted per collection; the
// runner's init check expects stock + inserted rows exactly, and a second run of this script
// against a seeded database inserts nothing.

function param(name, fallback) { return typeof this[name] === 'undefined' ? fallback : this[name]; }
var target = param('TARGET', null);
if (!target) { throw new Error('TARGET is required'); }
var HOTEL_MAX = param('HOTELS', 80), USER_MAX = param('USERS', 500), RES_ROWS = param('RES_ROWS_PER_NIGHT', 0);
var SEED_VALUE = param('SEED', 1), MAKE_INDEXES = param('INDEXES', 1) ? true : false;
var LAT_MIN_V = param('LAT_MIN', 38.0235 - 0.2405), LAT_MAX_V = param('LAT_MAX', 38.0235 + 0.2405);
var LON_MIN_V = param('LON_MIN', -122.095 - 0.157), LON_MAX_V = param('LON_MAX', -122.095 + 0.168);
var DESC_BYTES = param('DESCRIPTION_BYTES', 1000);
var STOCK_HOTELS = 80, STOCK_USERS = 500;
var NIGHTS = ['2015-04-09', '2015-04-10', '2015-04-11', '2015-04-12', '2015-04-13', '2015-04-14', '2015-04-15',
              '2015-04-16', '2015-04-17', '2015-04-18', '2015-04-19', '2015-04-20', '2015-04-21', '2015-04-22',
              '2015-04-23', '2015-04-24'];

var DATABASES = {
    geo: {db: 'geo-db', collections: ['geo']},
    rate: {db: 'rate-db', collections: ['inventory']},
    profile: {db: 'profile-db', collections: ['hotels']},
    recommendation: {db: 'recommendation-db', collections: ['recommendation']},
    reservation: {db: 'reservation-db', collections: ['reservation', 'number']},
    user: {db: 'user-db', collections: ['user']},
};
if (!DATABASES[target]) { throw new Error('unknown TARGET ' + target); }
var database = db.getSiblingDB(DATABASES[target].db);

// --- deterministic generators -----------------------------------------------------------------
function mulberry32(a) {
    return function () {
        a |= 0; a = a + 0x6D2B79F5 | 0;
        var t = Math.imul(a ^ a >>> 15, 1 | a);
        t = t + Math.imul(t ^ t >>> 7, 61 | t) ^ t;
        return ((t ^ t >>> 14) >>> 0) / 4294967296;
    };
}
function hotelRandom(i) { return mulberry32(Math.imul(SEED_VALUE | 0, 0x9E3779B1) ^ Math.imul(i | 0, 0x85EBCA6B)); }
function nightRandom(i, night) { return mulberry32(Math.imul(SEED_VALUE | 0, 0xC2B2AE35) ^ Math.imul(i | 0, 0x27D4EB2F) ^ Math.imul(night + 1, 0x165667B1)); }
function userRandom(i) { return mulberry32(Math.imul(SEED_VALUE | 0, 0x1B873593) ^ Math.imul(i | 0, 0xCC9E2D51)); }
function poisson(rng, mean) {
    if (mean <= 0) { return 0; }
    var limit = Math.exp(-mean), k = 0, p = 1;
    do { k++; p *= rng(); } while (p > limit);
    return k - 1;
}
function pick(rng, list) { return list[Math.floor(rng() * list.length)]; }
function round2(x) { return Math.round(x * 100) / 100; }

var WORDS = ('the hotel offers quiet rooms with views of the bay and a short walk to the waterfront ' +
    'guests enjoy a rooftop terrace daily breakfast free parking and fast internet in every room ' +
    'the building dates from the early twentieth century and keeps its original brick facade ' +
    'nearby you find cafes galleries a farmers market and the regional train station ' +
    'suites include a kitchenette a work desk and blackout curtains for late sleepers ' +
    'the staff arrange bicycle rentals wine tours and tickets for the evening concerts ' +
    'a small gym a sauna and a heated pool are open to all guests until late ' +
    'pets are welcome on request and the garden courtyard is lit for dinner outside').split(' ');
var NAMES = ['Harbor', 'Summit', 'Orchard', 'Meridian', 'Lantern', 'Cypress', 'Granite', 'Willow', 'Beacon', 'Juniper',
             'Tidewater', 'Copper', 'Marigold', 'Sequoia', 'Pelican', 'Ridgeline', 'Saffron', 'Driftwood', 'Ember', 'Quarry'];
var KINDS = ['Hotel', 'Inn', 'Suites', 'Lodge', 'House', 'Resort'];
var STREETS = ['Market St', 'Mission St', 'Grand Ave', 'Shoreline Dr', 'Bay Rd', 'Park Blvd', 'Harbor Way',
               'Oak Ave', 'Lakeshore Ave', 'Main St', 'Willow Pass Rd', 'Contra Costa Blvd'];
var CITIES = ['Vallejo', 'Benicia', 'Martinez', 'Concord', 'Pleasant Hill', 'Walnut Creek', 'Richmond', 'Hercules',
              'Pinole', 'El Cerrito', 'Napa', 'American Canyon'];

// hotel i's fixed attributes, identical in every database
function hotel(i) {
    var rng = hotelRandom(i);
    var lat = LAT_MIN_V + rng() * (LAT_MAX_V - LAT_MIN_V), lon = LON_MIN_V + rng() * (LON_MAX_V - LON_MIN_V);
    var rate = round2(89 + rng() * 240), price = round2(rate * (0.8 + rng() * 0.6));
    var capacity = [200, 300, 250][i % 3];
    return {id: String(i), lat: Math.round(lat * 1e4) / 1e4, lon: Math.round(lon * 1e4) / 1e4, rate: rate, price: price,
            capacity: capacity, rng: rng};
}
function description(rng) {
    var text = '';
    while (text.length < DESC_BYTES) {
        var words = 8 + Math.floor(rng() * 7), sentence = [];
        for (var k = 0; k < words; k++) { sentence.push(pick(rng, WORDS)); }
        var s = sentence.join(' ');
        text += (text ? ' ' : '') + s.charAt(0).toUpperCase() + s.slice(1) + '.';
    }
    return text;
}
function stockRatePlan(i) { return i === 1 || i === 2 || i === 3 || (i >= 7 && i <= 80 && i % 3 === 0); }
function digits(rng, n) { var s = ''; for (var k = 0; k < n; k++) { s += Math.floor(rng() * 10); } return s; }

// --- SHA-256 (the user service compares sha256 hex digests of the password) ------------------
function sha256hex(ascii) {
    var K = [0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01,
        0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
        0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
        0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
        0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116, 0x1e376c08,
        0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
        0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2];
    var H = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
    var bytes = [];
    for (var i = 0; i < ascii.length; i++) { bytes.push(ascii.charCodeAt(i) & 0xff); }
    var bitLen = bytes.length * 8;
    bytes.push(0x80);
    while (bytes.length % 64 !== 56) { bytes.push(0); }
    for (var j = 7; j >= 0; j--) { bytes.push(j >= 4 ? 0 : (bitLen >>> (j * 8)) & 0xff); }
    function rotr(x, n) { return (x >>> n) | (x << (32 - n)); }
    var w = new Array(64);
    for (var off = 0; off < bytes.length; off += 64) {
        for (var t = 0; t < 16; t++) {
            w[t] = (bytes[off + t * 4] << 24) | (bytes[off + t * 4 + 1] << 16) | (bytes[off + t * 4 + 2] << 8) | bytes[off + t * 4 + 3];
        }
        for (t = 16; t < 64; t++) {
            var s0 = rotr(w[t - 15], 7) ^ rotr(w[t - 15], 18) ^ (w[t - 15] >>> 3);
            var s1 = rotr(w[t - 2], 17) ^ rotr(w[t - 2], 19) ^ (w[t - 2] >>> 10);
            w[t] = (w[t - 16] + s0 + w[t - 7] + s1) | 0;
        }
        var a = H[0], b = H[1], c = H[2], d = H[3], e = H[4], f = H[5], g = H[6], h = H[7];
        for (t = 0; t < 64; t++) {
            var S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25), ch = (e & f) ^ (~e & g);
            var temp1 = (h + S1 + ch + K[t] + w[t]) | 0;
            var S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22), maj = (a & b) ^ (a & c) ^ (b & c);
            var temp2 = (S0 + maj) | 0;
            h = g; g = f; f = e; e = (d + temp1) | 0; d = c; c = b; b = a; a = (temp1 + temp2) | 0;
        }
        H[0] = (H[0] + a) | 0; H[1] = (H[1] + b) | 0; H[2] = (H[2] + c) | 0; H[3] = (H[3] + d) | 0;
        H[4] = (H[4] + e) | 0; H[5] = (H[5] + f) | 0; H[6] = (H[6] + g) | 0; H[7] = (H[7] + h) | 0;
    }
    var hex = '';
    for (i = 0; i < 8; i++) { hex += ('00000000' + (H[i] >>> 0).toString(16)).slice(-8); }
    return hex;
}

// --- documents per target ------------------------------------------------------------------------
function newHotelIds() { var ids = []; for (var i = STOCK_HOTELS + 1; i <= HOTEL_MAX; i++) { ids.push(i); } return ids; }

function insertAll(collection, docs) {
    var n = 0;
    for (var off = 0; off < docs.length; off += 2000) {
        var chunk = docs.slice(off, off + 2000);
        if (chunk.length) { collection.insertMany(chunk, {ordered: false}); n += chunk.length; }
    }
    return n;
}

var inserted = {}, indexes = [];
var marker = database.getCollection('seed_marker');
if (marker.countDocuments({_id: 'seed'}) > 0) {
    print(JSON.stringify({target: target, skipped: 'already seeded', marker: marker.findOne({_id: 'seed'})}));
    quit(0);
}

if (target === 'geo') {
    var points = newHotelIds().map(function (i) { var h = hotel(i); return {pid: h.id, plat: h.lat, plon: h.lon}; });
    inserted.geo = insertAll(database.getCollection('geo'), points);
} else if (target === 'profile') {
    var profiles = newHotelIds().map(function (i) {
        var h = hotel(i), rng = h.rng;
        return {id: h.id, name: pick(rng, NAMES) + ' ' + pick(rng, KINDS) + ' ' + h.id,
                phonenumber: '(' + pick(rng, ['415', '510', '707', '925']) + ') ' + digits(rng, 3) + '-' + digits(rng, 4),
                description: description(rng),
                address: {streetnumber: String(1 + Math.floor(rng() * 2999)), streetname: pick(rng, STREETS),
                          city: pick(rng, CITIES), state: 'CA', country: 'United States',
                          postalcode: '94' + digits(rng, 3), lat: h.lat, lon: h.lon}};
    });
    inserted.hotels = insertAll(database.getCollection('hotels'), profiles);
    if (MAKE_INDEXES) { database.getCollection('hotels').createIndex({id: 1}); indexes.push('hotels.id'); }
} else if (target === 'recommendation') {
    var hotels = newHotelIds().map(function (i) { var h = hotel(i); return {hid: h.id, hlat: h.lat, hlon: h.lon, hrate: h.rate, hprice: h.price}; });
    inserted.recommendation = insertAll(database.getCollection('recommendation'), hotels);
} else if (target === 'rate') {
    // one RACK plan for every hotel that has none from the constructor, so every search result keeps its hotels
    var plans = [];
    for (var i = 1; i <= HOTEL_MAX; i++) {
        if (stockRatePlan(i)) { continue; }
        var h = hotel(i);
        plans.push({hotelid: h.id, code: 'RACK', indate: '2015-04-09', outdate: '2015-04-24',
                    rtype: {bookablerate: h.rate, code: 'KNG', roomdescription: 'King sized bed',
                            totalrate: h.rate, totalrateinclusive: round2(h.rate * 1.13)}});
    }
    inserted.inventory = insertAll(database.getCollection('inventory'), plans);
    if (MAKE_INDEXES) { database.getCollection('inventory').createIndex({hotelid: 1}); indexes.push('inventory.hotelid'); }
} else if (target === 'reservation') {
    var numbers = newHotelIds().map(function (i) { var h = hotel(i); return {hotelid: h.id, number: NumberLong(h.capacity)}; });
    inserted.number = insertAll(database.getCollection('number'), numbers);
    var rows = [], total = 0;
    for (i = 1; i <= HOTEL_MAX; i++) {
        for (var night = 0; night + 1 < NIGHTS.length; night++) {
            var rng = nightRandom(i, night), count = poisson(rng, RES_ROWS);
            for (var k = 0; k < count; k++) {
                rows.push({hotelid: String(i), customername: 'Cornell_' + Math.floor(rng() * (USER_MAX + 1)),
                           indate: NIGHTS[night], outdate: NIGHTS[night + 1], number: NumberLong(1)});
            }
            if (rows.length >= 20000) { total += insertAll(database.getCollection('reservation'), rows); rows = []; }
        }
    }
    total += insertAll(database.getCollection('reservation'), rows);
    inserted.reservation = total;
    if (MAKE_INDEXES) {
        database.getCollection('reservation').createIndex({hotelid: 1, indate: 1, outdate: 1}); indexes.push('reservation.hotelid+indate+outdate');
        database.getCollection('number').createIndex({hotelid: 1}); indexes.push('number.hotelid');
    }
} else if (target === 'user') {
    var users = [];
    for (i = STOCK_USERS + 1; i <= USER_MAX; i++) {
        var password = '';
        for (var j = 0; j < 10; j++) { password += String(i); }
        users.push({username: 'Cornell_' + i, password: sha256hex(password)});
    }
    inserted.user = insertAll(database.getCollection('user'), users);
}

var record = {_id: 'seed', target: target, hotels: HOTEL_MAX, users: USER_MAX, res_rows_per_night: RES_ROWS, seed: SEED_VALUE,
              box: {lat: [LAT_MIN_V, LAT_MAX_V], lon: [LON_MIN_V, LON_MAX_V]}, description_bytes: DESC_BYTES,
              inserted: inserted, indexes: indexes, seeded_at: new Date()};
marker.insertOne(record);
print(JSON.stringify({target: target, inserted: inserted, indexes: indexes}));

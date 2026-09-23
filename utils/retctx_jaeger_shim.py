#!/usr/bin/env python3
"""Tomislav-RetCtx: Jaeger query API served from ClickHouse.

The OpenTelemetry ClickHouse exporter writes spans to otel.otel_traces (one row per span,
attributes as Map(String, String), bytes attributes base64-encoded by pcommon.Value.AsString).
Every downstream tool in this campaign (runner trace sampling, spread sampler, eligibility,
pb_jaeger_recon) reads Jaeger's JSON (/api/services, /api/traces), so this process speaks that
API on :16686 and a Jaeger-collector-shaped /metrics on :14269 for the runner's snapshots.
Only the standard library is used; the container is python:3.12-slim.
"""
import json
import os
import re
import threading
import time
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

CH = os.environ.get('CLICKHOUSE_URL', 'http://localhost:8123')
DB = os.environ.get('CLICKHOUSE_DB', 'otel')
TABLE = os.environ.get('CLICKHOUSE_TABLE', 'otel_traces')
USER = os.environ.get('CLICKHOUSE_USER', 'default')
PASSWORD = os.environ.get('CLICKHOUSE_PASSWORD', '')
# Attributes the SDK sets as OTLP bytes; Jaeger reports those as type "binary" (base64 value),
# which is also exactly what the ClickHouse exporter stores. Everything else is a string.
BINARY_KEYS = set(k for k in os.environ.get('JAEGER_SHIM_BINARY_KEYS', '_br,_d').split(',') if k)
HEX = re.compile(r'^[0-9a-fA-F]{1,32}$')
KIND = {'Server': 'server', 'Client': 'client', 'Internal': 'internal', 'Producer': 'producer',
        'Consumer': 'consumer'}
PAD_US = 300 * 1_000_000  # span timestamps of a sampled trace lie within the query window +- 5 min


def ch(sql, timeout=120):
    url = CH + '/?' + urllib.parse.urlencode({'default_format': 'JSONEachRow'})
    request = urllib.request.Request(url, data=sql.encode(), method='POST',
                                     headers={'X-ClickHouse-User': USER, 'X-ClickHouse-Key': PASSWORD})
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=timeout) as response:
        return [json.loads(line) for line in response.read().decode().splitlines() if line.strip()]


def quoted(text):
    return "'" + text.replace('\\', '\\\\').replace("'", "\\'") + "'"


def parse_duration(text):
    match = re.match(r'^(\d+)([smhd])$', text or '1h')
    if not match:
        return 3600
    return int(match.group(1)) * {'s': 1, 'm': 60, 'h': 3600, 'd': 86400}[match.group(2)]


def services():
    return [row['ServiceName'] for row in ch(f'SELECT DISTINCT ServiceName FROM {DB}.{TABLE}')]


def find_traces(service, start_us, end_us, limit, order='recent'):
    """`limit` trace IDs with a span of `service` inside [start, end]. order=recent reproduces
    Jaeger (most recent first, i.e. a slice from the END of the window); order=random picks
    uniformly across the window by hashing the trace ID (Tomislav-RetCtx: Jaeger's 100-trace
    end-of-window slice was a preposterously small and biased sample)."""
    where = [f'ServiceName = {quoted(service)}',
             f'Timestamp >= fromUnixTimestamp64Micro({int(start_us)})',
             f'Timestamp <= fromUnixTimestamp64Micro({int(end_us)})']
    order_by = 'cityHash64(TraceId)' if order == 'random' else 't DESC'
    rows = ch(f"SELECT TraceId, max(Timestamp) AS t FROM {DB}.{TABLE} WHERE {' AND '.join(where)} "
              f"GROUP BY TraceId ORDER BY {order_by} LIMIT {int(limit)}")
    return [row['TraceId'] for row in rows]


def bounds_for(ids):
    rows = ch(f"SELECT toUnixTimestamp(min(Start)) AS lo, toUnixTimestamp(max(End)) AS hi "
              f"FROM {DB}.{TABLE}_trace_id_ts WHERE TraceId IN ({','.join(quoted(i) for i in ids)})")
    if rows and rows[0].get('lo') not in (None, 0, '0'):
        return (int(rows[0]['lo']) - 120) * 1_000_000, (int(rows[0]['hi']) + 120) * 1_000_000
    return None, None


def spans_for(ids, lo_us=None, hi_us=None):
    if not ids:
        return []
    where = [f"TraceId IN ({','.join(quoted(i) for i in ids)})"]
    if lo_us is not None and hi_us is not None:
        where += [f'Timestamp >= fromUnixTimestamp64Micro({int(lo_us)})',
                  f'Timestamp <= fromUnixTimestamp64Micro({int(hi_us)})']
    return ch(f"SELECT TraceId, SpanId, ParentSpanId, SpanName, SpanKind, ServiceName, ScopeName, "
              f"toUnixTimestamp64Micro(Timestamp) AS ts, Duration, SpanAttributes, ResourceAttributes "
              f"FROM {DB}.{TABLE} WHERE {' AND '.join(where)}")


def to_jaeger(rows):
    traces = {}
    for row in rows:
        trace = traces.setdefault(row['TraceId'], {'traceID': row['TraceId'], 'spans': [], 'processes': {},
                                                   'warnings': None, '_pids': {}})
        service = row['ServiceName']
        pid = trace['_pids'].get(service)
        if pid is None:
            pid = f"p{len(trace['_pids']) + 1}"
            trace['_pids'][service] = pid
            resource = row.get('ResourceAttributes') or {}
            trace['processes'][pid] = {'serviceName': service,
                                       'tags': [{'key': k, 'type': 'string', 'value': v}
                                                for k, v in sorted(resource.items()) if k != 'service.name']}
        tags = [{'key': k, 'type': 'binary' if k in BINARY_KEYS else 'string', 'value': v}
                for k, v in (row.get('SpanAttributes') or {}).items()]
        kind = KIND.get(row.get('SpanKind') or '')
        if kind:
            tags.append({'key': 'span.kind', 'type': 'string', 'value': kind})
        if row.get('ScopeName'):
            tags.append({'key': 'otel.scope.name', 'type': 'string', 'value': row['ScopeName']})
        parent = row.get('ParentSpanId') or ''
        references = [{'refType': 'CHILD_OF', 'traceID': row['TraceId'], 'spanID': parent}] if parent else []
        trace['spans'].append({'traceID': row['TraceId'], 'spanID': row['SpanId'], 'operationName': row['SpanName'],
                               'references': references, 'startTime': int(row['ts']),
                               'duration': int(row['Duration']) // 1000, 'tags': tags, 'logs': [],
                               'processID': pid, 'warnings': None})
    out = []
    for trace in traces.values():
        trace.pop('_pids')
        trace['spans'].sort(key=lambda s: s['startTime'])
        out.append(trace)
    return out


class Api(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *_):
        pass

    def reply(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        url = urllib.parse.urlsplit(self.path)
        query = urllib.parse.parse_qs(url.query)
        try:
            if url.path == '/api/services':
                return self.reply(200, {'data': services(), 'total': 0, 'limit': 0, 'offset': 0, 'errors': None})
            if url.path.startswith('/api/traces/'):
                trace_id = url.path.rsplit('/', 1)[1]
                if not HEX.match(trace_id):
                    return self.reply(400, {'data': None, 'errors': [{'code': 400, 'msg': 'bad trace id'}]})
                rows = spans_for([trace_id], *bounds_for([trace_id]))
                return self.reply(200 if rows else 404, {'data': to_jaeger(rows), 'total': 0, 'limit': 0,
                                                          'offset': 0, 'errors': None})
            if url.path == '/api/traces':
                ids = query.get('traceID') or query.get('traceId')
                if ids:
                    ids = [i for i in ids if HEX.match(i)]
                    rows = spans_for(ids, *bounds_for(ids))
                else:
                    service = query.get('service', [''])[0]
                    limit = int(query.get('limit', ['20'])[0])
                    start = query.get('start', [None])[0]
                    end = query.get('end', [None])[0]
                    if not (start and end):
                        end = int(time.time() * 1_000_000)
                        start = end - parse_duration(query.get('lookback', ['1h'])[0]) * 1_000_000
                    ids = find_traces(service, start, end, limit, query.get('order', ['recent'])[0])
                    rows = spans_for(ids, int(start) - PAD_US, int(end) + PAD_US)
                return self.reply(200, {'data': to_jaeger(rows), 'total': 0, 'limit': 0, 'offset': 0, 'errors': None})
            if url.path in ('/', '/health'):
                return self.reply(200, {'status': 'ok', 'clickhouse': CH})
            return self.reply(404, {'data': None, 'errors': [{'code': 404, 'msg': 'not found'}]})
        except Exception as error:  # surfaced to the caller as Jaeger would
            return self.reply(500, {'data': None, 'errors': [{'code': 500, 'msg': repr(error)}]})


class Metrics(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *_):
        pass

    def do_GET(self):
        up, saved = 1, 0
        try:
            saved = int(ch(f'SELECT count() AS c FROM {DB}.{TABLE}')[0]['c'])
        except Exception:
            up = 0
        body = ('# Tomislav-RetCtx: Jaeger-collector-shaped metrics from ClickHouse\n'
                f'jaeger_collector_spans_saved_by_svc_total {saved}\n'
                'jaeger_collector_queue_length 0\n'
                'jaeger_collector_queue_capacity 0\n'
                'jaeger_collector_spans_dropped_total 0\n'
                f'retctx_shim_clickhouse_up {up}\n').encode()
        self.send_response(200)
        self.send_header('Content-Type', 'text/plain; version=0.0.4')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    metrics = ThreadingHTTPServer(('0.0.0.0', int(os.environ.get('SHIM_METRICS_PORT', '14269'))), Metrics)
    threading.Thread(target=metrics.serve_forever, daemon=True).start()
    api = ThreadingHTTPServer(('0.0.0.0', int(os.environ.get('SHIM_API_PORT', '16686'))), Api)
    print(f'retctx jaeger shim: api :16686, metrics :14269, clickhouse {CH} db {DB} table {TABLE}', flush=True)
    api.serve_forever()


if __name__ == '__main__':
    main()

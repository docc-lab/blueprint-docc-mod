"""Tomislav-RetCtx: read the reverse-path (retCtx) payload off a stored span.

Two wire formats exist and both must stay readable, because every campaign run
before 2026-09-19 used the first one:

  legacy  key "bridges.checkpoint", value = base64(StdEncoding) of a JSON envelope
          {"fp":..,"m":..,"k":..,"segs":[{"k":"checkpoint.pb","d":base64(bytes)}]}
  packed  key "_rc", value = base64(RawURLEncoding) of
          0x01 || (tag || uvarint(len(body)) || body)*
          tag  = kind in the low 3 bits, 0x08 when a reverse TTL leads the body
          body = [ttl] || spanID(8) || uvarint(depth) || truss

The segment bytes are identical in both; only the container changed. The two are
told apart without guessing: the legacy value decodes under the standard base64
alphabet and starts with '{', the packed value uses the URL-safe alphabet and
starts with its version byte.
"""
import base64
import json

LEGACY_KEY = 'bridges.checkpoint'
PACKED_KEY = '_rc'
CHECKPOINT_KEYS = (PACKED_KEY, LEGACY_KEY)

# Removed from emission 2026-09-19 (nothing read it). Kept here so captures taken
# before that date remain interpretable.
LEGACY_FORWARD_UP_KEY = 'bridges.forward_up'

PACKED_VERSION = 1
PACKED_KIND_MASK = 0x07
PACKED_TTL_PRESENT = 0x08
PACKED_KINDS = {1: 'checkpoint.pb', 2: 'checkpoint.cgpb', 3: 'checkpoint.sb', 4: 'checkpoint.v'}


def uvarint(buf, start=0):
    """Returns (value, index just past the varint)."""
    value = shift = 0
    while start < len(buf):
        byte = buf[start]
        value |= (byte & 0x7F) << shift
        start += 1
        if not byte & 0x80:
            return value, start
        shift += 7
    raise ValueError('truncated uvarint')


def checkpoint_tags(span):
    """Every reverse-checkpoint payload on this span, either wire key."""
    return [t['value'] for t in span.get('tags', []) if t['key'] in CHECKPOINT_KEYS]


def decode_envelope(value):
    """-> list of {'kind', 'data', 'ttl'}; 'data' is spanID(8) || uvarint(depth) || truss."""
    try:
        raw = base64.b64decode(value, validate=True)
    except Exception:
        raw = b''
    if raw[:1] == b'{':
        envelope = json.loads(raw)
        return [{'kind': s['k'], 'data': base64.b64decode(s['d'], validate=True) if s.get('d') else b'',
                 'ttl': s.get('ttl')} for s in envelope.get('segs', [])]
    raw = base64.urlsafe_b64decode(value + '=' * (-len(value) % 4))
    if raw[:1] != bytes([PACKED_VERSION]):
        raise ValueError(f'not a retCtx envelope: {value[:24]!r}')
    segments, i = [], 1
    while i < len(raw):
        tag = raw[i]
        kind = PACKED_KINDS.get(tag & PACKED_KIND_MASK)
        if kind is None:
            raise ValueError(f'unknown packed segment kind {tag & PACKED_KIND_MASK}')
        size, i = uvarint(raw, i + 1)
        body, i = raw[i:i + size], i + size
        ttl = None
        if tag & PACKED_TTL_PRESENT:
            ttl, body = body[0], body[1:]
        segments.append({'kind': kind, 'data': body, 'ttl': ttl})
    return segments


def is_packed(value):
    try:
        return base64.urlsafe_b64decode(value + '=' * (-len(value) % 4))[:1] == bytes([PACKED_VERSION])
    except Exception:
        return False

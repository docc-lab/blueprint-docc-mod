"""Tomislav-RetCtx: read the reverse-path (retCtx) payload off a stored span.

Three wire formats exist and all must stay readable, because every campaign run
before 2026-09-19 used the first one and every run before 2026-10-29 the second:

  legacy  key "bridges.checkpoint", value = base64(StdEncoding) of a JSON envelope
          {"fp":..,"m":..,"k":..,"segs":[{"k":"checkpoint.pb","d":base64(bytes)}]}
  packed  key "_rc", value = base64(RawURLEncoding) of
          0x01 || (tag || uvarint(len(body)) || body)*
          tag  = kind in the low 3 bits, 0x08 when a reverse TTL leads the body
          body = [ttl] || spanID(8) || uvarint(depth) || truss
  folded  (user 2026-10-29: one attribute per span) a BRIDGE span's reverse context
          is no longer its own attribute: the checkpoint's `_br` bytes value is
          uvarint(len(rc)) || rc || payload, rc being the packed envelope RAW (no
          base64), empty (a single 0x00) when the checkpoint took none over.  Stored
          traces hold `_br` as the base64(StdEncoding) text every bytes attribute
          becomes (the Jaeger API's binary tags, ClickHouse's AsString()).  Vanilla,
          which has no `_br`, still writes "_rc".

The segment bytes are identical in all three; only the container changed. They are
told apart without guessing: the legacy value decodes under the standard base64
alphabet and starts with '{', the packed value uses the URL-safe alphabet and
starts with its version byte, and a folded reverse context is checked to be a
well-formed packed envelope before it is believed (a `_br` captured before
2026-10-29 has no length prefix, so its leading depth varint must not pass for one).
"""
import base64
import json

LEGACY_KEY = 'bridges.checkpoint'
PACKED_KEY = '_rc'
CHECKPOINT_KEYS = (PACKED_KEY, LEGACY_KEY)
BR_KEY = '_br'

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
    """Every reverse-context payload on this span: the text value of either checkpoint key, or
    (folded) the raw packed envelope found inside the span's `_br`, as bytes."""
    payloads = []
    for tag in span.get('tags', []):
        if tag['key'] in CHECKPOINT_KEYS:
            payloads.append(tag['value'])
        elif tag['key'] == BR_KEY:
            rc = folded_retctx(tag['value'])
            if rc:
                payloads.append(rc)
    return payloads


def split_br(raw):
    """A `_br` bytes value -> (rc, payload); rc is b'' when the checkpoint took no reverse context
    over. Raises ValueError when the length prefix overruns the value."""
    n, i = uvarint(raw)
    if n > len(raw) - i:
        raise ValueError('`_br` reverse-context length overruns the value')
    return raw[i:i + n], raw[i + n:]


def folded_retctx(value):
    """The reverse context folded into a stored `_br` (its base64 text or the raw bytes), or b''.
    A `_br` from before 2026-10-29 (no prefix) is recognised by the prefix not framing a packed
    envelope and gives b'' too."""
    raw = value if isinstance(value, (bytes, bytearray)) else base64.b64decode(value, validate=False)
    try:
        rc, _ = split_br(raw)
    except ValueError:
        return b''
    if not rc:
        return b''
    if rc[:1] == bytes([PACKED_VERSION]) and packed_records_valid(rc):
        return bytes(rc)
    try:  # a legacy JSON envelope's text, stored raw inside `_br`
        if base64.b64decode(rc, validate=True)[:1] == b'{':
            return bytes(rc)
    except Exception:
        pass
    return b''


def packed_records_valid(raw):
    """True when raw is 0x01 followed by at least one complete, known record (backend.validPackedRecords)."""
    i, records = 1, 0
    while i < len(raw):
        tag = raw[i]
        if (tag & PACKED_KIND_MASK) not in PACKED_KINDS or tag & ~(PACKED_KIND_MASK | PACKED_TTL_PRESENT):
            return False
        try:
            size, i = uvarint(raw, i + 1)
        except ValueError:
            return False
        if size > len(raw) - i or size < (9 if not tag & PACKED_TTL_PRESENT else 10):
            return False
        i += size
        records += 1
    return records > 0


def decode_envelope(value):
    """-> list of {'kind', 'data', 'ttl'}; 'data' is spanID(8) || uvarint(depth) || truss.
    value is the text of a checkpoint tag, or the raw bytes a folded `_br` holds."""
    if isinstance(value, (bytes, bytearray)):
        if value[:1] == bytes([PACKED_VERSION]):
            return decode_packed(bytes(value))
        value = value.decode()  # a legacy JSON envelope's base64 text
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
    return decode_packed(raw)


def decode_packed(raw):
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

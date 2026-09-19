"""Tomislav-RetCtx: reject misleading live return-payload evidence."""
import base64
import gzip
import json
from pathlib import Path
import tempfile
import unittest

from analyze_dsb_sn_e2e import inspect_traces, uvarint


class ReturnAuditTests(unittest.TestCase):
    def inspect(self, *, ttl=None, oversized=False, depth=4, packed=False):
        origin = bytes.fromhex('1234567890abcdef')
        # PB depth 4, eight-byte checkpoint ID, CPD2 descriptor, three-byte Bloom.
        truss = bytes([depth]) + origin + b'\x01' + b'\x00' * (12 if oversized else 3)
        data = origin + b'\x04' + truss
        if packed:
            # Tomislav-RetCtx: the packed `_rc` envelope must satisfy exactly the same
            # contract as the legacy JSON one, so the audit is run over both.
            body = (bytes([ttl]) if ttl is not None else b'') + data
            record = bytes([1 | (0x08 if ttl is not None else 0)]) + bytes([len(body)]) + body
            key = '_rc'
            envelope = base64.urlsafe_b64encode(b'\x01' + record).decode().rstrip('=')
        else:
            segment = {'k': 'checkpoint.pb', 'd': base64.b64encode(data).decode()}
            if ttl is not None:
                segment['ttl'] = ttl
            key = 'bridges.checkpoint'
            envelope = base64.b64encode(json.dumps({'segs': [segment]}).encode()).decode()
        sample = {'data': [{'traceID': 'one', 'spans': [
            {'spanID': origin.hex(), 'tags': []},
            {'spanID': '2222222222222222', 'tags': [{'key': key, 'value': envelope}]},
        ]}]}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'sample.json.gz'
            with gzip.open(path, 'wt') as stream:
                json.dump(sample, stream)
            return inspect_traces(path, 'pb')

    def test_live_payload_contract(self):
        for packed in (False, True):
            with self.subTest(packed=packed):
                report = self.inspect(packed=packed)
                self.assertEqual(report['counts']['reverse_segments'], 1)
                self.assertEqual(report['counts']['missing_origin_spans'], 0)
                self.assertEqual(report['returned_window_distances'], {'2': 1})
                for args in ({'ttl': 0}, {'oversized': True}, {'depth': 5}):
                    with self.assertRaises(AssertionError):
                        self.inspect(packed=packed, **args)

    def test_truncated_and_overflowing_depths(self):
        self.assertEqual(uvarint(b'\xff\x01'), (255, 2))
        for raw in (b'\x80', b'\xff' * 10, b''):
            with self.assertRaises(ValueError):
                uvarint(raw)


if __name__ == '__main__':
    unittest.main()

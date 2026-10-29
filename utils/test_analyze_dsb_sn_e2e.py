"""Tomislav-RetCtx: reject misleading live return-payload evidence."""
import base64
import gzip
import json
from pathlib import Path
import tempfile
import unittest

from analyze_dsb_sn_e2e import inspect_traces, uvarint


class ReturnAuditTests(unittest.TestCase):
    def inspect(self, *, ttl=None, oversized=False, depth=4, packed=False, folded=False):
        origin = bytes.fromhex('1234567890abcdef')
        # PB depth 4, eight-byte checkpoint ID, CPD2 descriptor, three-byte Bloom.
        truss = bytes([depth]) + origin + b'\x01' + b'\x00' * (12 if oversized else 3)
        data = origin + b'\x04' + truss
        if packed or folded:
            # Tomislav-RetCtx: the packed `_rc` envelope must satisfy exactly the same
            # contract as the legacy JSON one, so the audit is run over both.
            body = (bytes([ttl]) if ttl is not None else b'') + data
            record = bytes([1 | (0x08 if ttl is not None else 0)]) + bytes([len(body)]) + body
            key = '_rc'
            envelope = base64.urlsafe_b64encode(b'\x01' + record).decode().rstrip('=')
            if folded:
                # Tomislav-RetCtx (user 2026-10-29): the same envelope raw inside the checkpoint's
                # one `_br` attribute, uvarint(len(rc)) || rc || payload, stored as the base64
                # text every bytes attribute becomes.
                raw = b'\x01' + record
                key = '_br'
                envelope = base64.b64encode(bytes([len(raw)]) + raw + truss).decode()
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
        for form in ({}, {'packed': True}, {'folded': True}):
            with self.subTest(form=form):
                report = self.inspect(**form)
                self.assertEqual(report['counts']['reverse_segments'], 1)
                self.assertEqual(report['counts']['missing_origin_spans'], 0)
                self.assertEqual(report['returned_window_distances'], {'2': 1})
                for args in ({'ttl': 0}, {'oversized': True}, {'depth': 5}):
                    with self.assertRaises(AssertionError):
                        self.inspect(**form, **args)

    def test_folded_br_without_reverse_context_or_from_an_older_capture_yields_nothing(self):
        # Tomislav-RetCtx (2026-10-29): a checkpoint that took nothing over (0x00 prefix), and a
        # `_br` captured before the fold (no prefix: its leading depth varint must not pass for one).
        import retctx_wire
        payload = b'\x03' + bytes.fromhex('1234567890abcdef') + b'\x01\x00\x00\x00'
        for raw in (b'\x00' + payload, payload, b'\x05' + b'\x01\x01', b''):
            span = {'tags': [{'key': '_br', 'value': base64.b64encode(raw).decode()}]}
            self.assertEqual(retctx_wire.checkpoint_tags(span), [], raw)
        record = b'\x01' + bytes([10]) + bytes.fromhex('1234567890abcdef') + b'\x04' + b'\x00'
        raw = b'\x01' + record
        span = {'tags': [{'key': '_br', 'value': base64.b64encode(bytes([len(raw)]) + raw + payload).decode()}]}
        self.assertEqual(retctx_wire.checkpoint_tags(span), [raw])
        self.assertEqual(retctx_wire.split_br(bytes([len(raw)]) + raw + payload), (raw, payload))
        self.assertEqual([s['kind'] for s in retctx_wire.decode_envelope(raw)], ['checkpoint.pb'])

    def test_truncated_and_overflowing_depths(self):
        self.assertEqual(uvarint(b'\xff\x01'), (255, 2))
        for raw in (b'\x80', b'\xff' * 10, b''):
            with self.assertRaises(ValueError):
                uvarint(raw)


if __name__ == '__main__':
    unittest.main()

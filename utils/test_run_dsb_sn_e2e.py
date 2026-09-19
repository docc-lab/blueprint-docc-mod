"""Tomislav-RetCtx: protect latency parsing and counter resets in real ramps."""
import unittest

from run_dsb_sn_e2e import counter_deltas, parse_wrk, prometheus

OUTPUT = '''Sent 3005 requests
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     3.12ms    0.86ms   999.00ms   73.13%
  Latency Distribution (HdrHistogram - Recorded Latency)
 50.000%    2.41ms
 75.000%    3.97ms
 90.000%    4.53ms
 99.000%    7.47ms
100.000%  999.00ms
  Detailed Percentile spectrum:
       Value   Percentile   TotalCount 1/(1-Percentile)
       5.123     0.950000         2850        20.00
#[Mean    =        3.123, StdDeviation   =        0.863]
  3000 requests in 30.00s, 1.00MB read
  Socket errors: connect 1, read 2, write 3, timeout 4
  Non-2xx or 3xx responses: 30
Requests/sec: 100.00
'''


class WrkParsingTests(unittest.TestCase):
    def test_explicit_hdr_percentiles_do_not_depend_on_summary_header(self):
        for header in ('Max', '99%'):
            result = parse_wrk(OUTPUT.replace('Max', header))
            self.assertEqual(result['p99_ms'], 7.47)
            self.assertEqual(result['p95_ms'], 5.123)
            self.assertEqual(result['max_ms'], 999)
            self.assertEqual(result['mean_ms'], 3.123)
            self.assertEqual(result['successful_rps'], 99)
            self.assertEqual(result['sent_requests'], 3005)
            self.assertEqual(result['socket_errors']['timeout'], 4)

    def test_units_and_missing_error_lines(self):
        text = OUTPUT.replace('2.41ms', '2410us').replace('7.47ms', '0.00747s')
        text = '\n'.join(line for line in text.splitlines() if not any(
            x in line for x in ('Socket errors:', 'Non-2xx or 3xx responses:')))
        result = parse_wrk(text)
        self.assertAlmostEqual(result['p50_ms'], 2.41)
        self.assertAlmostEqual(result['p99_ms'], 7.47)
        self.assertEqual(result['successful_rps'], 100)
        self.assertFalse(any(result['socket_errors'].values()))

    def test_truncated_or_ambiguous_output_fails(self):
        for text in (OUTPUT.replace(' 99.000%    7.47ms\n', ''),
                     OUTPUT.replace('Sent 3005 requests\n', ''), OUTPUT + OUTPUT):
            with self.assertRaises(ValueError):
                parse_wrk(text)

    def test_label_sums_and_counter_resets_are_explicit(self):
        metrics = prometheus('otelcol_receiver_accepted_spans{receiver="one"} 12\n'
                             'otelcol_receiver_accepted_spans{receiver="two"} 8\n')
        self.assertEqual(metrics['otelcol_receiver_accepted_spans'], 20)
        before = {'collectors': {'pod': metrics}}
        after = {'collectors': {'pod': {'otelcol_receiver_accepted_spans': 4}}}
        deltas, resets = counter_deltas(before, after)
        self.assertEqual(deltas, {})
        self.assertEqual(resets, ['pod/otelcol_receiver_accepted_spans'])

    def test_new_error_counters_count_and_missing_counters_are_flagged(self):
        before = {'collectors': {'pod': {'otelcol_receiver_accepted_spans': 20}}}
        after = {'collectors': {'pod': {'otelcol_receiver_accepted_spans': 25,
                                       'otelcol_receiver_refused_spans': 3}}}
        deltas, resets = counter_deltas(before, after)
        self.assertEqual(deltas['otelcol_receiver_refused_spans'], 3)
        self.assertEqual(deltas['otelcol_receiver_accepted_spans'], 5)
        self.assertFalse(resets)
        deltas, resets = counter_deltas(after, before)
        self.assertIn('pod/otelcol_receiver_refused_spans/missing', resets)


if __name__ == '__main__':
    unittest.main()

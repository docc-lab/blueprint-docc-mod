import unittest

from checkpoint_distance import configure, validate


class CheckpointDistanceTest(unittest.TestCase):
    def test_switching_between_protocols_clears_previous_configuration(self):
        config = {"cpd": 3, "other": True}
        configure(config, minimum=2, maximum=6)
        self.assertEqual(config, {"cpd_min": 2, "cpd_max": 6, "other": True})
        configure(config, cpd=4)
        self.assertEqual(config, {"cpd": 4, "other": True})

    def test_custom_collector_range_survives_default_options(self):
        config = {"cpd_min": 1, "cpd_max": 256}
        configure(config, default=3)
        self.assertEqual(config, {"cpd_min": 1, "cpd_max": 256})

    def test_invalid_options_fail_before_build(self):
        for args in [(2, 2, 6), (None, 2, None), (None, None, 6),
                     (None, 0, 6), (None, 6, 2), (None, 1, 257),
                     (None, 1.5, 6), (0, None, None)]:
            with self.subTest(args=args), self.assertRaises(ValueError):
                validate(*args)
        for config in [{"cpd_min": 2}, {"cpd_max": 6}, {"cpd_min": None, "cpd_max": None}]:
            with self.subTest(config=config), self.assertRaises(ValueError):
                configure(config)


if __name__ == "__main__":
    unittest.main()

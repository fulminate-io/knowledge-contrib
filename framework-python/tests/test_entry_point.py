# SPDX-License-Identifier: Apache-2.0

"""The library's ENTRY POINT: what an operator sees when a collector cannot be
served at all.

TWO DECLARATIONS THAT WERE UNOBSERVED and are the subject here. `main` is what
this port's README and its sample tell an author to call, and its failure arm is
the path an operator reaches when a collector is misdeclared -- a params schema
that does not declare an object raises inside the speaker build, before a frame
is read. A `main` that returned 0 on that path would show a spawning daemon a
child that exited cleanly having spoken nothing, which is the least useful
diagnostic available. `new_speaker`'s no-collector refusal is the other.

THE INSTRUMENT IS A REAL CHILD PROCESS, because the observable is an EXIT CODE and
the split between the two streams. The port's conformance stub is asserted the
same way for the same reason.
"""

import unittest

from knowledge_collector import new_speaker

from . import support


class FailingMainTest(unittest.TestCase):
    def test_a_misdeclared_collector_exits_non_zero(self):
        _, raw, stderr, code = support.drive_child("tests/misdeclared_collector.py", [support.initialize_frame()], env={})
        self.assertNotEqual(code, 0, "a collector that cannot be served must not exit 0: %s" % stderr)

    def test_it_writes_the_reason_to_stderr_and_nothing_to_stdout(self):
        _, raw, stderr, _ = support.drive_child("tests/misdeclared_collector.py", [support.initialize_frame()], env={})
        self.assertEqual(raw, b"", "stdout is the protocol stream; a failed build speaks none of it")
        self.assertIn("params", stderr)
        self.assertIn("object", stderr, "the reason names what the contract requires")

    def test_the_control_a_well_declared_collector_exits_zero_and_speaks(self):
        """THE SAME-RUN CONTROL through the same instrument: without it the two
        rows above would pass for a harness that failed to run anything."""
        responses, raw, stderr, code = support.drive_child("sample_collector.py", [support.initialize_frame()], env={})
        self.assertEqual(code, 0, stderr)
        self.assertNotEqual(raw, b"")
        self.assertIsNotNone(support.result_of(responses, 1))


class NoCollectorTest(unittest.TestCase):
    def test_new_speaker_refuses_a_missing_collector_by_name(self):
        with self.assertRaises(ValueError) as caught:
            new_speaker(None)
        self.assertIn("collector", str(caught.exception))


if __name__ == "__main__":
    unittest.main()

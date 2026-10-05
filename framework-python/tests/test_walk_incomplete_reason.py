# SPDX-License-Identifier: Apache-2.0

"""The incomplete walk's reason on the wire: walk_incomplete_reason.

The encoder sends the reason Incomplete(reason) carries, beside
walk_complete=false, and sends no key at all on a complete walk. The SDK's own
strict decoder admits the key, still refuses a misspelled one, and refuses the
two shapes that contradict the key's contract: a reason beside a walk asserting
completeness, and an empty reason. An incomplete walk with no reason key is
admitted, because the key is optional.
"""

import unittest

from knowledge_collector import (
    Complete,
    ContractError,
    Incomplete,
    Result,
    decode_result,
    encode_result,
)


class EncoderTest(unittest.TestCase):
    def test_an_incomplete_walk_sends_its_reason(self):
        out = encode_result("collect", Result([], [], Incomplete("the source paged out")))
        self.assertIs(out["walk_complete"], False)
        self.assertEqual(out.get("walk_incomplete_reason"), "the source paged out")

    def test_a_complete_walk_sends_no_reason_key(self):
        out = encode_result("collect", Result([], [], Complete()))
        self.assertNotIn("walk_incomplete_reason", out)


class DecoderTest(unittest.TestCase):
    def test_the_sdks_own_incomplete_result_round_trips(self):
        encoded = encode_result("collect", Result([], [], Incomplete("the source paged out")))
        decoded = decode_result("collect", encoded)
        self.assertIs(decoded.walk_complete, False)
        self.assertEqual(decoded.walk_incomplete_reason, "the source paged out")

    def test_a_misspelled_key_is_still_refused(self):
        with self.assertRaises(ContractError) as caught:
            decode_result(
                "collect",
                {"nodes": [], "edges": [], "walk_complete": False, "walk_incomplete_reasn": "r"},
            )
        self.assertIn("walk_incomplete_reasn", str(caught.exception))

    def test_a_reason_beside_a_complete_walk_is_refused(self):
        with self.assertRaises(ContractError) as caught:
            decode_result("collect", {"nodes": [], "edges": [], "walk_complete": True, "walk_incomplete_reason": "r"})
        self.assertIn("walk_incomplete_reason", str(caught.exception))
        self.assertIn("walk_complete true", str(caught.exception))

    def test_an_empty_reason_beside_an_incomplete_walk_is_refused(self):
        with self.assertRaises(ContractError) as caught:
            decode_result("collect", {"nodes": [], "edges": [], "walk_complete": False, "walk_incomplete_reason": ""})
        self.assertIn("empty walk_incomplete_reason", str(caught.exception))
        self.assertIn("walk_complete false", str(caught.exception))

    def test_control_an_incomplete_walk_with_no_reason_key_is_admitted(self):
        decoded = decode_result("collect", {"nodes": [], "edges": [], "walk_complete": False})
        self.assertIs(decoded.walk_complete, False)
        self.assertIsNone(decoded.walk_incomplete_reason)


if __name__ == "__main__":
    unittest.main()

# SPDX-License-Identifier: Apache-2.0

"""Row 4 — the two contract files in this port are byte-identical to the
knowledge client's, and the describe schema is pinned the same way.

A COPY NOTHING COMPARES IS A COPY THAT DRIFTS. The client's package is
unimportable from a published sibling repository in every language, so a copy is
the only shape that ships; this is what keeps it honest. It reads the CLIENT's
file from disk and compares bytes, never a transcription.
"""

import os
import unittest

from knowledge_collector import describe_contract_json, input_contract_json, output_contract_json

from . import support


class ContractCopiesTest(unittest.TestCase):
    def test_the_two_contract_copies_match_the_clients_files(self):
        client = support.require_client_contract(self)
        for filename, embedded in (
            ("collector_input.schema.json", input_contract_json()),
            ("collector_output.schema.json", output_contract_json()),
        ):
            with self.subTest(filename):
                authoritative = support.read_bytes(os.path.join(client, filename))
                self.assertEqual(
                    authoritative.decode("utf-8"),
                    embedded.decode("utf-8"),
                    "this port's copy of %s has drifted from the client's file" % filename,
                )

    def test_the_copies_are_the_files_this_package_actually_reads(self):
        """THE SAME-RUN CONTROL for the comparison above: the bytes the package
        serves come off the disk under knowledge_collector/contract, so a pin that
        compared two reads of the client's own file would prove nothing."""
        for filename, embedded in (
            ("collector_input.schema.json", input_contract_json()),
            ("collector_output.schema.json", output_contract_json()),
            ("collector_describe.schema.json", describe_contract_json()),
        ):
            with self.subTest(filename):
                on_disk = support.read_bytes(os.path.join(support.CONTRACT_DIR, filename))
                self.assertEqual(on_disk, embedded)

    def test_the_describe_schema_pin_is_in_test_pending_pins(self):
        """THE PIN ITSELF LIVES BESIDE ITS SIBLINGS, in test_pending_pins.py, and
        this row exists so a reader of THIS file is not left thinking the third
        schema is unpinned.

        It moved because a pin keyed on `does the file <name I expect> exist`
        stays skipped forever when the describe work lands under a different
        spelling. The pin over there keys on the client's contract directory
        gaining a THIRD file under ANY name, and then fails naming what it found.
        """
        from .test_pending_pins import DescribeToolPinTest

        self.assertTrue(hasattr(DescribeToolPinTest, "test_the_describe_schema_this_port_ships_is_the_clients_third_contract_file"))
        self.assertEqual(len(describe_contract_json()) > 0, True, "and this port does ship a copy to pin")


if __name__ == "__main__":
    unittest.main()

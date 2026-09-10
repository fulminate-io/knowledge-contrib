# SPDX-License-Identifier: Apache-2.0

"""The EMPTY-SENSITIVE MARK, this port's half.

A collector declares, per environment name, whether it tells that name PRESENT
AND EMPTY apart from ABSENT. The mark governs what a worked entry in
documentation may show rather than what an installer writes: a `${NAME:-}`
reference resolves to the empty string in the process serving the collect, so
the child receives the name present and empty, which is inert for an unmarked
name and a broken collect for a marked one.

THIS PORT OWES THE PROPERTY BECAUSE THE CLIENT'S DECODE IS STRICT. A declaration
crossing the wire is checked twice, against the JSON Schema and by a decode that
refuses a field the client does not define; a port that cannot express the mark
ships collectors that can never be marked, and one that spells it differently
ships collectors the client refuses by name.

ABSENT MEANS FALSE, so a collector that marks nothing renders exactly the bytes
it rendered before the property existed -- which is the row that keeps every
already-published collector valid.
"""

import json
import pathlib
import unittest

from knowledge_collector.describe import EnvName


class EmptySensitiveMarkTest(unittest.TestCase):
    def test_a_marked_name_renders_the_property(self):
        rendered = EnvName("LOKI_PASSWORD", "secret", empty_sensitive=True).render()
        self.assertEqual(
            {"name": "LOKI_PASSWORD", "class": "secret", "empty_sensitive": True}, rendered
        )

    def test_the_mark_is_legal_on_the_not_carried_class(self):
        # THE CLASS THAT MAKES THE PROPERTY NECESSARY. An installed entry never
        # carries a not-carried name, so the class table drops it; a document an
        # operator copies can still show it, which is the only place the mark has
        # to live.
        rendered = EnvName("KUBERNETES_SERVICE_HOST", "not-carried", empty_sensitive=True).render()
        self.assertEqual("not-carried", rendered["class"])
        self.assertIs(True, rendered["empty_sensitive"])

    def test_an_unmarked_name_renders_no_property_at_all(self):
        # ABSENT MEANS FALSE, and this is what makes the property backward
        # compatible: the rendered document is byte-identical to the one this port
        # rendered before the property existed.
        self.assertEqual({"name": "HOME", "class": "path"}, EnvName("HOME", "path").render())
        self.assertEqual(
            {"name": "HOME", "class": "path"},
            EnvName("HOME", "path", empty_sensitive=False).render(),
        )

    def test_the_description_and_the_mark_ride_together(self):
        # The control for the two rows above: the optional properties do not
        # exclude each other, so "the mark rendered" is not an artifact of the
        # renderer having stopped rendering everything else.
        rendered = EnvName("AZURE_TOKEN_CREDENTIALS", "selector", "the credential set", True).render()
        self.assertEqual(
            {
                "name": "AZURE_TOKEN_CREDENTIALS",
                "class": "selector",
                "description": "the credential set",
                "empty_sensitive": True,
            },
            rendered,
        )

    def test_this_ports_contract_copy_declares_the_property(self):
        # A rendered mark the port's OWN schema copy does not declare would
        # validate against nothing here and be refused at the client.
        copy = (
            pathlib.Path(__file__).resolve().parents[1]
            / "knowledge_collector"
            / "contract"
            / "collector_describe.schema.json"
        )
        doc = json.loads(copy.read_text())
        items = doc["properties"]["environment"]["items"]
        self.assertIn("empty_sensitive", items["properties"])
        self.assertEqual("boolean", items["properties"]["empty_sensitive"]["type"])
        self.assertNotIn("empty_sensitive", items["required"])
        # The control: the two required properties are still required.
        self.assertEqual(["name", "class"], items["required"])


if __name__ == "__main__":
    unittest.main()

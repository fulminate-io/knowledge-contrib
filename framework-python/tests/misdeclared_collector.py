#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""misdeclared_collector.py — a collector that CANNOT be served, as a fixture.

IT EXISTS TO DRIVE ONE PATH AND NOTHING ELSE: the failure arm of
knowledge_collector.serve.main. Its params_schema declares a string where the
collector contract declares an object, so building the speaker raises
SchemaDeclarationError before a single frame is read -- which is what an operator
reaches when a collector is misdeclared, and the one entry point the port's own
suite could not otherwise drive.

WHAT THE ROW THAT DRIVES IT ASSERTS: a NON-ZERO exit, the reason on stderr, and
NOTHING on stdout. A misdeclared collector that exited 0 having spoken nothing
reads to a spawning daemon as a clean session end, which is the least useful
diagnostic available; the client's own conformance stub asserts exit codes for
exactly this reason.

It is not a collector anyone should copy. sample_collector.py is the worked
example.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from knowledge_collector import (  # noqa: E402 -- the path insert above is what makes this importable uninstalled
    BehaviorDefaults,
    Collector,
    Complete,
    Declaration,
    Result,
    ToolSpec,
    main,
)


class MisdeclaredCollector(Collector):
    def tool(self):
        return ToolSpec(name="collect")

    def params_schema(self):
        # THE DEFECT, deliberately: the contract declares params as an object.
        return {"type": "string"}

    def describe(self):
        return Declaration(behavior=BehaviorDefaults(summarizable=False, embeddable=False, syncable=True), node_types=[], edge_types=[])

    def walk(self, collect_id, params, foreign):
        return Result([], [], Complete())


if __name__ == "__main__":
    sys.exit(main(MisdeclaredCollector()))

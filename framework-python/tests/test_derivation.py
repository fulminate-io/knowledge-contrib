# SPDX-License-Identifier: Apache-2.0

"""The PARITY LIST, DERIVED rather than enumerated, run as a test.

The count this port replicates is not a target and it is not a literal in a
comment: the authority is the knowledge client's four contract test files, and the
derivation is a count of their test functions. This file runs it, so a test added
or removed on the client side turns a named row red here instead of leaving a
prose count in a docstring quietly wrong.

IT SKIPS BY NAME OUTSIDE THIS REPOSITORY, because a published copy of this port
carries none of the client's tree.
"""

import os
import re
import unittest

from . import support

_FUNC = re.compile(r"^func (Test\w+)\(", re.MULTILINE)

# The split, stated so a change to it is a change to this file rather than a
# silent drift. Every name below is a Go-only UNIT of the sixteen.
REPLICATED_UNITS = (
    "TestOutputContract_RequiresTheCompletenessAssertion",
    "TestInputContract_RequiresTheCollectID",
    "TestContractSchemaAndEnvelopeAgree",
    "TestCheckToolSchemas_MissingSchemas",
    "TestCheckToolSchemas_AcceptsTheContractVerbatimAndStricter",
    "TestCheckToolSchemas_RefusesEachLooseningIndividually",
    "TestValidateResultPayload_RefusesNonConformingResults",
    "TestDecodeResult_RefusesAnUndefinedField",
    "TestDecodeResult_RefusesNoStructuredContent",
    "TestInputContract_DeclaresContextAsAnOptionalProperty",
    "TestCheckToolSchemas_AdmitsAProviderLackingAnOptionalProperty",
    "TestCheckToolSchemas_StillRefusesAMissingRequiredProperty",
    "TestCheckToolSchemas_RefusesAnOptionalPropertyOfTheWrongType",
)

# Covered by the four env-report arms among the eleven dialing tests, not mirrored:
# it is a PARENT-SIDE spawn rule and a collector is the child.
COVERED_UNIT = "TestChildEnv_EmptyBlockIsAnEmptyEnvironmentNotInheritance"

# Excluded by name, with the reason: both are shapes of the CLIENT's own internals
# that a collector library does not hold.
EXCLUDED_UNITS = ("TestContractSummary_StatesTheContextBlock", "TestRunMCP_RecordShapeGuards")

CONTRACT_FILES = ("contract_test.go", "contract_optional_test.go")
HOST_FILES = ("mcphost_test.go", "mcphost_failures_test.go")


class ParityDerivationTest(unittest.TestCase):
    def _functions(self, filenames):
        root = support.require_repo_root(self)
        package = os.path.join(root, "cmd", "knowledge", "internal", "externalcollector")
        found = {}
        for name in filenames:
            path = os.path.join(package, name)
            if not os.path.isfile(path):
                self.fail("the client's %s is missing; the parity list has no authority to derive from" % name)
            found[name] = _FUNC.findall(support.read_text(path))
        return found

    def test_the_four_client_test_files_carry_twenty_seven_test_functions(self):
        found = self._functions(CONTRACT_FILES + HOST_FILES)
        total = sum(len(v) for v in found.values())
        self.assertEqual(
            total,
            27,
            "the derivation moved: %s. Re-run it and re-split the sixteen units before changing this port's suite."
            % {k: len(v) for k, v in found.items()},
        )

    def _units(self, found):
        """The Go-ONLY units: every function of the two contract files, plus the
        two in the host file that dial nothing. The split is by what the function
        DRIVES -- a dialing test spawns or serves a provider -- so the two host-file
        units are named rather than pattern-matched."""
        host_units = (COVERED_UNIT, "TestRunMCP_RecordShapeGuards")
        units = list(found["contract_test.go"]) + list(found["contract_optional_test.go"])
        units += [n for n in found["mcphost_test.go"] if n in host_units]
        return units

    def test_the_sixteen_units_split_thirteen_replicated_one_covered_two_excluded(self):
        found = self._functions(CONTRACT_FILES + HOST_FILES)
        units = self._units(found)
        self.assertEqual(len(units), 16, "the sixteen Go-only units: %s" % units)

        self.assertEqual(len(REPLICATED_UNITS), 13)
        for name in REPLICATED_UNITS:
            self.assertIn(name, units, "a replicated unit that is no longer on the client side")
        self.assertIn(COVERED_UNIT, units)
        for name in EXCLUDED_UNITS:
            self.assertIn(name, units)
        self.assertCountEqual(units, list(REPLICATED_UNITS) + [COVERED_UNIT] + list(EXCLUDED_UNITS))

    def test_the_eleven_dialing_tests_are_the_rest(self):
        found = self._functions(CONTRACT_FILES + HOST_FILES)
        units = set(self._units(found))
        dialing = [n for n in found["mcphost_test.go"] if n not in units] + list(found["mcphost_failures_test.go"])
        self.assertEqual(len(dialing), 11, "the eleven provider-dialing tests: %s" % dialing)
        self.assertEqual(len(dialing) + len(units), 27, "the eleven and the sixteen are the whole derivation")

    def test_this_ports_suite_carries_a_class_for_every_replicated_unit(self):
        """The replication is checked against THIS suite's own class names, so a
        property dropped here is a red rather than a quieter suite."""
        from . import test_contract_gates

        classes = [name for name in dir(test_contract_gates) if name.startswith("Property")]
        self.assertEqual(len(classes), 13, "one class per replicated property: %s" % sorted(classes))
        numbers = sorted(int(name[len("Property"):][:2]) for name in classes)
        self.assertEqual(numbers, list(range(1, 14)))

    def test_the_four_env_report_arms_that_carry_the_covered_property_are_still_there(self):
        """Property 15's coverage is four of the eleven. If they are renamed or
        removed, the coverage claim in this port's docstrings is stale and this
        row says so."""
        found = self._functions(("mcphost_test.go",))
        names = found["mcphost_test.go"]
        for arm in (
            "TestRunMCP_StdioEnvironmentIsTheEntrysBlock",
            "TestRunMCP_StdioNameAbsentFromTheBlockIsAbsentInTheChild",
            "TestRunMCP_StdioBlockValueBeatsTheDaemonsOwn",
            "TestRunMCP_StdioEmptyValueArrivesPresentAndEmpty",
        ):
            self.assertIn(arm, names)


if __name__ == "__main__":
    unittest.main()

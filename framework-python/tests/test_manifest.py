# SPDX-License-Identifier: Apache-2.0

"""Rows 33 and 35 — the non-Go manifest, and the class it puts this directory in.

WHAT THE MANIFEST IS FOR. The collectors censuses partition `cmd/collectors` by
the presence of a `go.mod`, so a port is invisible to every one of them: no
module, no lint pass, no cache family, no `dir:` leg, and a census that cannot see
it reports a clean tree. Carrying the manifest is what makes a directory a MEMBER
of a second class -- one that is built, tested and run under a declaration of its
own rather than under the Go toolchain's.

THE NAME AND THE KEY SET ARE NOT THIS PORT'S TO CHOOSE. They are declared once, in
`scripts/testdata/collector-port-contract.tsv`, and read from there by four
scripts in two languages. This suite reads that file too: the rows below take the
manifest's expected NAME and its required KEY SET out of the contract rather than
out of a literal here, so a contract that moves reds this port instead of leaving
it conforming to a spelling nobody reads any more.

THE PINS KEY ON THE SIBLING'S LANDING, NOT ON A NAME THIS PORT GUESSED. That
distinction is the whole point and it was learned the expensive way: a guard
written as `does any script mention <the name I chose>` never fires when the
sibling lands under a different name, and a skip nobody reads is indistinguishable
from a pin that will never run. Every pin below detects that the PORT
INFRASTRUCTURE EXISTS AT ALL -- the contract file, a scripts/ file naming this
port, or a census emitting a ports banner -- and then FAILS BY NAME, quoting what
it actually found, when the shape disagrees. It skips only while nothing has
landed.
"""

import os
import subprocess
import sys
import unittest

from . import support

# The contract file, which is the authority for both facts below. Its own path is
# the one literal this file cannot avoid, and a pin asserts the port arms do not
# exist without it.
CONTRACT_RELATIVE = os.path.join("scripts", "testdata", "collector-port-contract.tsv")

# What this port carries, asserted AGAINST the contract rather than instead of it.
MANIFEST_NAME = "collector-manifest.tsv"
MANIFEST = os.path.join(support.PORT_ROOT, MANIFEST_NAME)


def parse_tsv(text):
    """key<TAB>value pairs, `#` comments and blank lines ignored. Returns a list
    of pairs, because the contract declares repeated keys."""
    pairs = []
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if "\t" not in line:
            raise AssertionError("the row %r carries no tab; the format is key<TAB>value" % line)
        key, _, value = line.partition("\t")
        pairs.append((key.strip(), value.strip()))
    return pairs


def manifest_pairs():
    return parse_tsv(support.read_text(MANIFEST))


def manifest_map():
    return dict(manifest_pairs())


def contract_path():
    """The contract file's absolute path, or None when it has not landed."""
    root = support.repo_root()
    if root is None:
        return None
    path = os.path.join(root, CONTRACT_RELATIVE)
    return path if os.path.isfile(path) else None


def contract_rows():
    """The contract's declarations, grouped by key, or None when it has not
    landed."""
    path = contract_path()
    if path is None:
        return None
    grouped = {}
    for key, value in parse_tsv(support.read_text(path)):
        grouped.setdefault(key, []).append(value)
    return grouped


def port_infrastructure_evidence():
    """Every sign that the port-infrastructure work has landed, as a list of
    human-readable strings. EMPTY means nothing has landed yet.

    THE DETECTORS ARE ABOUT EXISTENCE, NEVER ABOUT A NAME THIS PORT CHOSE. Three
    independent ones, so a sibling that lands the arms under a spelling nobody
    here anticipated still activates every pin:

      1. the contract file exists at all;
      2. some script under scripts/ names this port's directory;
      3. the membership census emits a ports banner.
    """
    root = support.repo_root()
    if root is None:
        return []
    evidence = []
    if contract_path() is not None:
        evidence.append("the contract file %s exists" % CONTRACT_RELATIVE)

    scripts = os.path.join(root, "scripts")
    if os.path.isdir(scripts):
        for name in sorted(os.listdir(scripts)):
            if not name.endswith((".sh", ".py")):
                continue
            try:
                source = support.read_text(os.path.join(scripts, name))
            except (OSError, UnicodeDecodeError):
                continue
            if "framework-python" in source:
                evidence.append("scripts/%s names this port" % name)

    census = os.path.join(root, "scripts", "collectors-membership-census.sh")
    if os.path.isfile(census):
        completed = subprocess.run(["bash", census], cwd=root, capture_output=True)
        if b"ports=" in completed.stdout:
            evidence.append("the membership census emits a ports banner")
    return evidence


def require_contract(test, what):
    """Return the contract's grouped rows, or SKIP BY NAME while the file is absent.

    THE GATE IS THE CONTRACT FILE ITSELF, and only that. The contract is the one
    declaration all four readers share, so a row that asserts agreement WITH it
    has nothing to compare against until it exists. The other port-arm evidence
    is still gathered and quoted in the skip message, so a reader can see that
    arms landed without their declaration rather than reading a bare absence --
    and the row below turns that case into a failure of its own.
    """
    rows = contract_rows()
    if rows is None:
        test.skipTest(
            "PENDING PIN (the port-infrastructure ticket): %s does not exist, so there is no declaration to agree "
            "with. This port carries %r with the keys %s meanwhile; when the contract lands, this row asserts %s. "
            "Other port-arm evidence found today: %s."
            % (
                CONTRACT_RELATIVE,
                MANIFEST_NAME,
                [k for k, _ in manifest_pairs()],
                what,
                port_infrastructure_evidence() or "none",
            )
        )
    return rows


class ManifestShapeTest(unittest.TestCase):
    def test_the_manifest_exists_at_the_port_root_and_parses_as_key_tab_value(self):
        self.assertTrue(os.path.isfile(MANIFEST), "%s is what makes this directory a censused port" % MANIFEST_NAME)
        self.assertGreater(len(manifest_pairs()), 0)

    def test_every_row_is_a_tab_separated_pair_with_a_non_empty_value(self):
        for key, value in manifest_pairs():
            with self.subTest(key):
                self.assertNotEqual(key.strip(), "")
                self.assertNotEqual(value.strip(), "")

    def test_no_key_is_declared_twice(self):
        keys = [key for key, _ in manifest_pairs()]
        self.assertEqual(sorted(keys), sorted(set(keys)))

    def test_the_language_is_python(self):
        self.assertEqual(manifest_map()["language"], "python")

    def test_the_commands_need_no_shell_interpolation(self):
        """A census that EXECUTES these runs them from a cd into the staged
        directory. A command carrying a shell metacharacter would be executing
        whatever the census's own quoting made of it."""
        for key in ("build", "test"):
            with self.subTest(key):
                command = manifest_map()[key]
                for character in ("$", "`", "&&", "||", ";", "|", ">", "<", "*"):
                    self.assertNotIn(character, command)

    def test_the_ci_leg_names_a_job_that_exists_in_the_workflow(self):
        root = support.require_repo_root(self)
        workflow = support.read_text(os.path.join(root, ".github", "workflows", "ci.yml"))
        self.assertIn("\n  %s:\n" % manifest_map()["ci_leg"], workflow)

    def test_the_tsv_parser_refuses_a_row_with_no_tab(self):
        """The control for every row above: a parser that accepted a space-
        separated row would read this file's own format wrong and say nothing."""
        with self.assertRaises(AssertionError):
            parse_tsv("language python\n")
        self.assertEqual(parse_tsv("# a comment\n\nlanguage\tpython\n"), [("language", "python")])


class ManifestCommandsRunTest(unittest.TestCase):
    """The manifest's two commands are RUN, not read. A census that executes them
    is a merge gate, and a command that does not work there is an infrastructure
    failure rather than this port's own red -- so it is this port's own red here."""

    def _run(self, command):
        argv = command.split()
        self.assertEqual(argv[0], "python3")
        return subprocess.run([sys.executable] + argv[1:], cwd=support.PORT_ROOT, capture_output=True)

    def test_the_build_command_succeeds_from_a_cd_into_the_port_directory(self):
        completed = self._run(manifest_map()["build"])
        self.assertEqual(completed.returncode, 0, completed.stderr.decode("utf-8", "replace"))

    def test_the_build_command_actually_compiles_the_package(self):
        """A ZERO NEEDS A CONTROL: `compileall` over a directory that does not
        exist exits 0 having compiled nothing."""
        completed = subprocess.run(
            [sys.executable, "-m", "compileall", "-q", os.path.join(support.PORT_ROOT, "knowledge_collector")],
            capture_output=True,
        )
        self.assertEqual(completed.returncode, 0)
        self.assertTrue(
            os.path.isdir(os.path.join(support.PACKAGE_ROOT, "__pycache__")),
            "compileall wrote no bytecode; it compiled nothing",
        )


class ClassMembershipTest(unittest.TestCase):
    """Row 35 — this port is class (b) and ONLY class (b).

    A directory carrying both a manifest and a go.mod is in two classes at once:
    the Go partition would build and lint it as a module while the port partition
    ran its own commands over it.
    """

    def test_the_port_carries_a_manifest(self):
        self.assertTrue(os.path.isfile(MANIFEST))

    def test_the_port_carries_no_go_mod_anywhere_beneath_it(self):
        offenders = []
        for dirpath, dirnames, filenames in os.walk(support.PORT_ROOT):
            dirnames[:] = [d for d in dirnames if d != "__pycache__"]
            if "go.mod" in filenames:
                offenders.append(os.path.relpath(dirpath, support.PORT_ROOT))
        self.assertEqual(offenders, [], "a manifest beside a go.mod puts this directory in two classes at once")

    def test_the_port_adds_no_go_source_at_all(self):
        offenders = [p for p in _all_files() if p.endswith(".go")]
        self.assertEqual(offenders, [])

    def test_the_port_is_absent_from_every_go_toolchain_registry(self):
        """The converse half: a directory with no Go module belongs in none of the
        four Go registries, and adding it to one would make the Go partition
        report a module that is not there."""
        root = support.require_repo_root(self)
        for relative in (
            "go.work",
            os.path.join("scripts", "lint-matrix.json"),
            os.path.join("scripts", "ci-go-cache-paths.sh"),
        ):
            with self.subTest(relative):
                self.assertNotIn("framework-python", support.read_text(os.path.join(root, relative)))

    def test_the_control_those_registries_do_name_the_go_framework(self):
        """A ZERO NEEDS A CONTROL: without it the row above would pass for three
        files that named nothing."""
        root = support.require_repo_root(self)
        for relative in (
            "go.work",
            os.path.join("scripts", "lint-matrix.json"),
            os.path.join("scripts", "ci-go-cache-paths.sh"),
        ):
            with self.subTest(relative):
                self.assertIn("collectors/framework", support.read_text(os.path.join(root, relative)))


class ContractAgreementPinTest(unittest.TestCase):
    """PENDING PINS on the port-infrastructure work.

    THE CONTRACT FILE IS THE GATE for every row that asserts agreement with it,
    and only that file: a comparison has nothing to compare against until the
    declaration exists. None keys on a name this port chose, and a contract that
    lands disagreeing is a FAILURE quoting both spellings rather than a skip.

    ARMS THAT LAND WITHOUT THE CONTRACT are not swallowed by that gate: the last
    row here fails when other port infrastructure exists and the declaration the
    four readers are supposed to share does not.
    """

    def test_the_manifest_name_is_the_one_the_contract_declares(self):
        rows = require_contract(self, "the manifest's name against the contract's `manifest` row")
        declared = rows.get("manifest")
        self.assertIsNotNone(declared, "%s declares no `manifest` row; it declares %s" % (CONTRACT_RELATIVE, sorted(rows)))
        self.assertEqual(
            declared,
            [MANIFEST_NAME],
            "the contract names the manifest %s and this port carries %r" % (declared, MANIFEST_NAME),
        )
        self.assertTrue(os.path.isfile(MANIFEST))

    def test_the_key_set_is_the_one_the_contract_declares(self):
        rows = require_contract(self, "the manifest's key set against the contract's `manifest-key` rows")
        required = rows.get("manifest-key")
        self.assertIsNotNone(
            required, "%s declares no `manifest-key` rows; it declares %s" % (CONTRACT_RELATIVE, sorted(rows))
        )
        carried = [key for key, _ in manifest_pairs()]
        self.assertEqual(
            sorted(carried),
            sorted(required),
            "the contract requires the keys %s and this port declares %s" % (sorted(required), sorted(carried)),
        )

    def test_this_ports_source_extension_is_in_the_contracts_set(self):
        """THE THIRD DECLARATION THE CONTRACT CARRIES, and this port depends on it
        as directly as it depends on the other two: the isolation census's
        vendored-copy arm parses the extensions this row asserts, and the
        transitional class rule keys on the same set. A contract whose extension
        set lost `.py` would make every source file of this port invisible to the
        arm that reads them."""
        rows = require_contract(self, "that `.py` is in the contract's `source-extension` set")
        declared = rows.get("source-extension")
        self.assertIsNotNone(
            declared, "%s declares no `source-extension` rows; it declares %s" % (CONTRACT_RELATIVE, sorted(rows))
        )
        self.assertIn(
            ".py",
            declared,
            "the contract's source-extension set is %s and this port's sources are Python" % sorted(declared),
        )

    def test_the_membership_census_exits_zero_naming_this_port(self):
        """THE CONTRACT IS THE GATE HERE TOO, because the readers and their shared
        declaration land together: a contract with no reader is a shape the port
        infrastructure never ships, and gating on the census's own output instead
        would let a landed contract with a broken reader read as 'not yet'."""
        require_contract(self, "that the membership census exits 0 naming this port")
        root = support.require_repo_root(self)
        census = os.path.join(root, "scripts", "collectors-membership-census.sh")
        completed = subprocess.run(["bash", census], cwd=root, capture_output=True)
        banner = (completed.stdout + completed.stderr).decode("utf-8", "replace")
        self.assertEqual(completed.returncode, 0, banner)
        self.assertIn(
            "framework-python", banner, "the contract has landed and the census does not name this port: %s" % banner
        )

    def test_port_arms_that_land_without_the_contract_are_a_failure_not_a_skip(self):
        """The gate above skips while the contract is absent, and this row is what
        keeps that from swallowing the case it must not: infrastructure that
        censuses this port WITHOUT the declaration all four readers are supposed
        to share is the enumeration those censuses exist to forbid."""
        if contract_path() is not None:
            self.skipTest("the contract has landed; this row describes the tree before it and no longer applies")
        evidence = port_infrastructure_evidence()
        self.assertEqual(
            evidence,
            [],
            "port infrastructure has landed (%s) and %s has not, so the four readers have no single declaration to "
            "share" % ("; ".join(evidence), CONTRACT_RELATIVE),
        )

    def test_the_control_the_detectors_report_the_tree_as_it_is(self):
        """A ZERO NEEDS A CONTROL, and for a detector the control is that it can
        see something. This row states what the detectors found, and asserts the
        contract-file detector agrees with the file's own presence."""
        evidence = port_infrastructure_evidence()
        self.assertEqual(
            contract_path() is not None,
            any("contract file" in e for e in evidence),
            "the contract-file detector disagrees with the filesystem: %s" % evidence,
        )


def _all_files():
    found = []
    for dirpath, dirnames, filenames in os.walk(support.PORT_ROOT):
        dirnames[:] = [d for d in dirnames if d != "__pycache__"]
        for name in filenames:
            found.append(os.path.join(dirpath, name))
    return found


if __name__ == "__main__":
    unittest.main()

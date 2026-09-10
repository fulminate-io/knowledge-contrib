# SPDX-License-Identifier: Apache-2.0

"""THE PENDING PINS — tests that exist to turn red when sibling work lands.

Several facts this port depends on belong to tickets that had not landed when it
was written. Each is carried here as a test that OBSERVES THE LANDED ARTIFACT and
FAILS BY NAME when it disagrees, skipping only while the artifact is absent.

EVERY DETECTOR KEYS ON THE SIBLING'S LANDING, NEVER ON A NAME THIS PORT GUESSED,
and that distinction is the whole design. A guard written as "does the client's
source contain <the string I expect>" does not pass vacuously -- it never runs at
all, and a skip nobody reads is indistinguishable from a pin that will never fire.
Measured: an earlier draft of every pin below stayed SKIPPED when its awaited
artifact was planted under a plausible alternative spelling. The detectors are
therefore STRUCTURAL:

  * the harness seam        -> any os.Getenv inside the harness's stdio builder
  * the skip protocol       -> the same seam existing, then a skip naming a mode
  * the describe tool       -> the client's contract directory gaining a THIRD
                               schema file, which is the describe work's own seam
  * the contrib LICENSE     -> the contrib directory gaining a LICENSE-shaped file

A DETECTOR THAT FIRES AND A SHAPE THAT DISAGREES IS A FAILURE, not a skip, and
every message quotes what was actually found.
"""

import os
import re
import subprocess
import sys
import tempfile
import unittest

from knowledge_collector import DESCRIBE_TOOL_NAME, describe_contract_json

from . import support

_EXTERNALCOLLECTOR = os.path.join("cmd", "knowledge", "internal", "externalcollector")
_STUB_PROVIDER = os.path.join(_EXTERNALCOLLECTOR, "stubprovider_test.go")
_CLIENT_CONTRACT = os.path.join(_EXTERNALCOLLECTOR, "contract")
_CONTRIB_DIR = os.path.join("cmd", "collectors", "contrib")
_PORT_LICENSE = os.path.join(support.PORT_ROOT, "LICENSE")

# The two files the client's contract directory carried before the describe work.
# A THIRD file appearing there is that work's own seam and is what activates the
# two describe pins.
_KNOWN_CONTRACT_FILES = ("collector_input.schema.json", "collector_output.schema.json")

_SAMPLE = os.path.join(support.PORT_ROOT, "sample_collector.py")

# THE SAMPLE'S SERVED TOOL NAME, read off its own ToolSpec rather than typed
# here, because the declaration no longer carries one: the frozen contract has no
# tool key, so the listing is the only authority for the name and this reads the
# same source the listing renders from.
def _sample_tool_name():
    sys.path.insert(0, support.PORT_ROOT)
    import sample_collector

    return sample_collector.DirectoryCollector().tool().name

# A LICENSE-shaped file, in the spellings this repository could plausibly use.
_LICENSE_SHAPED = re.compile(r"^(LICEN[SC]E|COPYING|NOTICE)")


def _read(test, relative):
    root = support.require_repo_root(test)
    path = os.path.join(root, relative)
    return support.read_text(path) if os.path.isfile(path) else None


def _third_contract_files(test):
    """Every file in the client's contract directory beyond the two known ones.

    EMPTY means the describe work has not landed. This is a structural detector:
    it names no file the port hoped for, so a third schema under any spelling
    activates the pins.
    """
    root = support.repo_root()
    if root is None:
        return []
    directory = os.path.join(root, _CLIENT_CONTRACT)
    if not os.path.isdir(directory):
        return []
    return sorted(n for n in os.listdir(directory) if n not in _KNOWN_CONTRACT_FILES and not n.startswith("."))


def _harness_package_source(source):
    """Every test file of the client's harness package, the stub provider first.

    The stdio registration builder's construction chain spans several files once
    the seam exists (the builder delegates to a registration helper in one file
    which reads the seam variable through a command resolver in another), so a
    detector scoped to one file reports the pre-seam tree forever.
    """
    root = support.repo_root()
    if root is None:
        return source
    package = os.path.join(root, _EXTERNALCOLLECTOR)
    parts = [source]
    for name in sorted(os.listdir(package)):
        if name.endswith("_test.go") and name != os.path.basename(_STUB_PROVIDER):
            parts.append(support.read_text(os.path.join(package, name)))
    return "\n".join(parts)


def _go_func_bodies(source):
    """Top-level Go function bodies by name, methods keyed by their bare name."""
    bodies = {}
    for chunk in re.split(r"\n(?=func )", "\n" + source):
        match = re.match(r"func (?:\([^)]*\)\s*)?(\w+)", chunk)
        if match:
            bodies.setdefault(match.group(1), chunk)
    return bodies


def _stdio_def_env(source):
    """The harness's stdio-registration builder and every function it reaches.

    The construct under observation is the CHAIN that builds a stdio registration,
    not one function's text: the bodies are unioned by following calls from
    stdioDefEnv through the package, so a helper extracted from it stays inside
    the observed construct.
    """
    bodies = _go_func_bodies(_harness_package_source(source))
    if "stdioDefEnv" not in bodies:
        raise AssertionError("the harness package carries no stdioDefEnv; the construction point was renamed")
    seen, queue = [], ["stdioDefEnv"]
    while queue:
        name = queue.pop()
        if name in seen:
            continue
        seen.append(name)
        for callee in re.findall(r"\b(\w+)\(", bodies[name]):
            if callee in bodies and callee not in seen:
                queue.append(callee)
    return "\n".join(bodies[name] for name in seen)


def _seam_variables(source):
    """The environment variables the harness's stdio builder reads, resolved.

    EMPTY means the seam has not landed. It keys on the CONSTRUCT -- any
    os.Getenv or os.LookupEnv reachable from the builder -- rather than on a
    variable name, so a seam under any spelling activates the pins that wait on it.
    """
    package = _harness_package_source(source)
    block = _stdio_def_env(source)
    return [
        _resolve_go_const(package, token)
        for token in re.findall(r"os\.(?:Getenv|LookupEnv)\((\w+|\"[^\"]+\")\)", block)
    ]


def _resolve_go_const(source, token):
    if token.startswith('"'):
        return token.strip('"')
    match = re.search(r'^\s*%s\s*=\s*"([^"]+)"' % re.escape(token), source, re.MULTILINE)
    return match.group(1) if match else token


def _stub_modes():
    source = support.read_text(support.client_stub_source())
    start = source.index("// Stub modes.")
    end = source.index("\n)\n", start)
    return re.findall(r'^\s*stubMode\w+\s*=\s*"([^"]+)"', source[start:end], re.MULTILINE)


class HarnessSeamPinTest(unittest.TestCase):
    def test_the_seam_variable_the_ci_leg_must_set_is_the_one_the_harness_reads(self):
        source = _read(self, _STUB_PROVIDER)
        if source is None:
            self.skipTest("the client's stub-provider source is out of reach from this copy of the port")
        variables = _seam_variables(source)
        if not variables:
            self.skipTest(
                "PENDING PIN (the port-infrastructure ticket): stdioDefEnv in %s reads no environment variable, so "
                "there is no seam to point at this port's conformance stub. The detector is the CONSTRUCT -- any "
                "os.Getenv in that function -- so a seam under any variable name activates this row." % _STUB_PROVIDER
            )
        root = support.require_repo_root(self)
        # The leg sets the variables through the make target it drives, so the
        # setter is the workflow OR the Makefile; a name in neither is unset.
        setters = support.read_text(os.path.join(root, ".github", "workflows", "ci.yml")) + support.read_text(
            os.path.join(root, "Makefile")
        )
        for variable in variables:
            with self.subTest(variable):
                self.assertIn(
                    variable,
                    setters,
                    "the harness reads %s and neither a CI leg nor the make target it drives sets it; a seam "
                    "variable that is unset falls back to the Go stub, and a fallback run is identical in every "
                    "count" % variable,
                )

    def test_the_harness_still_builds_its_provider_from_the_test_binary(self):
        """THE CONTROL FOR THE SKIP ABOVE, and it is what makes the skip a report
        rather than a shrug: the reason there is no seam is that the construction
        point is still the unseamed one, observed here rather than assumed."""
        source = _read(self, _STUB_PROVIDER)
        if source is None:
            self.skipTest("the client's stub-provider source is out of reach from this copy of the port")
        if _seam_variables(source):
            self.skipTest("the seam has landed; this control describes the pre-seam tree and no longer applies")
        self.assertIn("os.Executable()", _stdio_def_env(source))

    def test_the_skip_protocol_names_the_mode_the_external_collector_does_not_emulate(self):
        """ROW 15, keyed on the SEAM rather than on one sentence of prose. This
        port's stub declares NO mode unemulated, so its own leg never takes this
        path -- and a row that is only ever green proves nothing, which is why the
        protocol is asserted against the harness."""
        source = _read(self, _STUB_PROVIDER)
        if source is None:
            self.skipTest("the client's stub-provider source is out of reach from this copy of the port")
        if not _seam_variables(source):
            self.skipTest(
                "PENDING PIN (the port-infrastructure ticket): the seam has not landed, so there is no external "
                "collector for a mode to be unemulated BY. This port's stub emulates all %d modes and declares none "
                "unemulated." % len(_stub_modes())
            )
        # The skip lives wherever the seam does, which is the harness package rather
        # than the stub provider's own file.
        package = _harness_package_source(source)
        skips = re.findall(r"t\.Skipf?\(([^\n]*)", package)
        self.assertNotEqual(
            skips, [], "the seam has landed and %s carries no t.Skip at all, so an unemulated mode is silent" % _EXTERNALCOLLECTOR
        )
        self.assertTrue(
            any("mode" in skip for skip in skips),
            "the seam has landed and no skip in %s names the mode; the skips found are %s" % (_EXTERNALCOLLECTOR, skips),
        )

    def test_this_ports_stub_declares_no_mode_unemulated(self):
        """The half of row 15 that IS decidable today."""
        import conformance_stub as stub

        self.assertEqual(len(stub.MODES), len(set(stub.MODES)), "a mode is named twice: %s" % (stub.MODES,))
        if support.client_stub_source() is None:
            self.skipTest("the client's stub source is out of reach; the mode set has no authority to match")
        self.assertEqual(sorted(stub.MODES), sorted(_stub_modes()))


class DescribeToolPinTest(unittest.TestCase):
    """Both rows key on the describe work's OWN seam: a third file joining the
    client's contract directory."""

    def test_the_describe_schema_this_port_ships_is_the_clients_third_contract_file(self):
        support.require_repo_root(self)
        third = _third_contract_files(self)
        if not third:
            self.skipTest(
                "PENDING PIN (the describe-tool ticket): the client's contract directory carries only %s, so the "
                "describe schema has not landed. This port ships its own copy and validates its declaration against "
                "it meanwhile; the detector is a THIRD file appearing there under any name."
                % (list(_KNOWN_CONTRACT_FILES),)
            )
        self.assertEqual(
            third,
            ["collector_describe.schema.json"],
            "the client's contract directory gained %s and this port ships collector_describe.schema.json" % third,
        )
        root = support.require_repo_root(self)
        authoritative = support.read_bytes(os.path.join(root, _CLIENT_CONTRACT, third[0]))
        self.assertEqual(
            authoritative.decode("utf-8"),
            describe_contract_json().decode("utf-8"),
            "this port's copy of the describe schema has drifted from the client's file",
        )

    def test_the_fixed_describe_tool_name_agrees_with_the_clients_constant(self):
        root = support.require_repo_root(self)
        if not _third_contract_files(self):
            self.skipTest(
                "PENDING PIN (the describe-tool ticket): the describe schema has not joined the client's contract "
                "directory, so the client declares no describe tool yet. This port serves it under %r; the client "
                "must know a tool's name before it can call any tool, so the two constants have to agree."
                % DESCRIBE_TOOL_NAME
            )
        found = {}
        for dirpath, dirnames, filenames in os.walk(os.path.join(root, "cmd", "knowledge")):
            dirnames[:] = [d for d in dirnames if d != "testdata"]
            for name in filenames:
                if not name.endswith(".go") or name.endswith("_test.go"):
                    continue
                source = support.read_text(os.path.join(dirpath, name))
                for match in re.finditer(r'(\w*[Dd]escribe\w*)\s*=\s*"([^"]+)"', source):
                    found[match.group(1)] = match.group(2)
        self.assertNotEqual(
            found,
            {},
            "the describe schema has landed and no Go constant under cmd/knowledge names a describe tool, so nothing "
            "on the client side declares the name this port serves (%r)" % DESCRIBE_TOOL_NAME,
        )
        self.assertIn(
            DESCRIBE_TOOL_NAME,
            found.values(),
            "this port serves the describe tool as %r and the client's describe constants are %s"
            % (DESCRIBE_TOOL_NAME, found),
        )


class CollectorAddFillPinTest(unittest.TestCase):
    """Row 11c — `knowledge collector add` fills the entry from the describe
    result, which is the ONLY exercise of the client's fill path in this ticket.

    ITS ASSERTION IS NOT ITS GUARD. An earlier draft skipped on "the word describe
    is absent from the client's collector-add source" and then asserted the same
    predicate, so planting a comment that merely mentioned the word turned it
    green while nothing filled anything. The guard is now the describe work's own
    seam; the assertion is the SHAPE OF THE WRITTEN ENTRY against the declaration
    the sample serves.
    """

    def _declared(self):
        sys.path.insert(0, support.PORT_ROOT)
        import sample_collector

        return sample_collector.DirectoryCollector().describe().render()

    def test_the_written_entry_carries_the_fields_the_declaration_declares(self):
        support.require_repo_root(self)
        if not _third_contract_files(self):
            self.skipTest(
                "PENDING PIN (the describe-tool ticket): the describe schema has not joined the client's contract "
                "directory, so `collector add` has no describe result to fill an entry from. When it lands, this row "
                "registers this port's sample collector and reads the declared fields back out of the written entry."
            )
        binary = os.environ.get("KNOWLEDGE_BIN", "")
        if not binary or not os.path.isfile(binary):
            self.skipTest(
                "the describe fill has landed but KNOWLEDGE_BIN names no client binary (%r); this row registers "
                "through the real client, so it needs one" % binary
            )

        declared = self._declared()
        with tempfile.TemporaryDirectory() as scratch:
            completed = subprocess.run(
                [binary, "collector", "add", "--tool", _sample_tool_name(), "sample-py", "--", sys.executable, _SAMPLE],
                cwd=scratch,
                capture_output=True,
                timeout=120,
                env={"HOME": scratch, "PATH": os.environ.get("PATH", "/usr/bin:/bin"), "TMPDIR": tempfile.gettempdir()},
            )
            self.assertEqual(
                completed.returncode,
                0,
                (completed.stdout + completed.stderr).decode("utf-8", "replace"),
            )
            entry = _entry(scratch, "sample-py")

        rendered = json_dumps(entry)
        # THE TOOL NAME COMES FROM THE LISTING, not from the declaration: the
        # frozen contract carries no tool key, so the name asserted here is the
        # one the sample's own ToolSpec serves.
        self.assertEqual(entry.get("tool"), _sample_tool_name(), "the collect tool name did not reach the entry")
        for node_type in declared["node_types"]:
            self.assertIn(node_type, rendered, "the node vocabulary did not reach the entry: %s" % rendered)
        for edge_type in declared["edge_types"]:
            self.assertIn(edge_type, rendered, "the edge vocabulary did not reach the entry: %s" % rendered)
        for env_name in declared.get("environment", []):
            self.assertIn(env_name["name"], rendered, "the declared environment name did not reach the entry")
            self.assertIn(env_name["class"], rendered, "the declared environment class did not reach the entry")
        for field in declared.get("behavior", {}).get("bm25_fields", []):
            self.assertIn(field, rendered, "the declared bm25 field list did not reach the entry")

    def test_the_declaration_this_row_compares_against_is_populated(self):
        """A ZERO NEEDS A CONTROL: the row above asserts every declared part
        reached the entry, and it would pass over a declaration that declared
        nothing."""
        declared = self._declared()
        self.assertNotIn("tool", declared, "the frozen contract carries no tool key")
        self.assertGreater(len(declared["node_types"]), 1)
        self.assertGreater(len(declared["edge_types"]), 0)
        self.assertGreater(len(declared["environment"]), 0)
        self.assertIn("behavior", declared)


class LicensePinTest(unittest.TestCase):
    def test_the_port_carries_an_apache_2_license(self):
        """The half that is decidable today, so the pin below is about DRIFT
        rather than about presence."""
        text = support.read_text(_PORT_LICENSE)
        self.assertIn("Apache License", text)
        self.assertIn("Version 2.0, January 2004", text)
        self.assertIn("http://www.apache.org/licenses/LICENSE-2.0", text)

    def test_it_is_a_byte_copy_of_the_contrib_root_license(self):
        """THE DETECTOR IS A LICENSE-SHAPED FILE APPEARING IN THE CONTRIB
        DIRECTORY, under any name, rather than one path this port guessed."""
        root = support.require_repo_root(self)
        directory = os.path.join(root, _CONTRIB_DIR)
        if not os.path.isdir(directory):
            self.skipTest("PENDING PIN (the framework-README ticket): %s does not exist" % _CONTRIB_DIR)
        # BOTH SPELLINGS AND THE TWO NEIGHBOURING CONVENTIONS. Measured: a
        # detector keyed on LICENSE alone stayed SKIPPED when a planted
        # LICENCE.txt landed, which is the exact failure mode this rewrite
        # exists to remove -- one letter is enough to silence a pin forever.
        candidates = sorted(n for n in os.listdir(directory) if _LICENSE_SHAPED.match(n.upper()))
        if not candidates:
            self.skipTest(
                "PENDING PIN (the framework-README ticket): %s carries no LICENSE-shaped file, so this port's LICENSE "
                "has nothing authoritative to be a byte copy OF. It ships the unmodified Apache-2.0 text meanwhile; "
                "the files there today are %s." % (_CONTRIB_DIR, sorted(os.listdir(directory)))
            )
        self.assertEqual(
            candidates, ["LICENSE"], "the contrib directory carries %s and this port pins against LICENSE" % candidates
        )
        self.assertEqual(
            support.read_bytes(os.path.join(directory, candidates[0])),
            support.read_bytes(_PORT_LICENSE),
            "this port's LICENSE has drifted from the contrib root LICENSE",
        )

    def test_every_shipped_python_file_carries_the_spdx_marker(self):
        missing = []
        for dirpath, dirnames, filenames in os.walk(support.PORT_ROOT):
            dirnames[:] = [d for d in dirnames if d != "__pycache__"]
            for name in sorted(filenames):
                if not name.endswith(".py"):
                    continue
                path = os.path.join(dirpath, name)
                if "SPDX-License-Identifier: Apache-2.0" not in support.read_text(path).split("\n\n")[0]:
                    missing.append(os.path.relpath(path, support.PORT_ROOT))
        self.assertEqual(missing, [])

    def test_the_control_the_spdx_walk_found_files(self):
        count = 0
        for dirpath, dirnames, filenames in os.walk(support.PORT_ROOT):
            dirnames[:] = [d for d in dirnames if d != "__pycache__"]
            count += sum(1 for name in filenames if name.endswith(".py"))
        self.assertGreater(count, 15, "the walk found almost no Python; the row above would pass over nothing")


def json_dumps(value):
    import json

    return json.dumps(value, sort_keys=True)


def _entry(scratch, name):
    """The written entry, read back out of the scratch HOME's collector config."""
    import json

    for relative in (
        os.path.join(".knowledge", "collectors.json"),
        os.path.join(".config", "knowledge", "collectors.json"),
    ):
        path = os.path.join(scratch, relative)
        if os.path.isfile(path):
            document = json.loads(support.read_text(path))
            entries = document.get("collectors", document)
            if name in entries:
                return entries[name]
    raise AssertionError("no collector entry named %r was written under %s" % (name, scratch))


if __name__ == "__main__":
    unittest.main()

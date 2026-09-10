# SPDX-License-Identifier: Apache-2.0

"""Rows 26 to 29 — the README, pinned by a test the way the module READMEs are.

Row 26: one failure per required claim, over a length floor as the known positive.
Row 27: THE DAEMON-PATH SENTENCE IS A RECOMMENDATION AND THIS PINS THAT IT IS. The
        client requires no absolute path: it stores the command verbatim and
        resolves it with one lookup in the process serving the collect, so a bare
        name resolves against the DAEMON's PATH. A README asserting that the client
        REQUIRES an absolute path is a false claim about our own product, and this
        test refuses it.
Row 28: no pricing, and every worked command absolute.
Row 29: THE PUBLISH TRAP. No shipped non-Go file of this port may carry the
        in-repo module prefix: the publish rewrites module paths in *.go, go.mod
        and go.sum only, and then greps EVERY file in the staged tree for a
        survivor and aborts. This port is the first directory whose README,
        packaging file and sources are all non-Go.
"""

import os
import re
import unittest

from . import support

_README = os.path.join(support.PORT_ROOT, "README.md")

# THE NEEDLE, BUILT AT RUN TIME. Written whole it would be a survivor in this very
# file and would abort the publish this row exists to protect.
#
# IT IS A join() RATHER THAN A CONCATENATION, and the reason is measured rather
# than stylistic: CPython CONSTANT-FOLDS adjacent string literals, so
# `"a" + "b" + "c"` becomes ONE literal in the compiled bytecode. That bytecode
# lands in __pycache__, the publish copies the directory whole, and its survivor
# grep reads every staged file -- so the folded form aborted the publish from a
# .pyc while every source-level check stayed green. A method call is not folded,
# so neither the source nor the bytecode carries the needle.
_IN_REPO_MODULE_PREFIX = "/".join(("github.com", "fulminate-io", "knowledge", "cmd", "collectors"))
_PUBLISHED_MODULE_PREFIX = "/".join(("github.com", "fulminate-io", "knowledge-contrib"))


class ReadmeClaimTest(unittest.TestCase):
    def setUp(self):
        self.readme = support.read_text(_README)

    def test_the_known_positive_the_readme_is_a_document_rather_than_a_stub(self):
        self.assertGreater(len(self.readme), 2000, "a length floor, so the claim rows below are not passing over a stub")

    def test_row26a_it_states_how_to_install_from_source(self):
        self.assertIn("Install from source", self.readme)
        self.assertIn("no package to install", self.readme)

    def test_row26b_it_carries_the_worked_sample(self):
        self.assertIn("sample_collector.py", self.readme)
        self.assertIn("class DirectoryCollector(Collector):", self.readme)

    def test_row26c_it_shows_registration_with_the_interpreter_and_the_script(self):
        self.assertIn("knowledge collector add", self.readme)
        self.assertRegex(self.readme, r"--\s*\\?\s*\n?\s*/usr/bin/python3 /abs/path/to/sample_collector\.py")

    def test_row26d_it_states_that_the_env_block_is_the_childs_whole_environment(self):
        self.assertIn("the child's whole environment", self.readme)
        self.assertIn("present and empty", self.readme)

    def test_row27_the_daemon_path_sentence_is_a_recommendation_with_its_mechanism(self):
        self.assertIn("unless you know the name is on the daemon's own", self.readme)
        self.assertIn("recommendation with a mechanism behind it, not a requirement", self.readme)
        self.assertIn("stores whatever you write after `--` verbatim", self.readme)
        self.assertIn("is not executable", self.readme)

    def test_row27b_it_never_claims_the_client_requires_an_absolute_path(self):
        """A false claim about our own product. The forbidden shapes are spelled
        out rather than searched for by keyword, so a rewording that reintroduces
        the claim reds here."""
        for claim in (
            "must be an absolute path",
            "requires an absolute path",
            "the client requires an absolute",
            "absolute path is required",
        ):
            self.assertNotIn(claim, self.readme.lower())

    def test_row27c_it_carries_the_trap_that_PATH_in_the_env_block_does_not_help(self):
        self.assertIn("Putting `PATH` in the entry's environment block does not help", self.readme)

    def test_row28a_it_carries_no_pricing(self):
        for word in ("pricing", "per seat", "per-seat", "subscription", "$", "free tier", "paid plan"):
            self.assertNotIn(word, self.readme.lower())

    def test_row28b_every_worked_registration_command_uses_an_absolute_script_path(self):
        commands = re.findall(r"^\s*(/usr/bin/python3 \S+)", self.readme, re.MULTILINE)
        self.assertGreater(len(commands), 0, "the control: there are worked commands to check")
        for command in commands:
            for token in command.split():
                if token.endswith(".py") or token.startswith("/usr/bin"):
                    self.assertTrue(token.startswith("/"), "%r is not absolute" % token)


class PublishNeedleTest(unittest.TestCase):
    """Row 29. The publish rewrites *.go, go.mod and go.sum only, and then greps
    EVERY staged file for a survivor. Every non-Go file this port ships therefore
    names the PUBLISHED path, never the in-repo one."""

    def test_no_shipped_file_of_this_port_carries_the_in_repo_module_prefix(self):
        """IT READS BYTES AND IT WALKS EVERYTHING, INCLUDING __pycache__.

        Both halves were measured rather than chosen. The publish copies this
        directory with `cp -R` and greps EVERY file of the staged tree, so a walk
        that skipped compiled bytecode could not see the class that actually
        aborts it -- and it did not, until this row was written this way. Reading
        BYTES rather than text is the other half: a text read of a .pyc raises
        before it can compare anything.
        """
        needle = _IN_REPO_MODULE_PREFIX.encode("utf-8")
        offenders = []
        for path in _shipped_files():
            if needle in support.read_bytes(path):
                offenders.append(os.path.relpath(path, support.PORT_ROOT))
        self.assertEqual(
            offenders,
            [],
            "the publish's survivor grep would abort on these; the published modules would declare a path that "
            "resolves to nothing",
        )

    def test_the_control_the_byte_walk_reaches_compiled_bytecode_when_there_is_any(self):
        """A ZERO NEEDS A CONTROL, and the control for THIS row is the walk's
        reach: if a __pycache__ exists in this tree, the walk must be opening it.
        A run on a tree with none says so rather than claiming coverage it did not
        have."""
        compiled = [p for p in _shipped_files() if p.endswith(".pyc")]
        if not compiled:
            self.skipTest("this tree carries no compiled bytecode, so the row above had none to reach")
        for path in compiled:
            support.read_bytes(path)

    def test_the_needle_is_absent_from_this_files_own_compiled_bytecode(self):
        """THE DIRECT ROW FOR THE FOLDING TRAP. It compiles this file the way the
        interpreter does and looks for the needle in the result, so a future edit
        that reintroduces a concatenated literal reds here rather than in the
        publish."""
        source = support.read_text(os.path.join(support.PORT_ROOT, "tests", "test_readme.py"))
        code = compile(source, "test_readme.py", "exec")
        self.assertNotIn(_IN_REPO_MODULE_PREFIX, _all_consts(code))

    def test_the_control_a_concatenated_literal_would_be_found_by_that_row(self):
        folded = compile('X = "github.com/fulminate-io/" + "knowledge" + "/cmd/collectors"', "<control>", "exec")
        self.assertIn(_IN_REPO_MODULE_PREFIX, _all_consts(folded))

    def test_the_control_the_needle_matcher_does_fire(self):
        """A ZERO NEEDS A CONTROL, and this one is a planted positive through the
        same comparison the row above uses."""
        planted = "module " + _IN_REPO_MODULE_PREFIX + "/framework-python"
        self.assertIn(_IN_REPO_MODULE_PREFIX, planted)
        self.assertNotIn(_IN_REPO_MODULE_PREFIX, "module " + _PUBLISHED_MODULE_PREFIX + "/framework-python")

    def test_the_control_the_walk_opened_the_files_it_claims_to_have_scanned(self):
        scanned = [os.path.relpath(p, support.PORT_ROOT) for p in _shipped_files()]
        self.assertIn("README.md", scanned)
        self.assertIn("pyproject.toml", scanned)
        self.assertIn("collector-manifest.tsv", scanned)
        self.assertIn("sample_collector.py", scanned)
        self.assertGreater(len(scanned), 15, "the walk found almost nothing: %s" % scanned)

    def test_the_readme_names_the_published_repository_rather_than_this_one(self):
        readme = support.read_text(_README)
        self.assertIn(_PUBLISHED_MODULE_PREFIX.rsplit("/", 1)[-1], readme)


def _all_consts(code):
    """Every string constant in a compiled code object, recursively."""
    out = []
    for const in code.co_consts:
        if isinstance(const, str):
            out.append(const)
        elif hasattr(const, "co_consts"):
            out.extend(_all_consts(const))
    return out


def _shipped_files():
    """EVERY file under this port's root, with NOTHING pruned.

    The publish copies the directory with `cp -R` and greps all of it, so the
    subject of the survivor row is the directory as it stands on disk --
    __pycache__ included. Pruning anything here would make the row a weaker
    instrument than the gate it stands in for.
    """
    found = []
    for dirpath, _dirnames, filenames in os.walk(support.PORT_ROOT):
        for name in sorted(filenames):
            found.append(os.path.join(dirpath, name))
    return sorted(found)


if __name__ == "__main__":
    unittest.main()

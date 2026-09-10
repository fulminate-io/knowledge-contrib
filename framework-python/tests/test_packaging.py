# SPDX-License-Identifier: Apache-2.0

"""Rows 12a, 22 and 24 — the packaging file, the version floor, and the CI leg.

Row 12a: the dependency list is EMPTY, and the version floor and the CI pin agree,
         EACH READ INDEPENDENTLY. Row 22 is not a proxy for the first half: a suite
         runs green with a dependency list full of entries as long as nothing
         imports them at test time. And an agreement asserted by reading one value
         and echoing it is always true, which is why both halves are read from
         their own file here.
Row 22:  the suite runs from a `cd` into this directory with ONE command and no
         install step.
Row 24:  the CI leg exists, is hosted, is gated on the collectors filter, pins the
         interpreter, and is named by no required-check list.

THE WORKFLOW ROWS SKIP BY NAME outside this repository, where there is no
workflow to read.
"""

import ast as pyast
import os
import re
import unittest

from . import support

_PYPROJECT = os.path.join(support.PORT_ROOT, "pyproject.toml")
_JOB_NAME = "framework-python-tests"


def _declarations():
    """The pyproject's DECLARATIONS: every non-comment, non-blank line, stripped.

    A hand-rolled reader rather than tomllib, because tomllib is 3.11+ and this
    suite runs on the floor the file itself declares.
    """
    out = []
    for line in support.read_text(_PYPROJECT).splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        out.append(stripped)
    return out


def _requires_python():
    """The floor, read out of pyproject.toml. Parsed rather than imported: this
    port takes no dependency and tomllib is 3.11+, below the floor it would be
    reading."""
    for line in support.read_text(_PYPROJECT).splitlines():
        match = re.match(r'^\s*requires-python\s*=\s*"([^"]+)"\s*$', line)
        if match:
            return match.group(1)
    raise AssertionError("pyproject.toml declares no requires-python")


def _floor_tuple():
    spec = _requires_python()
    match = re.match(r"^>=\s*(\d+)\.(\d+)$", spec)
    assert match, "the floor %r is not a simple >=MAJOR.MINOR bound" % spec
    return (int(match.group(1)), int(match.group(2)))


def _workflow_text(test):
    root = support.require_repo_root(test)
    return support.read_text(os.path.join(root, ".github", "workflows", "ci.yml"))


def _job_block(workflow, job):
    """The YAML block of one top-level job, delimited by the next two-space key."""
    start = workflow.index("\n  %s:\n" % job) + 1
    rest = workflow[start + 1:]
    match = re.search(r"\n  [A-Za-z0-9_-]+:\n", rest)
    end = start + 1 + (match.start() if match else len(rest))
    return workflow[start:end]


class PackagingFileTest(unittest.TestCase):
    def test_row12a1_the_dependency_list_is_empty(self):
        """A dependency that creeps in reds HERE rather than surfacing in a
        merge-gating census that executes this port's test command, where it would
        read as an infrastructure failure rather than as this port's own red.

        THE SUBJECT IS THE DECLARATIONS, NOT THE FILE'S TEXT. This file's own
        header explains at length why there are no dependencies, so a substring
        test over the whole file reports its own documentation.
        """
        declarations = _declarations()
        offenders = [line for line in declarations if re.match(r"^(optional-)?dependencies\s*=", line)]
        offenders += [line for line in declarations if line.startswith("[build-system]")]
        offenders += [line for line in declarations if "dependencies" in line and line.startswith("[")]
        self.assertEqual(offenders, [], "this port declares no runtime dependency of any kind")

    def test_the_control_the_declaration_reader_sees_the_keys_that_are_there(self):
        """A ZERO NEEDS A CONTROL: without this the row above would pass for a
        reader that stripped the whole file."""
        declarations = _declarations()
        self.assertIn('requires-python = ">=3.9"', declarations)
        self.assertIn("[project]", declarations)
        self.assertTrue(all(not line.startswith("#") for line in declarations))

    def test_row12a1b_and_no_lockfile_ships_beside_it(self):
        for name in ("poetry.lock", "requirements.txt", "Pipfile", "Pipfile.lock", "uv.lock", "pdm.lock"):
            self.assertFalse(
                os.path.exists(os.path.join(support.PORT_ROOT, name)),
                "%s is a dependency manifest; this port has no dependencies to lock" % name,
            )

    def test_row12a2_the_version_floor_and_the_ci_pin_agree_each_read_independently(self):
        floor = _floor_tuple()
        workflow = _workflow_text(self)
        block = _job_block(workflow, _JOB_NAME)
        match = re.search(r"python-version:\s*'([0-9]+)\.([0-9]+)'", block)
        self.assertIsNotNone(match, "the CI leg names no explicit python-version; it would take the image default")
        pin = (int(match.group(1)), int(match.group(2)))
        self.assertGreaterEqual(
            pin,
            floor,
            "the CI leg pins Python %s while the packaging file declares the floor %s" % (pin, floor),
        )
        self.assertEqual(pin, floor, "the leg runs the floor itself, so the floor is exercised rather than asserted")

    def test_the_interpreter_running_this_suite_satisfies_the_declared_floor(self):
        import sys

        self.assertGreaterEqual(sys.version_info[:2], _floor_tuple())

    def test_every_shipped_file_parses_under_the_declared_floors_grammar(self):
        """THE FLOOR IS CHECKED, NOT ASSERTED. A syntax feature newer than the
        floor would build and run here and fail only on the pinned CI
        interpreter."""
        floor = _floor_tuple()
        for path in sorted(_python_files()):
            with self.subTest(os.path.relpath(path, support.PORT_ROOT)):
                pyast.parse(support.read_text(path), filename=path, feature_version=floor)

    def test_the_control_the_feature_version_gate_does_reject_newer_syntax(self):
        """A ZERO NEEDS A CONTROL: without this the row above would pass for a
        parser that ignored feature_version."""
        with self.assertRaises(SyntaxError):
            pyast.parse("match x:\n    case 1:\n        pass\n", feature_version=(3, 9))


class SuiteInvocationTest(unittest.TestCase):
    def test_row22_the_manifest_test_command_is_the_bare_interpreter_invocation(self):
        """The manifest's own reader is test_manifest.py's, so this row reads the
        file through the parser the format's rows are asserted with rather than
        through a second one that could disagree about it."""
        from .test_manifest import manifest_map

        command = manifest_map()["test"]
        self.assertEqual(command, "python3 -m unittest discover")
        self.assertNotIn("install", command)
        self.assertNotIn("pip", command)

    def test_row22b_the_suite_is_discovered_from_the_port_root_with_no_arguments(self):
        """`python3 -m unittest discover` finds this package because tests/ carries
        an __init__.py; without it the discovery is a namespace package and finds
        nothing, which reads exactly like a clean run."""
        self.assertTrue(os.path.isfile(os.path.join(support.PORT_ROOT, "tests", "__init__.py")))

    def test_the_readme_documents_the_same_invocation(self):
        readme = support.read_text(os.path.join(support.PORT_ROOT, "README.md"))
        self.assertIn("python3 -m unittest discover", readme)


class CILegTest(unittest.TestCase):
    def test_row24a_the_leg_exists_and_runs_on_a_hosted_runner(self):
        block = _job_block(_workflow_text(self), _JOB_NAME)
        self.assertIn("runs-on: ubuntu-latest", block)

    def test_row24b_the_leg_is_gated_on_the_collectors_filter_which_already_covers_this_directory(self):
        workflow = _workflow_text(self)
        block = _job_block(workflow, _JOB_NAME)
        self.assertIn("needs: changes", block)
        self.assertIn("needs.changes.outputs.collectors == 'true'", block)
        self.assertIn("- 'cmd/collectors/**'", workflow, "the filter that already fires for this port's directory")

    def test_row24c_the_leg_is_not_a_dir_shaped_collector_leg(self):
        """A port carries no go.mod, and the membership census partitions modules
        by go.mod presence while reading CI legs out of the `dir:` key. A port leg
        in that shape is the shape the census refuses."""
        block = _job_block(_workflow_text(self), _JOB_NAME)
        self.assertNotIn("dir: ./cmd/collectors", block)

    def test_row24d_no_required_check_list_in_the_repository_names_this_job(self):
        root = support.require_repo_root(self)
        offenders = []
        for dirpath, dirnames, filenames in os.walk(os.path.join(root, ".github")):
            dirnames[:] = [d for d in dirnames if d != "workflows"]
            for name in filenames:
                path = os.path.join(dirpath, name)
                if _JOB_NAME in support.read_text(path) or "Python collector framework" in support.read_text(path):
                    offenders.append(os.path.relpath(path, root))
        self.assertEqual(offenders, [], "adding a branch-ruleset name would make this leg a required check")

    def test_row25_the_leg_builds_the_client_and_hands_it_to_the_suite(self):
        """R4's second half: the sample collector is driven by the client binary in
        this leg. `collector add` DIALS the provider it registers, so the row is a
        real handshake against the Python speaker rather than a transcription."""
        block = _job_block(_workflow_text(self), _JOB_NAME)
        self.assertIn("go build -o", block)
        self.assertIn("KNOWLEDGE_BIN", block)

    def test_the_leg_restores_the_go_cache_and_does_not_save_it(self):
        """One writer per cache family: the writer for `workspace` on this venue is
        the server test leg, and a second saver on one key makes the entry's
        contents a race."""
        block = _job_block(_workflow_text(self), _JOB_NAME)
        self.assertIn("actions/cache/restore@v6", block)
        self.assertNotIn("actions/cache@v6", block)
        self.assertNotIn("actions/cache/save", block)


def _python_files():
    for dirpath, dirnames, filenames in os.walk(support.PORT_ROOT):
        dirnames[:] = [d for d in dirnames if d != "__pycache__"]
        for name in filenames:
            if name.endswith(".py"):
                yield os.path.join(dirpath, name)


if __name__ == "__main__":
    unittest.main()

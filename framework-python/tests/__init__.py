# SPDX-License-Identifier: Apache-2.0

"""The port's own test suite.

It runs from a `cd` into the port directory with ONE command and no install step:

    python3 -m unittest discover -v

That is the whole invocation the packaging file, the manifest and the CI leg all
name, and it is install-free because the port takes no runtime dependency. `python
-m` puts the current directory on the import path, which is what makes
`knowledge_collector` importable uninstalled.

SOME TESTS REACH OUTSIDE THE PORT DIRECTORY, and every one of them skips BY NAME
when what it reaches for is absent. Two classes: the pins on sibling work that has
not landed yet (each names the ticket that owns it), and the byte-parity reads of
the knowledge client's own files, which a published copy of this port does not
carry. A skip that names what it looked for and where is a report; a test that
quietly passes over a missing file is the vacuous pass this note exists to
forbid.
"""

#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""sample_collector.py — the worked example this port's README registers.

It walks ONE LOCAL DIRECTORY into a small graph: one `directory` node for the
root, one `file` node per entry, and a `contains` edge from the root to each. It
reads NO credential, opens no network connection and takes no dependency, which
is what lets the README's registration and the CI leg drive it with nothing
installed.

    knowledge collector add --tool collect sample-py -- \
        /usr/bin/python3 /abs/path/to/sample_collector.py

RUN IT BY HAND to see the frames: this file speaks MCP on stdin/stdout, so
`echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | python3
sample_collector.py` prints one handshake answer and waits for the next frame.

THE THREE COMPLETENESS ARMS ARE ALL REACHABLE FROM ITS PARAMS, deliberately, so
the port's own suite and a live confirmation can drive each without a fixture
that only a test can build:

  * an ordinary walk asserts COMPLETE;
  * a walk that hits `limit` asserts INCOMPLETE with the reason;
  * a walk whose `path` does not exist RAISES, which becomes an isError result
    carrying the text -- never an empty successful envelope, which would assert a
    walk that found nothing.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from knowledge_collector import (  # noqa: E402 -- the path insert above is what makes this importable uninstalled
    BehaviorDefaults,
    Collector,
    Complete,
    Declaration,
    Edge,
    EnvName,
    Incomplete,
    Node,
    NodeTypeBehavior,
    Result,
    ToolSpec,
    main,
)

# The node and edge types this walk emits. The declaration reports THIS TUPLE and
# the walk emits from it, so the vocabulary a `collector add` writes into the
# entry cannot drift from what a collect then produces.
NODE_TYPE_DIRECTORY = "directory"
NODE_TYPE_FILE = "file"
EDGE_TYPE_CONTAINS = "contains"

NODE_VOCABULARY = (NODE_TYPE_DIRECTORY, NODE_TYPE_FILE)
EDGE_VOCABULARY = (EDGE_TYPE_CONTAINS,)


class DirectoryCollector(Collector):
    """Walks one directory, one level deep."""

    def tool(self):
        return ToolSpec(name="collect", description="Walk one local directory into directory and file nodes.")

    def params_schema(self):
        return {
            "type": "object",
            "required": ["path"],
            "properties": {
                "path": {
                    "type": "string",
                    "minLength": 1,
                    "description": "The absolute path of the directory to walk.",
                },
                "limit": {
                    "type": "integer",
                    "minimum": 0,
                    "description": "Stop after this many entries and assert an INCOMPLETE walk. Zero means no limit.",
                },
            },
        }

    def describe(self):
        return Declaration(
            behavior=BehaviorDefaults(summarizable=False, embeddable=False, syncable=True, bm25_fields=["symbol_name"]),
            node_type_overrides={
                NODE_TYPE_FILE: NodeTypeBehavior(embeddable=False, bm25_fields=["symbol_name", "file_path"])
            },
            node_types=list(NODE_VOCABULARY),
            edge_types=list(EDGE_VOCABULARY),
            environment=[
                EnvName(
                    "SAMPLE_PY_ROOT",
                    "path",
                    "An optional default for the `path` param, read only when a collect sends none.",
                )
            ],
            context=[],
        )

    def walk(self, collect_id, params, foreign):
        params = params or {}
        path = params.get("path") or os.environ.get("SAMPLE_PY_ROOT", "")
        if not path:
            raise ValueError("no directory to walk: send a `path` param or set SAMPLE_PY_ROOT")
        if not os.path.isdir(path):
            # A source that is not there is a FAILED walk, never an empty
            # successful one: an empty complete collect would assert that the
            # directory exists and holds nothing.
            raise ValueError("%s is not a directory" % path)

        limit = params.get("limit") or 0
        root = os.path.abspath(path)
        entries = sorted(os.listdir(root))
        truncated = False
        if limit and len(entries) > limit:
            entries = entries[:limit]
            truncated = True

        nodes = [
            Node(
                id=root,
                type=NODE_TYPE_DIRECTORY,
                symbol_name=os.path.basename(root) or root,
                file_path=root,
                metadata={"entries": str(len(entries))},
            )
        ]
        edges = []
        for entry in entries:
            child = os.path.join(root, entry)
            nodes.append(
                Node(
                    id=child,
                    type=NODE_TYPE_FILE,
                    symbol_name=entry,
                    file_path=child,
                    metadata={"is_dir": "true" if os.path.isdir(child) else "false"},
                )
            )
            edges.append(Edge(from_id=root, to_id=child, type=EDGE_TYPE_CONTAINS))

        if truncated:
            complete = Incomplete("stopped at the caller's limit of %d entries" % limit)
        else:
            complete = Complete()
        return Result(nodes=nodes, edges=edges, complete=complete)


if __name__ == "__main__":
    sys.exit(main(DirectoryCollector()))

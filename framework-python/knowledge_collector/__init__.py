# SPDX-License-Identifier: Apache-2.0

"""knowledge_collector — the Python port of the knowledge collector framework.

A collector author writes a WALK and a params schema; this package serves that
walk as an MCP tool over stdio, advertises the collector contract's input and
output schemas, validates the call's params before the walk runs, and encodes the
walk's result as the contract envelope.

    from knowledge_collector import Collector, Complete, Edge, Node, Result, ToolSpec, serve_stdio

IT TAKES NO RUNTIME DEPENDENCY. Everything here is the standard library, which is
what lets a user copy the package into their own project and run its tests with a
bare interpreter and no install step. The cost is that the MCP speaking layer is
this package's own (speaker.py) rather than an SDK's, and that cost is bounded by
the surface being four messages fixed by a checked-in contract.

WHAT IS AND IS NOT HERE, against the Go framework this ports:

  * No `--version` switch. The knowledge client never reads a collector's banner.
  * No streamable-HTTP entry point. A `type: http` entry is served by whatever
    host the operator fronts it with; this package serves the stdio shape a
    daemon spawns.
  * No pre-emit output validator. The client validates every result it receives,
    so one here would duplicate a check the consumer performs -- and would then
    have to be bypassable for a conformance stub that must break its own word.
    `knowledge_collector.contract` exposes the client's gates for an author who
    wants to run them at their own test time.
"""

from .completeness import Complete, Completeness, Incomplete
from .context import FAMILY_CODE, ForeignContext, ForeignEdge, ForeignGraph, ForeignNode
from .contract import ContractError, check_tool_schemas, decode_result, validate_result_payload
from .describe import (
    DESCRIBE_TOOL_NAME,
    ENV_CLASSES,
    BehaviorDefaults,
    Declaration,
    DeclarationError,
    EnvName,
    FamilyDeclaration,
    NodeTypeBehavior,
)
from .envelope import EnvelopeError, encode_result
from .framework import DEFAULT_TOOL_NAME, Collector, Edge, Node, Result, ToolSpec
from .schema import (
    SchemaDeclarationError,
    advertised_input_schema,
    advertised_output_schema,
    describe_contract_json,
    input_contract_json,
    output_contract_json,
)
from .serve import IMPLEMENTATION_VERSION, main, new_speaker, resolved_tool_name, serve_stdio
from .speaker import PROTOCOL_VERSION, Speaker, ToolDefinition

__all__ = [
    "BehaviorDefaults",
    "Collector",
    "Complete",
    "Completeness",
    "ContractError",
    "DEFAULT_TOOL_NAME",
    "DESCRIBE_TOOL_NAME",
    "Declaration",
    "DeclarationError",
    "ENV_CLASSES",
    "Edge",
    "EnvName",
    "EnvelopeError",
    "FAMILY_CODE",
    "FamilyDeclaration",
    "ForeignContext",
    "ForeignEdge",
    "ForeignGraph",
    "ForeignNode",
    "IMPLEMENTATION_VERSION",
    "Incomplete",
    "Node",
    "NodeTypeBehavior",
    "PROTOCOL_VERSION",
    "Result",
    "SchemaDeclarationError",
    "Speaker",
    "ToolDefinition",
    "ToolSpec",
    "advertised_input_schema",
    "advertised_output_schema",
    "check_tool_schemas",
    "decode_result",
    "describe_contract_json",
    "encode_result",
    "input_contract_json",
    "main",
    "new_speaker",
    "output_contract_json",
    "resolved_tool_name",
    "serve_stdio",
    "validate_result_payload",
]

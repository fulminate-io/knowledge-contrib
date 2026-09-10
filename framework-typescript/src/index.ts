// index.ts — the public surface of the TypeScript collector framework.
//
// A collector author needs six things from this package: the Collector interface,
// the node and edge model, the completeness assertion, the foreign-context type,
// the stdio entry point, and — for their own tests — the three contract gates the
// client will run against them. Everything else is internal and is not exported.

export {
  DEFAULT_TOOL_NAME,
  defaultedToolName,
  type Collector,
  type CollectorEdge,
  type CollectorNode,
  type JsonSchema,
  type Result,
  type ToolSpec,
} from "./framework.js";

export { Completeness, complete, incomplete, isCompleteness } from "./completeness.js";

export {
  FAMILY_CODE,
  ForeignContext,
  decodeForeignContext,
  type ForeignEdge,
  type ForeignGraph,
  type ForeignNode,
} from "./context.js";

export { encodeResult, type CollectOutput } from "./envelope.js";

export {
  advertisedDescribeSchema,
  advertisedInputSchema,
  advertisedOutputSchema,
  describeContractJSON,
  inputContractJSON,
  outputContractJSON,
} from "./schema.js";

export {
  DESCRIBE_TOOL_NAME,
  DeclarationError,
  ENV_CLASSES,
  describeInputSchema,
  renderDeclaration,
  type BehaviorDeclaration,
  type Declaration,
  type EnvClass,
  type EnvDeclaration,
  type ForeignFamilyDeclaration,
  type NodeTypeOverride,
} from "./describe.js";

export {
  checkToolSchemas,
  decodeResult,
  schemaSatisfies,
  validateResultPayload,
} from "./contract.js";

export {
  PREFERRED_PROTOCOL_VERSION,
  SUPPORTED_PROTOCOL_VERSIONS,
  Session,
  negotiateProtocolVersion,
  serveSpeakerOverStdio,
  type CallToolResult,
  type SpeakerDefinition,
  type SpeakerIO,
  type TextContent,
  type ToolDefinition,
} from "./speaker.js";

export { IMPLEMENTATION_VERSION, isMainModule, newSpeakerDefinition, serveStdio } from "./serve.js";

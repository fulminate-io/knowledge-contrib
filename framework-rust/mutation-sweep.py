#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Mutation sweep: for every guard, branch, invariant and feature body this
change adds, delete or invert it and confirm a NAMED test turns red.

A guard whose absence leaves the suite green is unobserved and ships as a
liability. This script is the evidence that none of them is.

Each row: (label, file, old, new, test target, test name). The anchor must be
found exactly once, or the row aborts rather than silently mutating nothing.
"""

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent

ROWS = [
    # ---- envelope.rs -----------------------------------------------------
    (
        "envelope: the incomplete-with-no-reason refusal",
        "src/envelope.rs",
        '        if reason.is_empty() {\n            return Err(Error::new(format!(',
        '        if false {\n            return Err(Error::new(format!(',
        "envelope",
        "an_incomplete_walk_with_no_reason_is_refused",
    ),
    (
        "envelope: the empty-node-type refusal",
        "src/envelope.rs",
        "        if node.node_type.is_empty() {",
        "        if false {",
        "envelope",
        "a_node_with_an_empty_type_is_refused_naming_the_index_and_the_id",
    ),
    (
        "envelope: nodes and edges carry no skip-if-empty",
        "src/envelope.rs",
        "pub struct CollectOutput {\n    pub nodes: Vec<Node>,",
        'pub struct CollectOutput {\n    #[serde(default, skip_serializing_if = "Vec::is_empty")]\n    pub nodes: Vec<Node>,',
        "envelope",
        "every_list_serializes_as_an_array_never_null",
    ),
    (
        "envelope: walk_complete carries no skip-if-empty",
        "src/envelope.rs",
        "    pub walk_complete: bool,",
        '    #[serde(default, skip_serializing_if = "std::ops::Not::not")]\n    pub walk_complete: bool,',
        "envelope",
        "walk_complete_is_always_emitted_including_on_the_incomplete_arm",
    ),
    # ---- model.rs --------------------------------------------------------
    (
        "model: deny_unknown_fields on Node",
        "src/model.rs",
        '#[serde(deny_unknown_fields)]\npub struct Node {',
        "pub struct Node {",
        "payload",
        "decode_result_refuses_an_undefined_field",
    ),
    (
        "model: the node's optional fields skip when empty",
        "src/model.rs",
        '    #[serde(default, skip_serializing_if = "String::is_empty")]\n    pub symbol_name: String,',
        "    #[serde(default)]\n    pub symbol_name: String,",
        "envelope",
        "a_minimal_node_emits_only_id_and_type",
    ),
    (
        "model: the edge's graph fields skip when empty",
        "src/model.rs",
        '    #[serde(default, skip_serializing_if = "String::is_empty")]\n    pub source_graph: String,',
        "    #[serde(default)]\n    pub source_graph: String,",
        "envelope",
        "the_edges_two_graph_fields_serialize_away_when_empty",
    ),
    # ---- schema.rs -------------------------------------------------------
    (
        "schema: the params-type-must-be-an-object refusal",
        "src/schema.rs",
        'if declared != Some("object") {',
        'if false && declared != Some("object") {',
        "schemas",
        "params_type_must_infer_an_object",
    ),
    (
        "schema: the input splice touches params and nothing else",
        "src/schema.rs",
        '    props.insert("params".to_string(), Value::Object(params));',
        '    props.insert("params".to_string(), Value::Object(params));\n    doc.insert("required".to_string(), serde_json::json!(["id", "params"]));',
        "schemas",
        "advertised_input_schema_is_the_contract_with_only_params_replaced",
    ),
    (
        "schema: the output schema is the contract FILE",
        "src/schema.rs",
        'pub fn advertised_output_schema() -> Result<Map<String, Value>> {\n    parse_contract("output", OUTPUT_CONTRACT_JSON)\n}',
        'pub fn advertised_output_schema() -> Result<Map<String, Value>> {\n    let Value::Object(m) = serde_json::json!({"type": "object"}) else { unreachable!() };\n    Ok(m)\n}',
        "schemas",
        "advertised_output_schema_is_the_contract_file_verbatim",
    ),
    # ---- serve.rs --------------------------------------------------------
    (
        "serve: the advertised-input-schema check on the call arguments",
        "src/serve.rs",
        "        self.validate_call_arguments(arguments)\n            .map_err(|e| e.to_string())?;",
        "        // mutation: the schema check removed",
        "handler",
        "a_params_value_outside_the_declared_bound_is_refused_by_the_schema_check",
    ),
    (
        "serve: deny_unknown_fields on the collect input",
        "src/serve.rs",
        "#[derive(Debug, Deserialize)]\n#[serde(deny_unknown_fields)]\nstruct CollectInput<P> {",
        "#[derive(Debug, Deserialize)]\nstruct CollectInput<P> {",
        "handler",
        "a_call_with_an_undeclared_top_level_property_is_refused_naming_it",
    ),
    (
        "serve: the empty-collect-id refusal",
        "src/serve.rs",
        "        if input.id.is_empty() {",
        "        if false {",
        "handler",
        "an_empty_collect_id_is_refused_before_the_walk",
    ),
    (
        "serve: the panic containment around the walk",
        "src/serve.rs",
        "        let walked = std::panic::catch_unwind(AssertUnwindSafe(|| {\n            self.collector.walk(&input.id, input.params, &input.context)\n        }));",
        "        let walked: std::result::Result<_, Box<dyn std::any::Any + Send>> =\n            Ok(self.collector.walk(&input.id, input.params, &input.context));",
        "handler",
        "a_panicking_walk_is_contained_and_reported",
    ),
    (
        "serve: the walk-failure arm",
        "src/serve.rs",
        '                return Err(format!("{} collector: the walk failed: {e}", self.tool_name));',
        '                let _ = e;\n                return Ok(serde_json::json!({"nodes": [], "edges": [], "walk_complete": true}));',
        "handler",
        "a_failed_walk_is_a_tool_error_with_text",
    ),
    (
        "serve: the pinned protocol revision",
        "src/serve.rs",
        "pub const PROTOCOL_VERSION: ProtocolVersion = ProtocolVersion::V_2025_06_18;",
        "pub const PROTOCOL_VERSION: ProtocolVersion = ProtocolVersion::LATEST;",
        "handshake",
        "the_handshake_answers_the_pinned_revision",
    ),
    (
        "serve: serverInfo.name is the tool name",
        "src/serve.rs",
        "        info.server_info = Implementation::new(self.tool_name.clone(), IMPLEMENTATION_VERSION);",
        '        info.server_info = Implementation::new("knowledge-collector", IMPLEMENTATION_VERSION);',
        "handshake",
        "server_info_name_is_the_tool_name",
    ),
    (
        "serve: the unknown-tool protocol error",
        "src/serve.rs",
        "        if request.name != self.tool_name {",
        "        if false {",
        "handshake",
        "an_unknown_tool_is_a_protocol_error",
    ),
    (
        "serve: the advertised output schema reaches the tool",
        "src/serve.rs",
        "        tool.output_schema = Some(Arc::new(output.clone()));",
        "        tool.output_schema = None;",
        "handshake",
        "the_tool_listing_carries_the_contract_documents",
    ),
    # ---- contract.rs -----------------------------------------------------
    (
        "contract: the comparator's type check",
        "src/contract.rs",
        "    if let Some(want) = scalar_type(contract) {",
        "    if let Some(want) = None::<String>.or(scalar_type(contract)).filter(|_| false) {",
        "comparator",
        "check_tool_schemas_refuses_each_loosening_individually",
    ),
    (
        "contract: the comparator's required-list loop",
        "src/contract.rs",
        "    for req in string_list(contract.get(\"required\")) {\n        if !advertised_required.iter().any(|r| r == &req) {",
        "    for req in string_list(contract.get(\"required\")) {\n        if false && !advertised_required.iter().any(|r| r == &req) {",
        "comparator",
        "check_tool_schemas_refuses_each_loosening_individually",
    ),
    (
        "contract: the optional-property discriminator reads the CONTRACT's required list",
        "src/contract.rs",
        "                if !contract_required.iter().any(|r| r == name) {\n                    continue;\n                }",
        "                if !advertised_required.iter().any(|r| r == name) {\n                    continue;\n                }",
        "comparator",
        "check_tool_schemas_admits_a_provider_lacking_an_optional_property",
    ),
    (
        "contract: the comparator's item-schema check",
        "src/contract.rs",
        "    if let Some(items) = contract.get(\"items\") {",
        "    if let Some(items) = contract.get(\"items\").filter(|_| false) {",
        "comparator",
        "check_tool_schemas_refuses_each_loosening_individually",
    ),
    (
        "contract: the comparator ADMITS a stricter provider",
        "src/contract.rs",
        "    let advertised_required = string_list(advertised.get(\"required\"));",
        "    let advertised_required = string_list(advertised.get(\"required\"));\n    if advertised_required != string_list(contract.get(\"required\")) {\n        return Err(Error::new(format!(\"{path}: strictness refused\")));\n    }",
        "comparator",
        "check_tool_schemas_accepts_the_contract_verbatim_and_stricter",
    ),
    (
        "contract: the result-payload validator",
        "src/contract.rs",
        "    if failures.is_empty() {\n        return Ok(());\n    }\n    Err(Error::new(format!(\n        \"custom collector: tool {tool:?} result does not satisfy the collector contract's output schema: {}\",",
        "    if true {\n        return Ok(());\n    }\n    Err(Error::new(format!(\n        \"custom collector: tool {tool:?} result does not satisfy the collector contract's output schema: {}\",",
        "payload",
        "validate_result_payload_refuses_non_conforming_results",
    ),
    (
        "contract: the no-structured-content refusal",
        "src/contract.rs",
        "    let Some(structured) = structured else {",
        "    let Some(structured) = structured.or(Some(&Value::Null)) else {",
        "payload",
        "decode_result_refuses_no_structured_content",
    ),
    (
        "contract: the missing-schema refusals",
        "src/contract.rs",
        "    let Some(input) = advertised_input else {",
        "    let Some(input) = advertised_input.or(Some(&Value::Null)) else {",
        "comparator",
        "check_tool_schemas_missing_schemas",
    ),
    # ---- context.rs ------------------------------------------------------
    (
        "context: declared() distinguishes a present key from an absent one",
        "src/context.rs",
        "    pub fn declared(&self, family: &str) -> bool {\n        self.0.contains_key(family)\n    }",
        "    pub fn declared(&self, family: &str) -> bool {\n        let _ = family;\n        true\n    }",
        "foreign_context",
        "a_declared_family_with_no_graphs_is_distinguishable_from_an_undeclared_one",
    ),
    (
        "context: except() excludes the families it is given",
        "src/context.rs",
        "            .filter(|(name, _)| !families.contains(&name.as_str()))",
        "            .filter(|(name, _)| families.contains(&name.as_str()) || true)",
        "foreign_context",
        "except_flattens_every_family_but_the_named_ones_in_sorted_order",
    ),
    # ---- describe.rs -----------------------------------------------------
    (
        "describe: the closed env-class set",
        "src/describe.rs",
        '            "secret" => Ok(EnvClass::Secret),',
        '            "secret" => Ok(EnvClass::Secret),\n            _other if true => Ok(EnvClass::Selector),',
        "describe",
        "an_env_class_outside_the_closed_set_is_refused_naming_it",
    ),
    (
        "describe: the selector-beside-a-list refusal",
        "src/describe.rs",
        "        if self.all_node_types && !self.node_types.is_empty() {",
        "        if false {",
        "describe",
        "the_family_selector_beside_a_node_type_list_is_refused_naming_both",
    ),
    (
        "describe: render drops the author-facing reason",
        "src/describe.rs",
        '                m.insert("class".into(), Value::String(v.class.as_str().into()));',
        '                m.insert("class".into(), Value::String(v.class.as_str().into()));\n                if let Some(r) = &v.reason {\n                    m.insert("reason".into(), Value::String(r.clone()));\n                }',
        "describe",
        "the_rendered_declaration_never_carries_a_reason_key",
    ),
    (
        "describe: the malformed-env-name refusals",
        "src/describe.rs",
        "        if self.name.contains('=') {",
        "        if false {",
        "describe",
        "a_malformed_env_name_is_refused",
    ),
    (
        "describe: the override-for-an-undeclared-type refusal",
        "src/describe.rs",
        "            if !self.vocabulary.node_types.iter().any(|t| t == node_type) {",
        "            if false {",
        "describe",
        "a_field_override_for_an_undeclared_node_type_is_refused",
    ),
    (
        "describe: uncovered_node_types reports what the vocabulary omits",
        "src/describe.rs",
        "            .filter(|t| !self.node_types.iter().any(|d| d == t))\n            .map(str::to_string)\n            .collect();\n        missing.sort();\n        missing.dedup();\n        missing\n    }\n\n    /// uncovered_edge_types",
        "            .filter(|_| false)\n            .map(str::to_string)\n            .collect();\n        missing.sort();\n        missing.dedup();\n        missing\n    }\n\n    /// uncovered_edge_types",
        "sample",
        "the_declared_vocabulary_covers_every_type_the_walk_emits",
    ),
    # ---- sample.rs -------------------------------------------------------
    (
        "sample: the truncated walk asserts INCOMPLETE",
        "src/sample.rs",
        "        let complete = match truncated_at {",
        "        let complete = match None::<String>.or(truncated_at).filter(|_| false) {",
        "sample",
        "a_truncated_walk_asserts_incomplete_with_its_reason",
    ),
    (
        "sample: the declared vocabulary names every emitted type",
        "src/sample.rs",
        "                vec![\n                    NODE_TYPE_DIRECTORY.to_string(),\n                    NODE_TYPE_FILE.to_string(),\n                ],",
        "                vec![NODE_TYPE_DIRECTORY.to_string()],",
        "sample",
        "the_declared_vocabulary_covers_every_type_the_walk_emits",
    ),
    (
        "sample: the blank-root refusal",
        "src/sample.rs",
        "        if params.root.is_empty() {",
        "        if false {",
        "sample",
        "a_root_that_names_no_directory_is_an_error_not_an_empty_graph",
    ),
    (
        "sample: the walk is deterministic (entries are sorted)",
        "src/sample.rs",
        "    entries.sort_by_key(std::fs::DirEntry::path);",
        "    entries.sort_by_key(|e| std::cmp::Reverse(e.path()));",
        "sample",
        "the_walk_emits_a_connected_directory_graph",
    ),
    # ---- conformance.rs --------------------------------------------------
    (
        "conformance: the dial marker on stderr (the seam-engagement proof)",
        "src/conformance.rs",
        '    eprintln!("{STUB_STDERR_MARKER}: dialed mode={} exe={exe}", mode.as_str());',
        '    let _ = &exe;',
        "stub_modes",
        "every_dial_writes_the_marker_to_stderr_and_never_to_stdout",
    ),
    (
        "conformance: exit-mid-session really exits",
        "src/conformance.rs",
        '            eprintln!("{STUB_STDERR_MARKER}: exiting mid-session, deliberately");\n            std::process::exit(7);',
        "            return Ok(CallToolResponse::Complete(CallToolResult::error(vec![\n                ContentBlock::text(\"pretending to die\"),\n            ])));",
        "stub_modes",
        "exit_mid_session_serves_the_handshake_then_dies_inside_the_call",
    ),
    (
        "conformance: exit-before-handshake really exits",
        "src/conformance.rs",
        '        eprintln!("{STUB_STDERR_MARKER}: exiting before the handshake, deliberately");\n        std::process::exit(3);',
        '        eprintln!("{STUB_STDERR_MARKER}: not exiting after all");',
        "stub_modes",
        "exit_before_handshake_dies_before_writing_a_frame",
    ),
    (
        "conformance: the mode vocabulary refuses an unknown name",
        "src/conformance.rs",
        "            .find(|m| m.as_str() == name)\n            .ok_or_else(|| {",
        "            .find(|m| m.as_str() == name)\n            .or(Some(Mode::Conforming))\n            .ok_or_else(|| {",
        "stub_modes",
        "the_mode_vocabulary_is_complete_and_unique",
    ),
    (
        "conformance: the conforming payload's present-and-empty node",
        "src/conformance.rs",
        '            {\n                "id": "ISSUE-3", "type": "issue",',
        '            {\n                "id": "ISSUE-3", "type": "issue", "dropped": true,',
        "stub_modes",
        "the_conforming_payload_carries_the_absent_and_the_empty_node",
    ),
    (
        "conformance: env-report answers only the names asked",
        "src/conformance.rs",
        "    let names = asked_names(arguments);",
        "    let mut names = asked_names(arguments);\n    names.extend(std::env::vars().map(|(k, _)| k));",
        "stub_modes",
        "env_report_answers_only_the_names_asked_and_never_the_environment",
    ),
    (
        "conformance: the over-cap payload crosses the retired bound",
        "src/conformance.rs",
        "pub const OVER_FORMER_CAP_NODES: usize = 68;",
        "pub const OVER_FORMER_CAP_NODES: usize = 4;",
        "stub_modes",
        "over_former_cap_crosses_the_retired_bound_and_the_process_survives",
    ),
    (
        "conformance: the tool-error literal",
        "src/conformance.rs",
        'pub const TOOL_ERROR_TEXT: &str = "the stub provider refused this collect";',
        'pub const TOOL_ERROR_TEXT: &str = "refused";',
        "stub_modes",
        "tool_error_returns_the_literal_refusal",
    ),
    (
        "conformance: the four schema-bending modes bend their schemas",
        "src/conformance.rs",
        "            Mode::NoOutputSchema => None,",
        "            Mode::NoOutputSchema => Some(contract_schema_map(OUTPUT_CONTRACT_JSON)),",
        "stub_modes",
        "every_mode_advertises_and_answers_what_the_client_asserts",
    ),
    # ---- the publish trap and the manifest --------------------------------
    (
        "publish trap: the byte reader actually reads",
        "tests/publish_trap.rs",
        "    haystack\n        .windows(needle.len())\n        .any(|window| window == needle)",
        "    let _ = (haystack, needle);\n    false",
        "publish_trap",
        "no_shipped_file_carries_the_in_repo_module_path",
    ),
    (
        "publish trap: the byte scan over every shipped file",
        "tests/publish_trap.rs",
        "        if let Some(kind) = offence(&bytes) {\n            offenders.push(format!(\"{} ({})\", path.display(), kind.describe()));\n        }",
        "        let _ = &bytes;",
        "publish_trap",
        "no_shipped_file_carries_the_in_repo_module_path",
    ),
    (
        "publish trap: the walk prunes NOTHING",
        "tests/publish_trap.rs",
        "            Ok(meta) if meta.is_dir() => every_file(&path, out),",
        '            Ok(meta) if meta.is_dir() => {\n                if path.file_name().is_some_and(|n| n == "src") {\n                    continue;\n                }\n                every_file(&path, out)\n            }',
        "publish_trap",
        "no_shipped_file_carries_the_in_repo_module_path",
    ),
    (
        "publish trap: the CLIENT MODULE needle, the wider class",
        "tests/publish_trap.rs",
        "    let at = find(bytes, client.as_bytes())?;",
        "    let at = find(bytes, client.as_bytes()).filter(|_| false)?;",
        "publish_trap",
        "no_shipped_file_carries_the_in_repo_module_path",
    ),
    (
        "publish trap: the URL exemption is an exemption, not a blanket pass",
        "tests/publish_trap.rs",
        "    if at >= SCHEME.len() && &bytes[at - SCHEME.len()..at] == SCHEME {\n        return None;\n    }",
        "    if at >= SCHEME.len() {\n        return None;\n    }",
        "publish_trap",
        "no_shipped_file_carries_the_in_repo_module_path",
    ),
    (
        "publish trap: the build output lives outside the crate",
        "tests/publish_trap.rs",
        '    let target = crate_root().join("target");\n    assert!(\n        !target.exists(),',
        '    let target = crate_root().join("src");\n    assert!(\n        !target.exists(),',
        "publish_trap",
        "the_crate_directory_holds_no_build_output",
    ),
    (
        "manifest: the separator is a TAB",
        "collector-manifest.tsv",
        "language\trust",
        "language rust",
        "packaging",
        "the_port_manifest_ships_and_declares_its_four_keys",
    ),
    (
        "manifest: the declared commands pin the lockfile",
        "collector-manifest.tsv",
        "test\tcargo test --locked",
        "test\tcargo test",
        "packaging",
        "the_port_manifest_ships_and_declares_its_four_keys",
    ),
    # ---- the fix round's own additions ------------------------------------
    (
        "packaging: the two binary targets are named in the index",
        "tests/packaging.rs",
        'for name in ["src/bin/sample-rs.rs", "src/bin/conformance-stub.rs"] {',
        'for name in ["src/bin/sample-rs.rs", "src/bin/conformance-stub.rs", "src/bin/NOPE.rs"] {',
        "packaging",
        "no_source_file_the_crate_needs_is_ignored_or_untracked",
    ),
    (
        "packaging: the ignored-path reader actually reads",
        "tests/packaging.rs",
        '            "--ignored",\n            "--exclude-standard",\n            "--directory",\n            "--",\n            "target",',
        '            "--exclude-standard",\n            "--directory",\n            "--",\n            "target",',
        "packaging",
        "no_source_file_the_crate_needs_is_ignored_or_untracked",
    ),
    (
        "packaging: every .rs file on disk is in the index",
        "tests/packaging.rs",
        "            .filter(|f| !tracked.iter().any(|t| t == *f))",
        "            .filter(|f| { let _ = f; false })",
        "packaging",
        "no_source_file_the_crate_needs_is_ignored_or_untracked",
    ),
    (
        "packaging: the ship-set matcher covers this crate",
        "tests/packaging.rs",
        "            !prefix.is_empty() && path.starts_with(prefix)",
        "            !prefix.is_empty() && path.starts_with(prefix) && false",
        "packaging",
        "both_leak_instruments_cover_this_crate",
    ),
    (
        "packaging: the ship-set matcher is not a blanket yes",
        "tests/packaging.rs",
        "            let prefix = p.trim_end_matches('*');\n            !prefix.is_empty() && path.starts_with(prefix)",
        "            let _ = p;\n            let _ = path;\n            true",
        "packaging",
        "both_leak_instruments_cover_this_crate",
    ),
    (
        "sample: the undeclared-type switch emits an undeclared type",
        "src/sample.rs",
        'pub const UNDECLARED_NODE_TYPE: &str = "undeclared_probe";',
        'pub const UNDECLARED_NODE_TYPE: &str = "file";',
        "sample",
        "the_sample_can_emit_a_type_its_declaration_omits",
    ),
    (
        "pin 8: the README mirrors the framework's section structure",
        "README.md",
        "## Where the rest is written",
        "## Elsewhere",
        "pending_pins",
        "pin_8_this_readme_mirrors_the_framework_readme",
    ),
    # PIN 9 HAS NO ROW HERE, and the reason is a property of the pin rather than
    # an omission. Its owning artifact — the ingest-side vocabulary refusal —
    # exists on no branch, so the pin is in its SKIP arm whatever this sweep does
    # to it, and a mutation that leaves a skip a skip is not a red. Its assert arm
    # was exercised separately, against a synthesized refusal site written into a
    # scratch copy of the server's ingest path and removed immediately: the arm
    # was reached, and removing the end-to-end script's R3.7 arm turned it red.
    # Pins 1 and 5 are exercised the same way at the branch that carries THEIR
    # artifacts. A pin whose artifact does not exist anywhere cannot be killed
    # here, and saying so is better than a row that always reports green.
    # ---- the describe round's additions -----------------------------------
    (
        "pins: the constant reader admits the single-line const form",
        "tests/pending_pins.rs",
        '        let line = line.strip_prefix("const ").unwrap_or(line).trim_start();',
        "        // mutation: the const prefix is not stripped",
        "pending_pins",
        "the_go_constant_reader_handles_every_declaration_form",
    ),
    (
        "pins: the constant reader requires a token boundary after the name",
        "tests/pending_pins.rs",
        "        if !rest.starts_with(|c: char| c.is_whitespace() || c == '=') {\n            continue;\n        }",
        "        // mutation: the name may match a longer one",
        "pending_pins",
        "the_go_constant_reader_handles_every_declaration_form",
    ),
    (
        "pin 6: the guard PARSES rather than substring-matching",
        "tests/pending_pins.rs",
        '        let line = line.strip_prefix("const ").unwrap_or(line).trim_start();',
        "        // mutation: the const prefix is not stripped, so the reader is blind",
        "pending_pins",
        "pin_6_the_framework_declares_the_describe_tool_name",
    ),
    (
        "serve: the declaration tool is published",
        "src/serve.rs",
        "        vec![self.tool.clone(), self.describe_tool.clone()]",
        "        vec![self.tool.clone()]",
        "describe_tool",
        "the_declaration_tool_is_served_beside_the_collect_tool",
    ),
    (
        "serve: the describe tool advertises the CONTRACT document",
        "src/serve.rs",
        "        describe_tool.output_schema = Some(Arc::new(describe.clone()));",
        "        describe_tool.output_schema = Some(Arc::new(empty_object_schema()));",
        "describe_tool",
        "the_declaration_tool_is_served_beside_the_collect_tool",
    ),
    (
        "serve: the declaration is validated when the server is built",
        "src/serve.rs",
        "        let declaration = collector.declaration().render()?;",
        "        let declaration = collector.declaration().render().unwrap_or(Value::Null);",
        "describe_tool",
        "a_malformed_declaration_refuses_the_server_at_construction",
    ),
    (
        "describe: the rendered document carries the required behavior object",
        "src/describe.rs",
        '        doc.insert("behavior".into(), Value::Object(behavior));',
        "        let _ = behavior;",
        "describe_tool",
        "the_declaration_answered_on_the_wire_satisfies_the_advertised_schema",
    ),
    (
        "describe: the no-family refusal",
        "src/describe.rs",
        "        if self.family.is_empty() {",
        "        if false && self.family.is_empty() {",
        "describe",
        "a_foreign_context_declaration_naming_no_family_is_refused",
    ),
    (
        "describe: the no-family refusal NAMES the offending declaration",
        "src/describe.rs",
        '            return Err(Error::new(format!(\n                "framework: foreign-context declaration [{index}] names no family; it selects \\\n                 {selection}, and a declaration with no family names no graph type to read it from"\n            )));',
        '            let _ = (index, selection);\n            return Err(Error::new("framework: a foreign-context declaration names no family"));',
        "describe",
        "a_foreign_context_declaration_naming_no_family_is_refused",
    ),
    (
        "describe: the fourth env class the contract names",
        "src/describe.rs",
        '            "not-carried" => Ok(EnvClass::NotCarried),',
        "            // mutation: the fourth class is not admitted",
        "describe",
        "an_env_class_outside_the_closed_set_is_refused_naming_it",
    ),
    (
        "conformance: the four describe modes are in the vocabulary",
        "src/conformance.rs",
        "    Mode::DescribeError,\n];",
        "];",
        "pending_pins",
        "pin_3_this_stub_emulates_every_mode_the_clients_stub_declares",
    ),
    (
        "conformance: the no-describe mode is the only one withholding the tool",
        "src/conformance.rs",
        "        *self != Mode::NoDescribe",
        "        let _ = self;\n        true",
        "describe_tool",
        "the_no_describe_mode_serves_the_collect_tool_alone",
    ),
    (
        "conformance: the bad-describe-schema mode bends its schema",
        "src/conformance.rs",
        '                json!(["behavior", "edge_types", "environment"]),',
        '                json!(["behavior", "edge_types", "environment", "node_types"]),',
        "describe_tool",
        "the_bad_describe_schema_mode_advertises_a_schema_missing_the_vocabulary",
    ),
    (
        "conformance: the bad-declaration mode breaks its own word",
        "src/conformance.rs",
        '                doc.remove("behavior");',
        '                let _ = &mut doc;',
        "describe_tool",
        "the_bad_declaration_mode_serves_a_good_schema_and_breaks_it",
    ),
    (
        "conformance: the describe-error mode reports an error",
        "src/conformance.rs",
        "            Mode::NoDescribe | Mode::DescribeError => None,",
        "            Mode::NoDescribe => None,",
        "describe_tool",
        "the_describe_error_mode_reports_an_error_result",
    ),
    (
        "contract pin: the drift report names the first difference",
        "tests/contract_pin.rs",
        "    if ours == theirs {\n        return None;\n    }",
        "    if true {\n        return None;\n    }",
        "contract_pin",
        "the_drift_report_names_where_two_copies_diverge",
    ),
    # ---- the contract copies ---------------------------------------------
    (
        "the embedded contract copy is byte-identical to the framework module's",
        "contract/collector_output.schema.json",
        '"title": "knowledge custom-collector tool output (v1)"',
        '"title": "knowledge custom-collector tool output (v1) DRIFTED"',
        "contract_pin",
        "every_embedded_contract_copy_matches_the_framework_modules",
    ),
]


def run(row):
    label, rel, old, new, target, test = row
    path = ROOT / rel
    original = path.read_text()
    count = original.count(old)
    if count != 1:
        print(f"ANCHOR ERROR [{label}]: found {count} occurrences in {rel}, expected exactly 1")
        return False
    path.write_text(original.replace(old, new))
    try:
        proc = subprocess.run(
            ["cargo", "test", "--test", target, test, "--", "--exact"],
            cwd=ROOT,
            capture_output=True,
            text=True,
        )
        out = proc.stdout + proc.stderr
        reds = f"test {test} ... FAILED" in out
        compile_error = "error[E" in out or "error: could not compile" in out
        verdict = "RED" if (reds or compile_error) else "GREEN (UNOBSERVED)"
        why = "compile error" if (compile_error and not reds) else "test failed"
        print(f"--- [{label}]")
        print(f"    mutation: {rel}")
        print(f"    expects red: {target}::{test}")
        print(f"    result: {verdict}" + (f" ({why})" if verdict == "RED" else ""))
        for line in out.splitlines():
            if (
                line.startswith("test result:")
                or "FAILED" in line
                or line.startswith("error")
                or "panicked at" in line
            ):
                print(f"    | {line.strip()[:200]}")
        return verdict == "RED"
    finally:
        path.write_text(original)


def main():
    only = sys.argv[1] if len(sys.argv) > 1 else None
    ok = True
    for row in ROWS:
        if only and only not in row[0]:
            continue
        if not run(row):
            ok = False
    print("\nSWEEP VERDICT:", "every mutation reds" if ok else "AT LEAST ONE MUTATION LEFT THE SUITE GREEN")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())

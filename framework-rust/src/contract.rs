// SPDX-License-Identifier: Apache-2.0

//! contract.rs — the COLLECTOR CONTRACT as this crate can check it: whether an
//! advertised schema pair satisfies the checked-in contract, and whether a
//! result payload satisfies the contract's output schema.
//!
//! WHAT THIS MODULE IS FOR, because it is easy to mistake for a second gate on
//! the wire. The knowledge client runs these same two checks on ITS side, at
//! registration and again at every collect. This crate carries its own
//! implementation so a collector author can prove, in their own test suite,
//! that what their collector advertises is ADMISSIBLE and that what it returns
//! is CONFORMING — before a client ever dials them. It is the same rule read
//! from the same checked-in files, not a duplicate policy: the contract schema
//! IS the requirement and [`schema_satisfies`] walks it, exactly as the client's
//! comparator does.
//!
//! THE REQUIRED / OPTIONAL DISTINCTION IS THE SUBTLE PART AND IT IS NOT AN
//! OVERSIGHT. The comparator walks every property the contract declares. Under a
//! naive rule, adding an optional property to the contract — the declared
//! foreign-graph context was the first — would refuse every PRE-EXISTING
//! third-party provider at once, naming a property its author had never heard
//! of. So a provider LACKING an optional property is admitted; a provider that
//! DOES declare one still has it compared, so declaring `context` as a string is
//! refused rather than waved through. THE DISCRIMINATOR IS THE CONTRACT'S OWN
//! REQUIRED LIST, never the advertised one: a provider that lists `id` as
//! required and then declares no `id` property is still refused, by the property
//! loop rather than the required-list loop.

use serde_json::Value;

use crate::envelope::CollectOutput;
use crate::error::{Error, Result};
use crate::schema::{INPUT_CONTRACT_JSON, OUTPUT_CONTRACT_JSON};

/// check_tool_schemas is the contract's HARD GATE, the one the client runs at
/// registration and again at collect: the target tool must advertise BOTH an
/// input schema and an output schema, and both must satisfy the contract.
///
/// `advertised_input` and `advertised_output` are the tool's raw schema values as
/// an MCP tool listing carries them. `None` is the absent case.
pub fn check_tool_schemas(
    tool: &str,
    advertised_input: Option<&Value>,
    advertised_output: Option<&Value>,
) -> Result<()> {
    let Some(input) = advertised_input else {
        return Err(Error::new(format!(
            "custom collector: tool {tool:?} advertises NO input schema; the collector contract requires one (see contract/collector_input.schema.json)"
        )));
    };
    let Some(output) = advertised_output else {
        return Err(Error::new(format!(
            "custom collector: tool {tool:?} advertises NO output schema; the collector contract requires one, including the walk_complete completeness assertion (see contract/collector_output.schema.json)"
        )));
    };
    check_against_contract(tool, "input", INPUT_CONTRACT_JSON, input)?;
    check_against_contract(tool, "output", OUTPUT_CONTRACT_JSON, output)
}

/// check_against_contract compares one advertised schema to the contract schema
/// of the same side.
fn check_against_contract(
    tool: &str,
    side: &str,
    contract_json: &[u8],
    advertised: &Value,
) -> Result<()> {
    let contract: Value = serde_json::from_slice(contract_json).map_err(|e| {
        Error::new(format!(
            "custom collector: the checked-in {side} contract schema is unreadable: {e}"
        ))
    })?;
    if !advertised.is_object() {
        return Err(Error::new(format!(
            "custom collector: tool {tool:?} {side} schema is not a readable JSON Schema: it is not a JSON object"
        )));
    }
    schema_satisfies(&contract, advertised, &format!("{side}Schema")).map_err(|e| {
        Error::new(format!(
            "custom collector: tool {tool:?} {side} schema does not satisfy the collector contract: {e}"
        ))
    })
}

/// schema_satisfies reports whether an advertised schema declares AT LEAST what
/// the contract declares, at every path the contract names: the same type, every
/// required key the contract requires, every property the contract declares, and
/// the item schema of every array.
///
/// It says nothing about the keywords the contract does not name — a provider is
/// free to be STRICTER (extra properties, extra required keys, formats, enums),
/// never looser. The error names the PATH, the expectation and what was found,
/// so a collector author reading the refusal knows which line of their tool
/// definition to fix.
pub fn schema_satisfies(contract: &Value, advertised: &Value, path: &str) -> Result<()> {
    let Some(contract) = contract.as_object() else {
        // A contract sub-schema that is not an object (a `true`/`false` schema)
        // constrains nothing, so there is nothing to compare.
        return Ok(());
    };
    let Some(advertised) = advertised.as_object() else {
        return Err(Error::new(format!(
            "{path}: the contract declares this and the tool does not"
        )));
    };

    // THE TYPE. A union type (`["null","array"]`) reads as NO DECLARED TYPE, the
    // same way the client's comparator reads it: it compares a scalar, so a
    // union is not the scalar the contract names.
    if let Some(want) = scalar_type(contract) {
        let got = scalar_type(advertised);
        if got.as_deref() != Some(want.as_str()) {
            let rendered = got.unwrap_or_else(|| "no declared type".to_string());
            return Err(Error::new(format!(
                "{path}: the contract requires type {want:?}, the tool declares {rendered}"
            )));
        }
    }

    // THE REQUIRED LIST.
    let advertised_required = string_list(advertised.get("required"));
    for req in string_list(contract.get("required")) {
        if !advertised_required.iter().any(|r| r == &req) {
            return Err(Error::new(format!(
                "{path}: the contract requires {req:?} to be a required property, the tool's required list is {advertised_required:?}"
            )));
        }
    }

    // THE PROPERTIES.
    let contract_required = string_list(contract.get("required"));
    if let Some(props) = contract.get("properties").and_then(Value::as_object) {
        let advertised_props = advertised.get("properties").and_then(Value::as_object);
        for (name, sub) in props {
            let advertised_sub = advertised_props.and_then(|p| p.get(name));
            let Some(advertised_sub) = advertised_sub else {
                // AN OPTIONAL PROPERTY THE TOOL DOES NOT DECLARE IS ADMITTED, and
                // the discriminator is the CONTRACT'S OWN required list rather
                // than the advertised one.
                if !contract_required.iter().any(|r| r == name) {
                    continue;
                }
                return Err(Error::new(format!(
                    "{path}: the contract declares the property {name:?} and the tool does not"
                )));
            };
            schema_satisfies(sub, advertised_sub, &format!("{path}.{name}"))?;
        }
    }

    // THE ITEM SCHEMA.
    if let Some(items) = contract.get("items") {
        let Some(advertised_items) = advertised.get("items") else {
            return Err(Error::new(format!(
                "{path}: the contract declares an item schema and the tool does not"
            )));
        };
        schema_satisfies(items, advertised_items, &format!("{path}[]"))?;
    }

    Ok(())
}

/// validate_result_payload validates raw result JSON against the checked-in
/// contract output schema.
///
/// IT IS A DIFFERENT QUESTION FROM THE SCHEMA GATE: a provider may advertise a
/// perfect schema and still return something else. Split from
/// [`decode_result`] so a caller holding bytes validates through the same
/// instrument.
pub fn validate_result_payload(tool: &str, raw: &[u8]) -> Result<()> {
    let contract: Value = serde_json::from_slice(OUTPUT_CONTRACT_JSON).map_err(|e| {
        Error::new(format!(
            "custom collector: the checked-in output contract schema is unreadable: {e}"
        ))
    })?;
    let validator = jsonschema::validator_for(&contract).map_err(|e| {
        Error::new(format!(
            "custom collector: the checked-in output contract schema does not resolve: {e}"
        ))
    })?;
    let instance: Value = serde_json::from_slice(raw).map_err(|e| {
        Error::new(format!(
            "custom collector: tool {tool:?} result is not JSON: {e}"
        ))
    })?;
    let failures: Vec<String> = validator
        .iter_errors(&instance)
        .map(|e| {
            let at = e.instance_path().to_string();
            let at = if at.is_empty() {
                "the result".to_string()
            } else {
                at
            };
            format!("at {at}: {e}")
        })
        .collect();
    if failures.is_empty() {
        return Ok(());
    }
    Err(Error::new(format!(
        "custom collector: tool {tool:?} result does not satisfy the collector contract's output schema: {}",
        failures.join("; ")
    )))
}

/// decode_result validates a tool call's structured content against the CONTRACT
/// output schema and decodes it into the envelope.
///
/// TWO GATES, ANSWERING DIFFERENT QUESTIONS. The schema validation asks whether
/// the payload is a conforming collector result at all; the strict decode asks
/// whether every field it carries is one the contract defines. A key the
/// envelope does not name is an ERROR rather than a silent drop, because a
/// typo'd field name silently dropped is a collector author debugging an empty
/// graph with no message to go on.
pub fn decode_result(tool: &str, structured: Option<&Value>) -> Result<CollectOutput> {
    let Some(structured) = structured else {
        return Err(Error::new(format!(
            "custom collector: tool {tool:?} returned no structuredContent; the contract requires a structured result matching its advertised output schema"
        )));
    };
    let raw = serde_json::to_vec(structured).map_err(|e| {
        Error::new(format!(
            "custom collector: tool {tool:?} result is not JSON: {e}"
        ))
    })?;
    validate_result_payload(tool, &raw)?;
    serde_json::from_slice::<CollectOutput>(&raw).map_err(|e| {
        Error::new(format!(
            "custom collector: tool {tool:?} result carries a field this collector contract does not define: {e}"
        ))
    })
}

/// scalar_type reads a schema's `type` keyword when it is a plain string.
fn scalar_type(schema: &serde_json::Map<String, Value>) -> Option<String> {
    schema.get("type")?.as_str().map(str::to_string)
}

/// string_list reads a schema keyword that holds an array of strings.
fn string_list(value: Option<&Value>) -> Vec<String> {
    value
        .and_then(Value::as_array)
        .map(|items| {
            items
                .iter()
                .filter_map(Value::as_str)
                .map(str::to_string)
                .collect()
        })
        .unwrap_or_default()
}

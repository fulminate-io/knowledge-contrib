// SPDX-License-Identifier: Apache-2.0

//! schema.rs — the SCHEMAS THIS CRATE ADVERTISES, and why they are the
//! checked-in contract files rather than schemas inferred from the Rust types.
//!
//! THE CONTRACT SCHEMAS ARE CHECKED-IN JSON, here as they are on the client
//! side, because a collector author writing a provider in another language reads
//! the file. The two files in `contract/` are a COPY of the client's pair; the
//! copy is kept honest by a test that compares them byte for byte against
//! `../framework/contract/`, which is the one relative path that resolves BOTH
//! in this repository and in the published knowledge-contrib layout. See the
//! crate's `tests/contract_pin.rs` for the sentinel that makes an absent sibling
//! a red rather than a skip.
//!
//! WHY THE OUTPUT SCHEMA IS THE FILE AND NOT AN INFERRED ONE. An inferred output
//! schema is REFUSED by the client's registration gate: a schema generator
//! writes a nullable array as the union type `["null","array"]`, the client's
//! comparator reads a scalar type, finds none, and reports "the tool declares no
//! declared type". Advertising the contract file verbatim closes that for every
//! collector at once — and rmcp takes the file directly, because `Tool` carries
//! its schemas as JSON objects rather than as a schema type it re-renders.
//!
//! WHY THE INPUT SCHEMA IS THE FILE WITH ONE PROPERTY SPLICED IN. The
//! collector's own params schema has to reach the caller, or nothing validates a
//! collect's params; but inferring the WHOLE input from a Rust type puts `params`
//! on the advertised top-level required list, and the client omits `params` from
//! the call arguments entirely when a collect carries none — so a required
//! `params` would refuse every paramless collect before it was sent. Building the
//! input from the contract file keeps its required list at `["id"]` by
//! construction.

use schemars::{JsonSchema, SchemaGenerator};
use serde_json::{Map, Value};

use crate::error::{Error, Result};

/// INPUT_CONTRACT_JSON is this crate's checked-in copy of the collector
/// contract's input schema.
pub const INPUT_CONTRACT_JSON: &[u8] =
    include_bytes!("../contract/collector_input.schema.json");

/// OUTPUT_CONTRACT_JSON is this crate's checked-in copy of the collector
/// contract's output schema.
pub const OUTPUT_CONTRACT_JSON: &[u8] =
    include_bytes!("../contract/collector_output.schema.json");

/// input_contract_json returns the checked-in input contract bytes.
pub fn input_contract_json() -> Vec<u8> {
    INPUT_CONTRACT_JSON.to_vec()
}

/// output_contract_json returns the checked-in output contract bytes.
pub fn output_contract_json() -> Vec<u8> {
    OUTPUT_CONTRACT_JSON.to_vec()
}

/// DESCRIBE_CONTRACT_JSON is this crate's checked-in copy of the collector
/// contract's DESCRIBE output schema — the document the required declaration
/// tool advertises.
pub const DESCRIBE_CONTRACT_JSON: &[u8] =
    include_bytes!("../contract/collector_describe.schema.json");

/// describe_contract_json returns the checked-in describe contract bytes.
pub fn describe_contract_json() -> Vec<u8> {
    DESCRIBE_CONTRACT_JSON.to_vec()
}

/// advertised_describe_schema is the contract's describe schema VERBATIM. The
/// declaration tool takes no arguments, so there is no describe INPUT schema to
/// splice: the tool advertises an empty object for its input and this document
/// for its output.
pub fn advertised_describe_schema() -> Result<Map<String, Value>> {
    parse_contract("describe", DESCRIBE_CONTRACT_JSON)
}

/// parse_contract decodes one of the checked-in contract documents.
fn parse_contract(side: &str, raw: &[u8]) -> Result<Map<String, Value>> {
    let value: Value = serde_json::from_slice(raw).map_err(|e| {
        Error::new(format!(
            "framework: this crate's checked-in {side} contract schema does not decode: {e}"
        ))
    })?;
    match value {
        Value::Object(map) => Ok(map),
        other => Err(Error::new(format!(
            "framework: this crate's checked-in {side} contract schema is a {} rather than an object; the copy is corrupt",
            json_type_name(&other)
        ))),
    }
}

/// advertised_output_schema is the contract output schema VERBATIM.
///
/// It is decoded rather than handed over as bytes only because rmcp's `Tool`
/// carries an object; every key the file holds — including the `$schema`, the
/// descriptions and anything neither this crate nor the client names — survives
/// the round trip and reaches the wire.
pub fn advertised_output_schema() -> Result<Map<String, Value>> {
    parse_contract("output", OUTPUT_CONTRACT_JSON)
}

/// advertised_input_schema is the contract input schema with `properties.params`
/// replaced by the schema inferred from the collector's params type `P`.
///
/// The document is edited as a MAP rather than rebuilt from a schema type: the
/// splice is one property replacement, and a map keeps every keyword the
/// contract file carries.
pub fn advertised_input_schema<P: JsonSchema>() -> Result<Map<String, Value>> {
    let params = params_schema_document::<P>()?;
    let mut doc = parse_contract("input", INPUT_CONTRACT_JSON)?;
    let props = doc
        .get_mut("properties")
        .and_then(Value::as_object_mut)
        .ok_or_else(|| {
            Error::new(
                "framework: this crate's checked-in input contract schema declares no properties object; the copy is corrupt",
            )
        })?;
    if !props.contains_key("params") {
        return Err(Error::new(
            "framework: this crate's checked-in input contract schema declares no params property; the copy is corrupt",
        ));
    }
    props.insert("params".to_string(), Value::Object(params));
    Ok(doc)
}

/// params_schema_document infers the JSON Schema of the collector's params type
/// and returns it as a decoded document.
///
/// IT REFUSES A PARAMS TYPE WHOSE SCHEMA IS NOT AN OBJECT, naming the type. The
/// contract declares `params` as an object and the client's registration gate
/// compares that type, so a collector declaring a string for its params would
/// build here and be refused at registration with an error about a schema it
/// never wrote. A collector with no parameters declares a unit struct, which
/// infers to an object.
pub fn params_schema_document<P: JsonSchema>() -> Result<Map<String, Value>> {
    let schema = SchemaGenerator::default().into_root_schema_for::<P>();
    let value = schema.to_value();
    let Value::Object(doc) = value else {
        return Err(Error::new(format!(
            "framework: the params type {} infers a JSON Schema that is not a document; \
             the collector contract declares params as an object",
            std::any::type_name::<P>()
        )));
    };
    let declared = doc.get("type").and_then(Value::as_str);
    if declared != Some("object") {
        let rendered = match doc.get("type") {
            Some(t) => t.to_string(),
            None => "no declared type".to_string(),
        };
        return Err(Error::new(format!(
            "framework: the params type {} infers the JSON Schema type {rendered}, and the collector contract declares params as an object; \
             declare a struct or a map params type (a unit struct is the shape for a collector with no parameters)",
            std::any::type_name::<P>()
        )));
    }
    Ok(doc)
}

fn json_type_name(v: &Value) -> &'static str {
    match v {
        Value::Null => "null",
        Value::Bool(_) => "boolean",
        Value::Number(_) => "number",
        Value::String(_) => "string",
        Value::Array(_) => "array",
        Value::Object(_) => "object",
    }
}

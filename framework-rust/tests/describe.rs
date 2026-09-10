// SPDX-License-Identifier: Apache-2.0

//! describe.rs — the declaration a collector renders about itself, and every
//! shape the contract refuses.
//!
//! Row R1.17. What is NOT here is the describe TOOL: its name, the third
//! contract schema file that describes its output, and whether the client
//! re-verifies it at every collect are the sibling ticket's, and
//! `tests/pending_pins.rs` carries them as reds that turn green when it lands.

use knowledge_collector_framework::describe::{
    Declaration, EnvClass, EnvVar, ForeignFamilyDeclaration, Vocabulary, ENV_CLASSES,
};
use knowledge_collector_framework::sample::SampleCollector;
use serde_json::Value;

fn minimal() -> Declaration {
    Declaration {
        walks_the_whole_source: true,
        vocabulary: Vocabulary::new(vec!["issue".into()], vec!["blocks".into()]),
        ..Declaration::default()
    }
}

// ---------------------------------------------------------------------------
// R1.17 (a) — an env class outside the closed set is refused, NAMING the class.
// ---------------------------------------------------------------------------
#[test]
fn an_env_class_outside_the_closed_set_is_refused_naming_it() {
    let err = EnvClass::parse("token").expect_err("an unknown env class must be refused");
    assert!(err.message().contains("token"), "{err}");
    for class in ENV_CLASSES {
        assert!(
            err.message().contains(class),
            "the refusal must name the classes it accepts: {err}"
        );
    }

    // THE CONTROL: each of the three parses, so the refusal above is about the
    // class and not about a parser that refuses everything.
    assert_eq!(EnvClass::parse("path").expect("path"), EnvClass::Path);
    assert_eq!(
        EnvClass::parse("selector").expect("selector"),
        EnvClass::Selector
    );
    assert_eq!(EnvClass::parse("secret").expect("secret"), EnvClass::Secret);
    assert_eq!(
        EnvClass::parse("not-carried").expect("not-carried"),
        EnvClass::NotCarried,
        "the contract's describe schema names four classes, and a collector that READS a \
         variable an entry deliberately does not carry says so with this one"
    );
}

// ---------------------------------------------------------------------------
// R1.17 (b) — a family declaration setting the all-node-types selector BESIDE a
// non-empty node-type list is refused, naming BOTH.
// ---------------------------------------------------------------------------
#[test]
fn the_family_selector_beside_a_node_type_list_is_refused_naming_both() {
    let mut declaration = minimal();
    declaration.foreign_context = vec![ForeignFamilyDeclaration {
        family: "code".into(),
        node_types: vec!["function".into()],
        all_node_types: true,
        ..Default::default()
    }];
    let err = declaration
        .validate()
        .expect_err("the selector beside a list must be refused");
    assert!(err.message().contains("all_node_types"), "{err}");
    assert!(err.message().contains("function"), "{err}");
    assert!(err.message().contains("code"), "{err}");

    // THE CONTROL, both halves separately: each alone is admitted.
    let mut selector_only = minimal();
    selector_only.foreign_context = vec![ForeignFamilyDeclaration::all("code")];
    selector_only
        .validate()
        .expect("control: the selector alone is admitted");

    let mut list_only = minimal();
    list_only.foreign_context = vec![ForeignFamilyDeclaration::new(
        "code",
        vec!["function".into()],
    )];
    list_only
        .validate()
        .expect("control: a node-type list alone is admitted");
}

// ---------------------------------------------------------------------------
// A foreign-context declaration that names NO FAMILY is refused, naming what it
// does carry.
//
// THIS ROW EXISTS BECAUSE ITS ABSENCE WAS MEASURED. Neutralizing each of the
// declaration validator's refusals in turn reds a named test for six of the
// seven; this one was the exception, and the whole suite passed with the guard
// disabled. A guard whose absence leaves the suite green is unobserved.
//
// THE FAMILY IS THE GRAPH TYPE THE BLOCK IS READ FROM. A declaration with none
// asks the client to fill a slice from nowhere: the entry it writes would carry
// a key with no name, and the refusal an operator eventually saw would be about
// their config rather than about the collector that produced it.
// ---------------------------------------------------------------------------
#[test]
fn a_foreign_context_declaration_naming_no_family_is_refused() {
    let mut declaration = minimal();
    declaration.foreign_context = vec![
        ForeignFamilyDeclaration::new("code", vec!["function".into()]),
        ForeignFamilyDeclaration::new("", vec!["instance".into(), "bucket".into()]),
    ];

    let err = declaration
        .validate()
        .expect_err("a family declaration with no family must be refused");
    // IT NAMES THE OFFENDING DECLARATION: its position, and the selection it
    // carries, which is the only handle an author has on which one is wrong.
    assert!(err.message().contains("[1]"), "{err}");
    assert!(err.message().contains("instance"), "{err}");
    assert!(err.message().contains("names no family"), "{err}");

    // ...and the selector spelling too, for the other shape a family can take.
    let mut selector = minimal();
    selector.foreign_context = vec![ForeignFamilyDeclaration::all("")];
    let err = selector
        .validate()
        .expect_err("the selector shape is refused on the same terms");
    assert!(err.message().contains("all_node_types"), "{err}");
    assert!(err.message().contains("[0]"), "{err}");

    // THE NEAR MISS, in the same run: the identical declaration WITH a family
    // name renders. Without it this row would pass against a validator that
    // refused every foreign-context declaration.
    let mut named = minimal();
    named.foreign_context = vec![
        ForeignFamilyDeclaration::new("code", vec!["function".into()]),
        ForeignFamilyDeclaration::new("acme-aws", vec!["instance".into(), "bucket".into()]),
    ];
    let rendered = named
        .render()
        .expect("the same declaration with a family name renders");
    assert!(rendered["context"]["acme-aws"].is_object(), "{rendered}");
}

// ---------------------------------------------------------------------------
// R1.17 (c) — the RENDERED declaration never carries a `reason` key, anywhere.
//
// The client's config loader decodes an entry with unknown fields REFUSED, so a
// rendered declaration carrying a key the entry schema does not define is
// refused by name at load time. The author-facing reason is dropped by render.
// ---------------------------------------------------------------------------
#[test]
fn the_rendered_declaration_never_carries_a_reason_key() {
    let mut declaration = minimal();
    declaration.env = vec![
        EnvVar::new("ACME_REGION", EnvClass::Selector)
            .with_reason("the region this collector reads from"),
        EnvVar::new("ACME_TOKEN", EnvClass::Secret).with_reason("the API token"),
    ];

    // THE FIXTURE CONTROL: the reason really is set on the value being rendered,
    // so an absent key below is a drop rather than a value that was never there.
    assert!(declaration.env[0].reason.is_some());

    let rendered = declaration.render().expect("the declaration renders");
    let text = serde_json::to_string(&rendered).expect("the declaration serializes");
    assert!(
        !text.contains("reason"),
        "the rendered declaration must carry no reason key: {text}"
    );

    // ...and the class DOES survive, so the row above is not a render that
    // dropped everything.
    assert!(text.contains(r#""class":"secret""#), "{text}");
    assert!(text.contains("ACME_TOKEN"), "{text}");

    // A SECRET'S VALUE IS NEVER PART OF A DECLARATION: what is rendered is the
    // NAME and the CLASS, and nothing reads the variable.
    let env = rendered["environment"]
        .as_array()
        .expect("environment is an array");
    for entry in env {
        let keys: Vec<&str> = entry
            .as_object()
            .expect("an env entry is an object")
            .keys()
            .map(String::as_str)
            .collect();
        assert_eq!(keys, vec!["class", "name"], "an env entry carries the name and the class and nothing else");
    }
}

// ---------------------------------------------------------------------------
// A blank env name, or one containing '=', is refused: those are the two shapes
// the client's own env-block loader refuses, so a declaration that rendered them
// would produce an entry that cannot load.
// ---------------------------------------------------------------------------
#[test]
fn a_malformed_env_name_is_refused() {
    let mut blank = minimal();
    blank.env = vec![EnvVar::new("", EnvClass::Path)];
    assert!(blank.validate().is_err(), "a blank env name must be refused");

    let mut equals = minimal();
    equals.env = vec![EnvVar::new("A=B", EnvClass::Path)];
    let err = equals
        .validate()
        .expect_err("an env name containing '=' must be refused");
    assert!(err.message().contains("A=B"), "{err}");

    let mut twice = minimal();
    twice.env = vec![
        EnvVar::new("ACME_REGION", EnvClass::Selector),
        EnvVar::new("ACME_REGION", EnvClass::Path),
    ];
    let err = twice
        .validate()
        .expect_err("a name declared twice must be refused");
    assert!(err.message().contains("ACME_REGION"), "{err}");
}

// ---------------------------------------------------------------------------
// A per-node-type field override naming a type the vocabulary does not declare
// is refused: the two would otherwise disagree about what this collector emits.
// ---------------------------------------------------------------------------
#[test]
fn a_field_override_for_an_undeclared_node_type_is_refused() {
    let mut declaration = minimal();
    declaration
        .node_type_fields
        .insert("epic".into(), vec!["summary".into()]);
    let err = declaration
        .validate()
        .expect_err("an override for an undeclared node type must be refused");
    assert!(err.message().contains("epic"), "{err}");
}

// ---------------------------------------------------------------------------
// The sample collector's own declaration renders, and renders the two postures
// the vocabulary distinguishes.
// ---------------------------------------------------------------------------
#[test]
fn the_sample_collectors_declaration_renders() {
    let declaration = SampleCollector::declaration();
    assert!(declaration.vocabulary.is_declared());
    let rendered = declaration.render().expect("the sample's declaration renders");
    assert_eq!(rendered["node_types"], serde_json::json!(["directory", "file"]));
    assert_eq!(rendered["edge_types"], serde_json::json!(["contains"]));
    assert_eq!(rendered["environment"], serde_json::json!([]));

    // AND IT SATISFIES THE CONTRACT'S OWN DESCRIBE SCHEMA. That is the assertion
    // that keeps the render honest: a shape this crate invented would pass every
    // key-by-key check above and be refused by the client.
    let schema: serde_json::Value =
        serde_json::from_slice(&knowledge_collector_framework::schema::describe_contract_json())
            .expect("the describe contract decodes");
    let validator = jsonschema::validator_for(&schema).expect("it resolves");
    let failures: Vec<String> = validator
        .iter_errors(&rendered)
        .map(|e| format!("at {}: {e}", e.instance_path()))
        .collect();
    assert!(
        failures.is_empty(),
        "the sample's rendered declaration does not satisfy the contract's describe schema: {failures:?}"
    );

    // AN UNDECLARED VOCABULARY IS A DIFFERENT POSTURE, not an unfilled field: a
    // collector declaring nothing is accepted and the client says so once per
    // collect, while a declared one is enforced at ingest.
    assert!(!Vocabulary::default().is_declared());
}

// ---------------------------------------------------------------------------
// A declared family renders its selector OR its list, never both keys, so the
// entry the client writes from it cannot say two things.
// ---------------------------------------------------------------------------
#[test]
fn a_family_renders_its_selector_or_its_list_never_both() {
    let mut declaration = minimal();
    declaration.foreign_context = vec![
        ForeignFamilyDeclaration::all("code"),
        ForeignFamilyDeclaration::new("acme-aws", vec!["instance".into()]),
    ];
    let rendered = declaration.render().expect("the declaration renders");
    // THE FROZEN SHAPE keys the declaration by FAMILY NAME under `context`, in
    // the same shape the registration entry carries.
    let context = rendered["context"].as_object().expect("context is an object");

    let code = context["code"].as_object().expect("an object");
    assert_eq!(code["all_node_types"], Value::Bool(true));
    assert!(!code.contains_key("node_types"));

    let aws = context["acme-aws"].as_object().expect("an object");
    assert_eq!(aws["node_types"], serde_json::json!(["instance"]));
    assert!(!aws.contains_key("all_node_types"));
}

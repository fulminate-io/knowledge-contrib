// SPDX-License-Identifier: Apache-2.0

//! sample.rs — the worked example's own walk.
//!
//! Row R3.6, and the crate-side halves of R3.3 (byte-idempotence) and R3.4 (the
//! incomplete assertion and the failed walk). The through-the-client halves of
//! R3.1 to R3.7 run on an isolated client and server pair and are reported
//! separately; these are what turns a defect red in this crate's own CI.

use std::collections::BTreeSet;
use std::fs;
use std::path::{Path, PathBuf};

use knowledge_collector_framework::completeness::Completeness;
use knowledge_collector_framework::envelope::encode_result;
use knowledge_collector_framework::prelude::*;
use knowledge_collector_framework::sample::{
    SampleCollector, SampleParams, EDGE_TYPE_CONTAINS, NODE_TYPE_DIRECTORY, NODE_TYPE_FILE,
};
use knowledge_collector_framework::serve::CollectorServer;
use serde_json::json;

/// fixture_tree builds a small directory tree under a unique scratch path and
/// returns it. It writes only inside the system temporary directory.
fn fixture_tree(name: &str) -> PathBuf {
    let root = std::env::temp_dir().join(format!(
        "kn-framework-rust-{name}-{}-{}",
        std::process::id(),
        name.len()
    ));
    let _ = fs::remove_dir_all(&root);
    fs::create_dir_all(root.join("a/b")).expect("the fixture tree is created");
    fs::write(root.join("top.txt"), b"top").expect("a fixture file is written");
    fs::write(root.join("a/mid.txt"), b"mid").expect("a fixture file is written");
    fs::write(root.join("a/b/deep.txt"), b"deep").expect("a fixture file is written");
    root
}

fn walk(root: &Path, max_depth: u32) -> WalkResult {
    SampleCollector
        .walk(
            "probe",
            SampleParams {
                root: root.to_string_lossy().to_string(),
                max_depth,
                demo_panic: false,
                demo_undeclared_node_type: false,
            },
            &ForeignContext::default(),
        )
        .expect("the sample walk succeeds over a readable tree")
}

// ---------------------------------------------------------------------------
// R3.6 — THE DECLARED VOCABULARY COVERS EVERY TYPE THE WALK EMITS.
//
// The emitted set is DERIVED FROM THE WALK'S OWN OUTPUT rather than typed here:
// a hand-written list on both sides would agree with itself and with nothing
// else. The mutation that reds this is removing one type from the declaration.
// ---------------------------------------------------------------------------
#[test]
fn the_declared_vocabulary_covers_every_type_the_walk_emits() {
    let root = fixture_tree("vocab");
    let result = walk(&root, 0);

    let emitted_nodes: BTreeSet<&str> =
        result.nodes.iter().map(|n| n.node_type.as_str()).collect();
    let emitted_edges: BTreeSet<&str> =
        result.edges.iter().map(|e| e.edge_type.as_str()).collect();

    // THE FIXTURE CONTROL: the walk really did emit both node types and the edge
    // type, so an empty uncovered set below is coverage rather than an empty
    // walk.
    assert!(emitted_nodes.contains(NODE_TYPE_DIRECTORY));
    assert!(emitted_nodes.contains(NODE_TYPE_FILE));
    assert!(emitted_edges.contains(EDGE_TYPE_CONTAINS));

    let declaration = SampleCollector::declaration();
    assert_eq!(
        declaration
            .vocabulary
            .uncovered_node_types(emitted_nodes.iter().copied()),
        Vec::<String>::new(),
        "every node type the walk emits must be in the declared vocabulary"
    );
    assert_eq!(
        declaration
            .vocabulary
            .uncovered_edge_types(emitted_edges.iter().copied()),
        Vec::<String>::new(),
        "every edge type the walk emits must be in the declared vocabulary"
    );

    // THE KNOWN POSITIVE for the comparison itself, in the same run: a type the
    // declaration does not name IS reported.
    assert_eq!(
        declaration.vocabulary.uncovered_node_types(["symlink"]),
        vec!["symlink".to_string()],
        "control: an undeclared type must be reported, or the assertion above means nothing"
    );

    let _ = fs::remove_dir_all(&root);
}

// ---------------------------------------------------------------------------
// R3.3, crate-side half — the walk is BYTE-IDEMPOTENT over an unchanged source.
//
// The end-to-end half compares the read-back graph across two collects; this is
// the half that shows the collector itself is deterministic, which is what makes
// a difference downstream attributable to the client rather than to the walk.
// A count is not the assertion: the encoded bytes are.
// ---------------------------------------------------------------------------
#[test]
fn two_walks_of_an_unchanged_tree_encode_to_identical_bytes() {
    let root = fixture_tree("idem");

    let first = encode_result("collect", walk(&root, 0)).expect("the first walk encodes");
    let second = encode_result("collect", walk(&root, 0)).expect("the second walk encodes");
    let first = serde_json::to_vec(&first).expect("the envelope serializes");
    let second = serde_json::to_vec(&second).expect("the envelope serializes");
    assert_eq!(
        first, second,
        "two walks of an unchanged tree must encode to identical BYTES, not merely to equal counts"
    );

    // THE CONTROL: a CHANGED tree encodes differently, so the equality above is
    // determinism rather than a comparison that cannot fail.
    fs::write(root.join("new.txt"), b"new").expect("a fixture file is written");
    let third = encode_result("collect", walk(&root, 0)).expect("the third walk encodes");
    let third = serde_json::to_vec(&third).expect("the envelope serializes");
    assert_ne!(first, third);

    let _ = fs::remove_dir_all(&root);
}

// ---------------------------------------------------------------------------
// R3.4, crate-side half (a) — a truncated walk asserts INCOMPLETE with the
// reason, never COMPLETE with fewer nodes.
//
// That is what `walk_complete` decides downstream: an incomplete collect
// disables the server's deletion phase, so a truncated walk asserting COMPLETE
// would let the server treat every file it did not reach as deleted.
// ---------------------------------------------------------------------------
#[test]
fn a_truncated_walk_asserts_incomplete_with_its_reason() {
    let root = fixture_tree("depth");

    let whole = walk(&root, 0);
    assert_eq!(whole.complete, Completeness::Complete);

    let truncated = walk(&root, 1);
    match &truncated.complete {
        Completeness::Incomplete { reason } => {
            assert!(reason.contains("max_depth"), "{reason}");
            assert!(reason.contains("did not descend"), "{reason}");
        }
        other => panic!("a truncated walk must assert INCOMPLETE, not {other}"),
    }

    // THE CONTROL that the truncation really happened: the truncated walk found
    // fewer nodes than the whole one.
    assert!(
        truncated.nodes.len() < whole.nodes.len(),
        "truncated {} vs whole {}",
        truncated.nodes.len(),
        whole.nodes.len()
    );

    // ...and it reaches the wire as walk_complete false.
    let encoded = encode_result("collect", truncated).expect("the truncated walk encodes");
    let raw = serde_json::to_string(&encoded).expect("the envelope serializes");
    assert!(raw.contains(r#""walk_complete":false"#), "{raw}");

    let _ = fs::remove_dir_all(&root);
}

// ---------------------------------------------------------------------------
// R3.4, crate-side half (b) — bad input to the walk ERRORS rather than returning
// an empty successful graph. An empty complete collect would assert a successful
// walk that found nothing, which is a different and destructive claim.
// ---------------------------------------------------------------------------
#[test]
fn a_root_that_names_no_directory_is_an_error_not_an_empty_graph() {
    let server = CollectorServer::new(SampleCollector).expect("the sample serves");

    let err = server
        .handle_collect(&json!({"id": "probe", "params": {"root": ""}}))
        .expect_err("a blank root must be refused");
    // The BLANK arm's own message, not the not-a-directory arm's: a blank root
    // names no source at all, and the two refusals tell an operator different
    // things about what to fix.
    assert!(
        err.contains("params.root is empty"),
        "the blank-root arm must refuse with its own message: {err}"
    );
    assert!(
        err.contains("names the directory to walk"),
        "and must say what the parameter is for: {err}"
    );

    let missing = std::env::temp_dir().join("kn-framework-rust-does-not-exist");
    let err = server
        .handle_collect(&json!({"id": "probe", "params": {"root": missing.to_string_lossy()}}))
        .expect_err("a root that is not a directory must be refused");
    assert!(err.contains("not a directory"), "{err}");
}

// ---------------------------------------------------------------------------
// R3.5 — the sample collector is CREDENTIAL-FREE: it declares no environment
// variable and reads none.
//
// The assertion on the source is what makes it more than a declaration: a
// declaration that said "no env" while the walk read one would be a false
// promise, and the declaration alone cannot show that.
// ---------------------------------------------------------------------------
#[test]
fn the_sample_collector_is_credential_free() {
    let declaration = SampleCollector::declaration();
    assert!(
        declaration.env.is_empty(),
        "the sample collector declares no environment variable"
    );

    let source = fs::read_to_string(
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("src/sample.rs"),
    )
    .expect("the sample's source opens");
    for reader in ["std::env::var", "env::var", "var_os", "std::env::vars"] {
        assert!(
            !source.contains(reader),
            "the sample collector must read no environment variable; found {reader}"
        );
    }

    // THE KNOWN POSITIVE for the scan: the reader IS found where it is used.
    let conformance = fs::read_to_string(
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("src/conformance.rs"),
    )
    .expect("the conformance stub's source opens");
    assert!(
        conformance.contains("std::env::var"),
        "control: the scan finds an environment read where one exists"
    );
}

// ---------------------------------------------------------------------------
// The walk's own shape: a directory node per directory, a file node per file, a
// contains edge per entry, and every node carrying the id the edges reference.
// ---------------------------------------------------------------------------
#[test]
fn the_walk_emits_a_connected_directory_graph() {
    let root = fixture_tree("shape");
    let result = walk(&root, 0);

    let directories = result
        .nodes
        .iter()
        .filter(|n| n.node_type == NODE_TYPE_DIRECTORY)
        .count();
    let files = result
        .nodes
        .iter()
        .filter(|n| n.node_type == NODE_TYPE_FILE)
        .count();
    assert_eq!(directories, 3, "root, a, a/b");
    assert_eq!(files, 3, "top.txt, a/mid.txt, a/b/deep.txt");
    assert_eq!(result.edges.len(), 5, "every entry but the root has a parent");

    let ids: BTreeSet<&str> = result.nodes.iter().map(|n| n.id.as_str()).collect();
    for edge in &result.edges {
        assert!(ids.contains(edge.from_id.as_str()), "{}", edge.from_id);
        assert!(ids.contains(edge.to_id.as_str()), "{}", edge.to_id);
    }

    // THE ENTRIES OF EACH DIRECTORY ARE EMITTED IN SORTED PATH ORDER. That is
    // what the walk's own sort produces, and it is the observable: a walk that
    // took the filesystem's order would produce a different result per run on a
    // filesystem that does not promise one, and byte-idempotence would be luck.
    let root_children: Vec<&str> = result
        .edges
        .iter()
        .filter(|e| e.from_id == root.to_string_lossy())
        .map(|e| e.to_id.as_str())
        .collect();
    let mut sorted = root_children.clone();
    sorted.sort_unstable();
    assert_eq!(
        root_children, sorted,
        "a directory's entries are emitted in sorted path order"
    );

    let _ = fs::remove_dir_all(&root);
}

// ---------------------------------------------------------------------------
// The producer half of the ingest-refusal row: the sample CAN emit a node type
// its declaration omits, on demand, so the client's refusal has something to
// refuse. Without this arm that refusal has no producer and could only be
// described.
// ---------------------------------------------------------------------------
#[test]
fn the_sample_can_emit_a_type_its_declaration_omits() {
    use knowledge_collector_framework::sample::UNDECLARED_NODE_TYPE;

    let root = fixture_tree("undeclared");
    let result = SampleCollector
        .walk(
            "probe",
            SampleParams {
                root: root.to_string_lossy().to_string(),
                max_depth: 0,
                demo_panic: false,
                demo_undeclared_node_type: true,
            },
            &ForeignContext::default(),
        )
        .expect("the walk succeeds");

    let emitted: BTreeSet<&str> = result.nodes.iter().map(|n| n.node_type.as_str()).collect();
    assert!(
        emitted.contains(UNDECLARED_NODE_TYPE),
        "the switch must actually emit the undeclared type: {emitted:?}"
    );
    assert_eq!(
        SampleCollector::declaration()
            .vocabulary
            .uncovered_node_types(emitted.iter().copied()),
        vec![UNDECLARED_NODE_TYPE.to_string()],
        "and the declaration must NOT name it, or there is nothing for the client to refuse"
    );

    // THE CONTROL: with the switch off the same walk is fully covered, so the
    // row above is about the switch rather than about a declaration that never
    // covered anything.
    let clean = walk(&root, 0);
    let clean_types: BTreeSet<&str> = clean.nodes.iter().map(|n| n.node_type.as_str()).collect();
    assert_eq!(
        SampleCollector::declaration()
            .vocabulary
            .uncovered_node_types(clean_types.iter().copied()),
        Vec::<String>::new()
    );

    let _ = fs::remove_dir_all(&root);
}

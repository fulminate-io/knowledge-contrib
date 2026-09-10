// SPDX-License-Identifier: Apache-2.0

//! contract_pin.rs — the BYTE-PARITY PIN between this crate's embedded contract
//! copies and the framework module's, and the SPDX header on every source file.
//!
//! WHY THE SIBLING AND NOT THE CLIENT. The Go framework pins its copy with a
//! relative symlink into the client's package directory. That shape does not
//! transfer: the publish staging is written for that one link and would copy a
//! second one verbatim and publish it DANGLING. `../framework/contract/` is the
//! path that resolves in BOTH layouts — `cmd/collectors/framework-rust/../framework/contract/`
//! in this repository, and `framework-rust/../framework/contract/` in the
//! published tree, since the staging copies every top-level entry.
//!
//! IT IS A TWO-HOP PIN AND BOTH HOPS ALREADY RUN. The framework module's own
//! suite pins its copy to the client's files, and the publish staging compares
//! them again and aborts naming the file when they have drifted. This crate pins
//! itself to the framework module, which closes the chain.
//!
//! THE SENTINEL IS NOT OPTIONAL. A test that SKIPPED when the sibling directory
//! is absent would make every comparison below compare this crate's copy against
//! itself. An absent sibling is a RED.

use std::path::PathBuf;

fn crate_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
}

fn sibling_contract_dir() -> PathBuf {
    crate_root().join("..").join("framework").join("contract")
}

/// THE CONTRACT FILES this crate ships a copy of. Read from this crate's own
/// directory listing rather than typed, so a third file added by a sibling
/// ticket is a red in `tests/pending_pins.rs` rather than a silent omission here.
fn embedded_files() -> Vec<String> {
    let mut names: Vec<String> = std::fs::read_dir(crate_root().join("contract"))
        .expect("this crate's contract directory opens")
        .flatten()
        .map(|e| e.file_name().to_string_lossy().to_string())
        .filter(|n| n.ends_with(".json"))
        .collect();
    names.sort();
    names
}

// ---------------------------------------------------------------------------
// THE SENTINEL — the sibling directory exists and holds at least what this crate
// embeds. It fails LOUD rather than skipping, because a skipped comparison is a
// green that proves nothing.
// ---------------------------------------------------------------------------
#[test]
fn the_sibling_contract_directory_resolves() {
    let dir = sibling_contract_dir();
    assert!(
        dir.is_dir(),
        "the sibling contract directory {} does not resolve; every byte comparison below \
         would compare this crate's copy against itself, so this is a red and never a skip",
        dir.display()
    );

    let mut sibling: Vec<String> = std::fs::read_dir(&dir)
        .expect("the sibling contract directory opens")
        .flatten()
        .map(|e| e.file_name().to_string_lossy().to_string())
        .filter(|n| n.ends_with(".json"))
        .collect();
    sibling.sort();

    let embedded = embedded_files();
    assert!(
        !embedded.is_empty(),
        "this crate embeds at least one contract document"
    );
    for name in &embedded {
        assert!(
            sibling.contains(name),
            "this crate embeds {name} and the sibling directory {} does not hold it",
            dir.display()
        );
    }
}

// ---------------------------------------------------------------------------
// R1.2's other half — every embedded copy is byte-identical to the sibling's.
// ---------------------------------------------------------------------------
#[test]
fn every_embedded_contract_copy_matches_the_framework_modules() {
    let dir = sibling_contract_dir();
    for name in embedded_files() {
        let ours = std::fs::read(crate_root().join("contract").join(&name))
            .unwrap_or_else(|e| panic!("reading this crate's {name}: {e}"));
        let theirs = std::fs::read(dir.join(&name))
            .unwrap_or_else(|e| panic!("reading the sibling's {name}: {e}"));
        // THE FAILURE MESSAGE IS THE POINT OF THIS BLOCK. Comparing two byte
        // vectors with assert_eq! prints both of them as decimal arrays —
        // thousands of numbers — after a message that already named the file and
        // both sizes. What a reader needs is where they diverge, so the
        // comparison is on the DECODED text and the report is one offset with a
        // window of context.
        if let Some(report) = drift_report(&name, &ours, &theirs) {
            panic!("{report}");
        }
    }
}

/// drift_report compares two copies and, when they differ, renders WHERE.
///
/// IT IS A FUNCTION SO IT CAN BE EXERCISED. Comparing two byte vectors with
/// `assert_eq!` prints both as decimal arrays — thousands of numbers — after a
/// message that already named the file and both sizes, which is noise in the one
/// log a reader reads. A first-difference offset with a window of context is
/// what a reader can act on, and a renderer nothing ever calls on a real
/// difference is a renderer nobody has seen work.
fn drift_report(name: &str, ours: &[u8], theirs: &[u8]) -> Option<String> {
    if ours == theirs {
        return None;
    }
    let at = ours
        .iter()
        .zip(theirs.iter())
        .position(|(a, b)| a != b)
        .unwrap_or_else(|| ours.len().min(theirs.len()));
    let window = |bytes: &[u8]| {
        let start = at.saturating_sub(40);
        let end = (at + 80).min(bytes.len());
        String::from_utf8_lossy(&bytes[start..end]).to_string()
    };
    Some(format!(
        "this crate's contract/{name} has drifted from ../framework/contract/{name}\n\
         sizes: {} here, {} there\n\
         first difference at byte {at}\n\
         here : ...{}...\n\
         there: ...{}...\n\
         refresh the copy from the sibling module; the sibling is the source.",
        ours.len(),
        theirs.len(),
        window(ours),
        window(theirs),
    ))
}

// ---------------------------------------------------------------------------
// The drift report, exercised on a synthetic pair. The comparison above is
// silent on a healthy tree by construction, so this is what shows the renderer
// works at all — and what keeps it from regressing to a byte dump.
// ---------------------------------------------------------------------------
#[test]
fn the_drift_report_names_where_two_copies_diverge() {
    let ours = b"{\n  \"a\": 1,\n  \"b\": 2\n}\n";
    let theirs = b"{\n  \"a\": 1,\n  \"b\": 3\n}\n";
    let report = drift_report("probe.json", ours, theirs).expect("two different copies drift");

    assert!(report.contains("probe.json"), "{report}");
    assert!(report.contains("first difference at byte 19"), "{report}");
    assert!(report.contains("sizes: 23 here, 23 there"), "{report}");
    // It renders TEXT, not a byte array: the whole point of the change.
    assert!(
        !report.contains("123, 10"),
        "the report must not dump decimal bytes: {report}"
    );
    assert!(report.contains("\"b\": 2"), "{report}");
    assert!(report.contains("\"b\": 3"), "{report}");

    // THE CONTROL: identical copies produce no report at all.
    assert!(drift_report("probe.json", ours, ours).is_none());
}

// ---------------------------------------------------------------------------
// The embedded bytes the crate ADVERTISES are the bytes on disk, so the two
// assertions above reach what the wire carries rather than a second copy.
// ---------------------------------------------------------------------------
#[test]
fn the_embedded_bytes_are_the_files_on_disk() {
    use knowledge_collector_framework::schema::{input_contract_json, output_contract_json};

    let input = std::fs::read(crate_root().join("contract/collector_input.schema.json"))
        .expect("the input contract opens");
    let output = std::fs::read(crate_root().join("contract/collector_output.schema.json"))
        .expect("the output contract opens");
    assert_eq!(input_contract_json(), input);
    assert_eq!(output_contract_json(), output);
}

// ---------------------------------------------------------------------------
// CHECK B — every Rust source file carries the SPDX header as its FIRST line.
//
// IT IS A CRATE-SIDE TEST RATHER THAN A CORPUS CHECK, and the reason is the
// tool's vocabulary rather than a preference: the corpus checks that read source
// shape are AST patterns, the pattern language matches no comment node, so an
// AST check for a first-line comment would compile, scan and report a permanent
// green. That holds for every language, not only Rust.
//
// The fixture pair this row was admitted on is stated rather than implied: the
// BAD example is a file whose first line is `use serde_json::Value;`, and the
// NEAR-MISS GOOD example is the same file with the SPDX line above it and a
// blank line after. The pair varies only the header, which is the property.
// ---------------------------------------------------------------------------
#[test]
fn every_rust_source_file_carries_the_spdx_header() {
    const HEADER: &str = "// SPDX-License-Identifier: Apache-2.0";

    fn rust_files(dir: &std::path::Path, out: &mut Vec<PathBuf>) {
        let Ok(entries) = std::fs::read_dir(dir) else {
            return;
        };
        for entry in entries.flatten() {
            let path = entry.path();
            if path.is_dir() {
                rust_files(&path, out);
            } else if path.extension().is_some_and(|e| e == "rs") {
                out.push(path);
            }
        }
    }

    let mut files = Vec::new();
    rust_files(&crate_root().join("src"), &mut files);
    rust_files(&crate_root().join("tests"), &mut files);
    files.sort();

    assert!(
        files.len() >= 10,
        "the scan found only {} Rust files, which is fewer than this crate has; \
         a green over an empty scan is not a green",
        files.len()
    );

    // THE KNOWN POSITIVE, in the same run and through the same reader: the check
    // below fires on a file whose first line is not the header.
    let bad = "use serde_json::Value;\n\nfn main() {}\n";
    assert_ne!(
        bad.lines().next(),
        Some(HEADER),
        "control: the bad fixture must NOT satisfy the check this test runs"
    );
    let good = format!("{HEADER}\n\nuse serde_json::Value;\n\nfn main() {{}}\n");
    assert_eq!(
        good.lines().next(),
        Some(HEADER),
        "control: the near-miss good fixture must satisfy it"
    );

    for path in files {
        let text = std::fs::read_to_string(&path)
            .unwrap_or_else(|e| panic!("reading {}: {e}", path.display()));
        assert_eq!(
            text.lines().next(),
            Some(HEADER),
            "{} does not open with the SPDX header",
            path.display()
        );
    }
}

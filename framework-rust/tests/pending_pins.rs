// SPDX-License-Identifier: Apache-2.0

//! pending_pins.rs — the EIGHT PINS on work that lands in sibling changes.
//!
//! THE SHAPE, AND WHY IT IS NOT A STANDING RED. Each pin has two arms. While its
//! owning artifact is ABSENT it SKIPS BY NAME, printing the pin, the ticket that
//! closes it and what the assertion will be — never silently, and never as a
//! pass that claims anything. The moment the artifact is PRESENT the pin
//! ASSERTS, and a mismatch fails by name.
//!
//! A standing red would be the wrong shape here even though the obligation is
//! real: `cargo test` is this crate's declared test command, so a permanent
//! non-zero exit makes the publish census's staged test leg red for every lane
//! and the CI leg red until three sibling tickets land. A red that is expected
//! everywhere is a red nobody reads.
//!
//! THE SKIP MESSAGES ARE VISIBLE, which is what keeps this from being a silent
//! pass: the crate's declared test command and its CI leg both run the harness
//! with `--nocapture`, so every skip reaches the log of every run that matters.
//!
//! EVERY PIN OBSERVES A LANDED ARTIFACT — a file in the contract directory, a
//! constant in the framework module, a row in the port contract, a target in the
//! Makefile — rather than comparing this crate against a sentence in a plan.
//!
//! THE PUBLISHED LAYOUT IS AN ARM, NOT A SKIP. Several pins read the knowledge
//! client's own tree, which the publish staging deliberately does not carry. Each
//! such pin asserts one of TWO things: the pin itself in the source layout, or
//! the ABSENCE of the client tree in the published one, which is a positive
//! assertion about the layout.

use std::collections::BTreeSet;
use std::path::{Path, PathBuf};

fn crate_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
}

/// framework_dir is the sibling Go framework module. It resolves in BOTH
/// layouts, which is why the contract pin is anchored on it.
fn framework_dir() -> PathBuf {
    crate_root().join("..").join("framework")
}

/// client_dir is the knowledge client's own package directory. It resolves ONLY
/// in this repository.
fn client_dir() -> PathBuf {
    crate_root()
        .join("..")
        .join("..")
        .join("knowledge")
        .join("internal")
        .join("externalcollector")
}

fn repo_root() -> PathBuf {
    crate_root().join("..").join("..").join("..")
}

enum Layout {
    Source,
    Published,
}

/// layout decides which tree this suite is running in, and ASSERTS the shape of
/// whichever it found rather than inferring one from a missing file.
fn layout() -> Layout {
    let framework = framework_dir();
    assert!(
        framework.join("contract").is_dir(),
        "the sibling framework module must resolve in either layout: {} is not a directory",
        framework.display()
    );
    if client_dir().join("contract").is_dir() {
        return Layout::Source;
    }
    assert!(
        !crate_root().join("..").join("..").join("knowledge").exists(),
        "the client tree is neither fully present nor absent; this is a half-staged tree, \
         which is a red rather than a layout"
    );
    Layout::Published
}

/// pending announces a pin whose owning artifact has not landed. It prints
/// rather than failing, and the printed line is what a reader of the run's log
/// sees; see the module doc for why the runs that matter pass `--nocapture`.
fn pending(pin: &str, ticket: &str, waiting_for: &str, assertion: &str) {
    // STDERR, not stdout, and the crate's own corpus check is what says so: in
    // this crate stdout is the protocol stream, and a diagnostic goes to stderr
    // wherever it is written. `--nocapture` surfaces both streams, so nothing is
    // lost by obeying the rule here.
    eprintln!(
        "SKIPPED {pin} — PENDING {ticket}. Waiting for: {waiting_for}. \
         When it lands this test ASSERTS: {assertion}"
    );
}

fn read(path: &Path) -> String {
    std::fs::read_to_string(path).unwrap_or_else(|e| panic!("reading {}: {e}", path.display()))
}

fn json_files(dir: &Path) -> BTreeSet<String> {
    std::fs::read_dir(dir)
        .unwrap_or_else(|e| panic!("reading {}: {e}", dir.display()))
        .flatten()
        .map(|e| e.file_name().to_string_lossy().to_string())
        .filter(|n| n.ends_with(".json"))
        .collect()
}

/// tsv_rows parses the key-tab-value shape the port contract and the manifest
/// share: `#` comments and blank lines ignored, one pair per line.
fn tsv_rows(text: &str) -> Vec<(String, String)> {
    text.lines()
        .map(str::trim_end)
        .filter(|l| !l.trim().is_empty() && !l.trim_start().starts_with('#'))
        .filter_map(|l| l.split_once('\t'))
        .map(|(k, v)| (k.trim().to_string(), v.trim().to_string()))
        .collect()
}

/// go_const_value reads a Go string constant's VALUE out of source text, so a
/// pin compares against what the framework declares rather than against a
/// literal typed here.
///
/// IT ADMITS BOTH DECLARATION FORMS, and the omission of the second is a defect
/// this reader already shipped: it required the trimmed line to BEGIN with the
/// constant's name, which is true only inside a grouped `const ( ... )` block.
/// A single-line `const Name = "value"` begins with `const `, so the reader
/// answered None for every constant in such a file — including the one its own
/// pin used to convince itself it was looking at the right place, which is why
/// that guard now PARSES rather than substring-matches.
///
/// The third form, `const Name string = "value"`, is admitted while we are here:
/// it is the same declaration with an explicit type, and a reader that refused
/// it would have the same blind spot one rename away.
fn go_const_value(source: &str, name: &str) -> Option<String> {
    for line in source.lines() {
        let line = line.trim();
        // An optional leading `const `, for the single-line form.
        let line = line.strip_prefix("const ").unwrap_or(line).trim_start();
        let Some(rest) = line.strip_prefix(name) else {
            continue;
        };
        // THE NAME MUST END AT A TOKEN BOUNDARY. Without this, stripping the
        // prefix from `DescribeToolNameSuffix = "other"` leaves `Suffix =
        // "other"`, whose first token reads as a type and whose value reads as
        // the answer — so a longer constant would silently answer for a shorter
        // one. This crate's own control caught that.
        if !rest.starts_with(|c: char| c.is_whitespace() || c == '=') {
            continue;
        }
        let rest = rest.trim_start();
        let rest = match rest.strip_prefix('=') {
            Some(rest) => rest,
            None => {
                // `const Name string = "value"` — one type token, then `=`.
                let mut parts = rest.splitn(2, '=');
                let ty = parts.next().unwrap_or("").trim();
                let Some(after) = parts.next() else { continue };
                if ty.is_empty() || ty.contains(char::is_whitespace) {
                    continue;
                }
                after
            }
        };
        let rest = rest.trim();
        if !rest.starts_with('"') {
            continue;
        }
        return rest[1..].split('"').next().map(str::to_string);
    }
    None
}

// ===========================================================================
// PIN 1 — the port contract: the manifest's FILE NAME and its four keys.
//
// OWNED BY: the change that adds the non-Go port class to the three collector
// censuses and the publish sweep. It declares the manifest's name and keys in
// ONE file all four readers parse, so this crate's manifest and those readers
// cannot drift.
//
// THE COMPARISON IS TWO-WAY. A subset check passes a crate that carries extra
// keys the contract does not define and passes a contract that grew a key the
// crate does not carry; only equality reds on a rename in EITHER direction,
// which is the whole point of pinning against a declaration rather than a memory.
// ===========================================================================
#[test]
fn pin_1_this_crates_manifest_matches_the_declared_port_contract() {
    let Layout::Source = layout() else {
        // The contract file is repository machinery and is not published. That
        // the SHIPPED manifest parses as declared is asserted in
        // tests/packaging.rs, which runs in both layouts.
        return;
    };

    let contract_path = repo_root()
        .join("scripts")
        .join("testdata")
        .join("collector-port-contract.tsv");
    if !contract_path.is_file() {
        pending(
            "pin 1",
            "the port-class census change",
            &format!("{} — the one declaration of what a port directory's manifest is called and which keys it carries", contract_path.display()),
            "the manifest's file name equals the contract's `manifest` row, and this crate's key set EQUALS the contract's `manifest-key` set, both directions",
        );
        return;
    }

    let contract = tsv_rows(&read(&contract_path));
    let declared_name = contract
        .iter()
        .find(|(k, _)| k == "manifest")
        .map(|(_, v)| v.clone())
        .expect("the port contract declares the manifest's file name");
    let declared_keys: BTreeSet<String> = contract
        .iter()
        .filter(|(k, _)| k == "manifest-key")
        .map(|(_, v)| v.clone())
        .collect();
    assert!(
        !declared_keys.is_empty(),
        "the port contract declares at least one manifest key; a reader that found none is a \
         reader defect rather than an empty contract"
    );

    let manifest = crate_root().join(&declared_name);
    assert!(
        manifest.is_file(),
        "the port contract declares the manifest file {declared_name:?} and this crate does not \
         carry it; rename this crate's manifest to match"
    );

    let carried: BTreeSet<String> = tsv_rows(&read(&manifest))
        .into_iter()
        .map(|(k, _)| k)
        .collect();
    assert_eq!(
        carried, declared_keys,
        "this crate's {declared_name} declares the key set {carried:?} and the port contract \
         declares {declared_keys:?}; they must be EQUAL, so a key added or renamed on either \
         side reds here"
    );

    // ...and the crate declares the language the contract lists a source
    // extension for, which is what makes the two documents about the same port.
    let extensions: BTreeSet<String> = contract
        .iter()
        .filter(|(k, _)| k == "source-extension")
        .map(|(_, v)| v.clone())
        .collect();
    assert!(
        extensions.contains(".rs"),
        "the port contract lists the source extensions its isolation arm parses and .rs is not \
         among them: {extensions:?}"
    );
}

// ===========================================================================
// PIN 2 — the describe tool's OUTPUT SCHEMA FILE and its name.
//
// OWNED BY: the change that adds the describe tool to the client and to the Go
// framework. It adds a third document to the contract directory; this crate
// embeds a byte-identical copy of whatever it turns out to be.
// ===========================================================================
#[test]
fn pin_2_this_crate_embeds_every_contract_document() {
    let theirs = json_files(&framework_dir().join("contract"));
    let ours = json_files(&crate_root().join("contract"));

    if theirs.len() < 3 {
        pending(
            "pin 2",
            "the describe-tool change",
            &format!("a third document in the framework module's contract directory, which today holds {:?}", theirs),
            "this crate's contract directory holds exactly the same document set as the framework module's",
        );
        // The two-document state is still asserted, so a document going MISSING
        // from either side is a red today rather than a quieter skip.
        assert_eq!(
            ours, theirs,
            "this crate must embed exactly the documents the framework module carries"
        );
        return;
    }

    assert_eq!(
        ours, theirs,
        "this crate must embed EVERY contract document, not only the two it started with"
    );
}

// ===========================================================================
// PIN 3 — the stub MODE VOCABULARY.
//
// OWNED BY: the describe-tool change, which adds modes for a provider that
// serves describe, one that does not, and one that serves a non-conforming
// declaration.
//
// IT IS GREEN TODAY and turns RED the moment the sibling adds a mode this crate
// does not emulate, which is the shape a pin should have wherever it can. It is
// therefore the one pin with no skip arm in the source layout.
// ===========================================================================
#[test]
fn pin_3_this_stub_emulates_every_mode_the_clients_stub_declares() {
    use knowledge_collector_framework::conformance::STUB_MODES;

    let Layout::Source = layout() else {
        return;
    };

    let stub = read(&client_dir().join("stubprovider_test.go"));
    // The client's stub names its modes `stubMode<Name> = "<wire-name>"`. The
    // WIRE NAMES are what this crate has to emulate, so they are what is read —
    // never the Go identifiers, which are that file's private business.
    let mut declared: BTreeSet<String> = BTreeSet::new();
    for line in stub.lines() {
        let line = line.trim();
        let Some(rest) = line.strip_prefix("stubMode") else {
            continue;
        };
        let Some((_, value)) = rest.split_once('=') else {
            continue;
        };
        let value = value.trim();
        if !value.starts_with('"') {
            continue;
        }
        let Some(name) = value[1..].split('"').next() else {
            continue;
        };
        // stubModeEnv names the environment VARIABLE that carries the mode, not
        // a mode. It is excluded by its value's shape: a variable name, not a
        // wire mode.
        if name.contains('_') && name.to_uppercase() == name {
            continue;
        }
        declared.insert(name.to_string());
    }
    assert!(
        !declared.is_empty(),
        "the reader found no mode constants in the client's stub; that is a reader defect, not \
         an empty vocabulary"
    );

    let ours: BTreeSet<String> = STUB_MODES.iter().map(|m| m.as_str().to_string()).collect();
    assert_eq!(
        ours, declared,
        "this crate's conformance stub emulates {} modes and the client's stub declares {}. \
         Every mode the client's dialing tests can ask for must be emulated, or those tests skip \
         by name and this port has not proven the contract half of its ticket.",
        ours.len(),
        declared.len()
    );
}

// ===========================================================================
// PIN 4 — whether the describe declaration is RE-VERIFIED ON EVERY COLLECT or
// only at registration.
//
// OWNED BY: the describe-tool change, whose implementer decides it and states it
// in a doc comment. It decides whether every mode of this crate's conformance
// stub that reaches a successful dial must ALSO serve describe.
// ===========================================================================
#[test]
fn pin_4_the_client_states_when_describe_is_verified() {
    let Layout::Source = layout() else {
        return;
    };
    let collector = read(&client_dir().join("collector.go"));
    if !collector.to_lowercase().contains("describe") {
        pending(
            "pin 4",
            "the describe-tool change",
            "the client's collector module saying whether the declaration is re-verified at every collect or only at registration",
            "this crate's conformance stub serves describe in every mode that reaches a successful dial, if the client re-verifies at collect",
        );
        return;
    }

    // It landed: the crate's serving layer must serve the describe tool, or a
    // re-verifying client refuses every collect this crate's collectors serve.
    use knowledge_collector_framework::sample::SampleCollector;
    use knowledge_collector_framework::serve::CollectorServer;
    let server = CollectorServer::new(SampleCollector).expect("the sample collector serves");
    assert!(
        server.served_tools().len() >= 2,
        "the client now speaks about describe and this crate still serves only {} tool(s); a \
         collector that does not serve describe is refused by a client that verifies it",
        server.served_tools().len()
    );
}

// ===========================================================================
// PIN 5 — the HARNESS SEAM: the runner a port's CI leg calls, and the
// environment variable it sets to point the client's own dialing tests at an
// external collector command.
//
// OWNED BY: the change that adds the seam to the contract harness.
// ===========================================================================
#[test]
fn pin_5_the_contract_harness_exposes_an_external_collector_runner() {
    let Layout::Source = layout() else {
        return;
    };
    let makefile_path = repo_root().join("Makefile");
    assert!(makefile_path.is_file(), "the repository Makefile resolves");
    let makefile = read(&makefile_path);
    if !makefile.contains("test-collector-contract") {
        pending(
            "pin 5",
            "the harness-seam change",
            "a `test-collector-contract` target in the repository Makefile, which is what this crate's CI leg calls to run the client's eleven provider-dialing tests against this crate's conformance stub",
            "the target exists AND the seam's environment variable reaches the harness that builds the stdio registration",
        );
        return;
    }

    let seam = read(&client_dir().join("stubprovider_test.go"));
    let external = std::fs::read_to_string(client_dir().join("externalseam_test.go"))
        .unwrap_or_default();
    assert!(
        seam.contains("EXTERNAL_COLLECTOR") || external.contains("EXTERNAL_COLLECTOR"),
        "the runner exists but the seam's environment variable does not reach the harness that \
         builds the stdio registration, so the runner has nothing to switch on"
    );

    // AND THE CI LEG CALLS IT. A seam nothing invokes proves nothing.
    let workflow = read(&repo_root().join(".github").join("workflows").join("ci.yml"));
    assert!(
        workflow.contains("test-collector-contract"),
        "this crate's CI leg must invoke the contract runner, or the eleven never run against \
         this port on any venue but a developer's machine"
    );
}

// ===========================================================================
// PIN 6 — the describe TOOL NAME constant.
//
// OWNED BY: the describe-tool change. The name is FIXED rather than declared,
// because the client must know a tool's name before it can call any tool.
// ===========================================================================
#[test]
fn pin_6_the_framework_declares_the_describe_tool_name() {
    let framework = read(&framework_dir().join("framework.go"));
    // THE GUARD PARSES RATHER THAN SUBSTRING-MATCHES. A `contains` check passes
    // on the doc comment above a declaration whatever the parse does, so a
    // reader blind to this file's declaration form would report "looking at the
    // right file" and then skip forever. This asserts the reader can actually
    // read a constant out of it, using the one whose value is already known.
    assert_eq!(
        go_const_value(&framework, "DefaultToolName").as_deref(),
        Some("collect"),
        "the reader cannot parse the framework's own DefaultToolName out of {}, so it cannot \
         be trusted to answer for any other constant in it",
        framework_dir().join("framework.go").display()
    );
    let Some(declared) = go_const_value(&framework, "DescribeToolName") else {
        pending(
            "pin 6",
            "the describe-tool change",
            "a DescribeToolName constant in the Go framework, beside DefaultToolName",
            "this crate serves a tool by exactly that name; a guessed constant here would be a literal typed twice",
        );
        return;
    };

    use knowledge_collector_framework::sample::SampleCollector;
    use knowledge_collector_framework::serve::CollectorServer;
    let server = CollectorServer::new(SampleCollector).expect("the sample collector serves");
    let served: Vec<String> = server
        .served_tools()
        .iter()
        .map(|t| t.name.to_string())
        .collect();
    assert!(
        served.iter().any(|n| n == &declared),
        "the framework declares the describe tool {declared:?} and this crate serves {served:?}"
    );
}

// ===========================================================================
// PIN 7 — the family-level selector's FIELD NAME.
//
// OWNED BY: the describe-tool change. This crate renders `all_node_types`, which
// is the settled spelling; the pin is what catches a rename.
// ===========================================================================
#[test]
fn pin_7_the_family_level_selector_spelling_matches_the_framework() {
    let dir = framework_dir();
    let mut sources = Vec::new();
    for entry in std::fs::read_dir(&dir)
        .unwrap_or_else(|e| panic!("reading {}: {e}", dir.display()))
        .flatten()
    {
        let path = entry.path();
        if path.extension().is_some_and(|e| e == "go")
            && !path.to_string_lossy().ends_with("_test.go")
        {
            sources.push(read(&path));
        }
    }
    let all = sources.join("\n");
    assert!(
        all.contains("ForeignContext"),
        "the reader is looking at the right module"
    );

    // The framework renders the selector as a json tag on its declaration type;
    // read the tag rather than a Go identifier, because the tag is what reaches
    // the entry the client writes.
    let declared = all
        .split("json:\"")
        .skip(1)
        .filter_map(|rest| rest.split('"').next())
        .map(|tag| tag.split(',').next().unwrap_or("").to_string())
        .find(|tag| tag.contains("node_types") && tag.starts_with("all"));

    let Some(declared) = declared else {
        pending(
            "pin 7",
            "the describe-tool change",
            "a family-level selector field on the Go framework's declaration type",
            "this crate renders the selector under exactly the framework's own key; it renders `all_node_types` today",
        );
        return;
    };

    use knowledge_collector_framework::describe::{Declaration, ForeignFamilyDeclaration, Vocabulary};
    let rendered = Declaration {
        vocabulary: Vocabulary::new(vec!["issue".into()], vec!["blocks".into()]),
        foreign_context: vec![ForeignFamilyDeclaration::all("code")],
        ..Declaration::default()
    }
    .render()
    .expect("the declaration renders");
    // THE FROZEN SHAPE keys the foreign-context declaration by FAMILY NAME under
    // `context`, in the same shape the registration entry carries, rather than
    // as an array. The pin reads it that way because the contract's own describe
    // schema says so.
    let family = rendered["context"]["code"]
        .as_object()
        .expect("a family declaration is an object keyed by family name under `context`");
    assert!(
        family.contains_key(&declared),
        "the framework renders the family-level selector as {declared:?} and this crate renders \
         {:?}",
        family.keys().collect::<Vec<_>>()
    );
}

// ===========================================================================
// PIN 8 — the framework README this crate's README mirrors.
//
// OWNED BY: the change that writes the Go framework's README. This crate's
// README derives its section structure from that file rather than from a
// hand-enumeration.
// ===========================================================================
#[test]
fn pin_8_this_readme_mirrors_the_framework_readme() {
    let theirs = framework_dir().join("README.md");
    if !theirs.is_file() {
        pending(
            "pin 8",
            "the framework-README change",
            &format!("{}, the parity target this crate's README structure derives from", theirs.display()),
            "every second-level section of the framework README has a counterpart in this crate's",
        );
        return;
    }

    fn headings(text: &str) -> Vec<String> {
        text.lines()
            .filter_map(|l| l.strip_prefix("## "))
            .map(|h| h.trim().to_lowercase())
            .collect()
    }
    let ours = headings(&read(&crate_root().join("README.md")));
    for heading in headings(&read(&theirs)) {
        assert!(
            ours.iter().any(|h| h == &heading),
            "the framework README carries the section {heading:?} and this crate's README does not"
        );
    }
}

// ===========================================================================
// PIN 9 — the INGEST-SIDE VOCABULARY REFUSAL.
//
// OWNED BY: the describe-tool change, whose R4 adds the product's first
// ingest-side type refusal in the server's bootstrap ingest path: a collect
// carrying a node type the entry's declared vocabulary omits is refused, naming
// the node id and the offending value, and an entry declaring NO vocabulary
// collects with a one-line client notice instead.
//
// WHY IT IS A PIN RATHER THAN A TEST. Nothing in the product refuses a node type
// on any path today, so there is no message to match and no arm to drive. What
// this pin exists to prevent is the deferral being INVISIBLE: every other blocked
// obligation in this crate announces itself by turning red when its sibling
// lands, and without this one the crate that its own ticket calls the first
// collector the vocabulary applies to would ship never having driven it.
//
// THE ASSERTION READS THE LANDED REFUSAL TEXT rather than guessing it, and
// requires the end-to-end script's arm to key on that same text — so the arm
// cannot pass by matching a message the product does not emit, which is the
// query-miss class this crate has already been bitten by once.
// ===========================================================================
#[test]
fn pin_9_the_ingest_vocabulary_refusal_is_driven_end_to_end() {
    let Layout::Source = layout() else {
        return;
    };

    let ingest = repo_root()
        .join("cmd")
        .join("knowledge-server")
        .join("internal")
        .join("bootstrap")
        .join("ingest.go");
    let landed = std::fs::read_to_string(&ingest).unwrap_or_default();

    // The refusal is a per-node type check. Its tell is a message naming the
    // offending value beside the node; read the source for one rather than for
    // the word "type", which appears in every Go file ever written.
    let refusal_line = landed
        .lines()
        .map(str::trim)
        .find(|l| {
            l.contains("CodeInvalidArgument")
                && (l.contains("node type") || l.contains("declared vocabulary"))
        })
        .map(str::to_string);

    let Some(refusal_line) = refusal_line else {
        pending(
            "pin 9",
            "the describe-tool change",
            &format!("an ingest-side vocabulary refusal in {}, which is the product's first node-type refusal on any path", ingest.display()),
            "the end-to-end script drives BOTH arms through the isolated client and server — a collect carrying an undeclared type refused by name, and a no-vocabulary entry collecting with the notice — keying on the landed refusal's own text",
        );
        return;
    };

    // It landed. The end-to-end script must drive it, and must key on a phrase
    // the landed source actually emits.
    let script = read(&repo_root().join("scripts").join("framework-rust-e2e.sh"));
    assert!(
        script.contains("R3.7"),
        "the ingest refusal has landed and the end-to-end script drives no R3.7 arm; the crate \
         declares a vocabulary and must show the refusal it now owes"
    );
    let quoted: Vec<&str> = refusal_line
        .split('"')
        .skip(1)
        .step_by(2)
        .filter(|s| s.len() > 8)
        .collect();
    assert!(
        !quoted.is_empty(),
        "the reader found the refusal site but no quoted message in it; the reader is wrong, \
         not the product: {refusal_line}"
    );
    assert!(
        quoted.iter().any(|phrase| {
            phrase
                .split_whitespace()
                .filter(|w| w.len() > 4)
                .any(|w| script.contains(w))
        }),
        "the end-to-end arm keys on no word of the landed refusal {quoted:?}; an arm matching a \
         message the product does not emit passes on a miss"
    );
}

// ===========================================================================
// THE CONSTANT READER ITSELF, on all three declaration forms.
//
// IT IS A TEST RATHER THAN A HELPER'S DOC because its blindness already shipped:
// the reader required the trimmed line to BEGIN with the constant's name, which
// is true only inside a grouped `const ( ... )` block, so it answered None for
// every constant in a file using the single-line form — and pin 6, whose only
// sanity check was a substring test, skipped forever while reporting that it was
// looking at the right file.
//
// A reader that decides whether a pin fires is a subject, not scaffolding.
// ===========================================================================
#[test]
fn the_go_constant_reader_handles_every_declaration_form() {
    let grouped = "const (\n\t// a doc comment naming DefaultToolName\n\tDefaultToolName = \"collect\"\n)\n";
    let single = "// DefaultToolName is the tool name.\nconst DefaultToolName = \"collect\"\n";
    let typed = "const DefaultToolName string = \"collect\"\n";

    for (form, source) in [("grouped", grouped), ("single-line", single), ("typed", typed)] {
        assert_eq!(
            go_const_value(source, "DefaultToolName").as_deref(),
            Some("collect"),
            "the reader is blind to the {form} declaration form"
        );
    }

    // THE NEGATIVE CONTROLS, in the same run. Without them a reader that
    // answered Some("collect") for anything would pass every row above.
    assert_eq!(
        go_const_value(single, "NoSuchConstant"),
        None,
        "control: a constant that is not there has no value"
    );
    assert_eq!(
        go_const_value("// DefaultToolName is only mentioned in prose here.\n", "DefaultToolName"),
        None,
        "control: a doc comment naming the constant is not a declaration — this is exactly \
         what pin 6's old substring guard could not tell apart"
    );
    assert_eq!(
        go_const_value("const DefaultToolNameSuffix = \"other\"\n", "DefaultToolName"),
        None,
        "control: a longer name that merely starts with the one asked for is not a match"
    );
    assert_eq!(
        go_const_value("const DefaultToolName = collectVar\n", "DefaultToolName"),
        None,
        "control: a non-literal value has no string to read"
    );
}

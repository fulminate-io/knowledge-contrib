// SPDX-License-Identifier: Apache-2.0

//! packaging.rs — the crate's PACKAGING as an observable rather than an
//! assumption.
//!
//! Rows R1.1 and R1.1a. Every one of these reds locally rather than in CI, which
//! is the whole point: a packaging row that only reds in a workflow costs a
//! round trip to find out.

use std::path::{Path, PathBuf};
use std::process::Command;

fn crate_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
}

fn manifest_text() -> String {
    std::fs::read_to_string(crate_root().join("Cargo.toml")).expect("the crate's Cargo.toml opens")
}

/// manifest_key reads a top-level `key = "value"` out of the manifest. It is a
/// deliberate three-line reader rather than a TOML dependency: the crate owes no
/// TOML parser at run time and a test that pulled one in would be asserting a
/// parser's behaviour beside its own.
fn manifest_key(key: &str) -> Option<String> {
    for line in manifest_text().lines() {
        let line = line.trim();
        let Some(rest) = line.strip_prefix(key) else {
            continue;
        };
        let rest = rest.trim_start();
        let Some(rest) = rest.strip_prefix('=') else {
            continue;
        };
        return Some(rest.trim().trim_matches('"').to_string());
    }
    None
}

/// rmcp_msrv reads the MSRV out of the rmcp version this crate LOCKED, from that
/// version's own vendored manifest.
///
/// IT IS READ RATHER THAN TYPED, which is the row's point: hard-coding the floor
/// in the test and again in `Cargo.toml` is two copies of one fact, and the copy
/// in the test is the one nobody updates.
fn rmcp_msrv() -> (String, String) {
    let lock = std::fs::read_to_string(crate_root().join("Cargo.lock"))
        .expect("the crate's Cargo.lock opens");
    let mut version = None;
    let mut lines = lock.lines().peekable();
    while let Some(line) = lines.next() {
        if line.trim() == r#"name = "rmcp""# {
            if let Some(next) = lines.peek() {
                version = next
                    .trim()
                    .strip_prefix("version = ")
                    .map(|v| v.trim_matches('"').to_string());
            }
            break;
        }
    }
    let version = version.expect("Cargo.lock names the rmcp version this crate locked");

    let cargo_home = std::env::var("CARGO_HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|_| {
            PathBuf::from(std::env::var("HOME").expect("HOME is set")).join(".cargo")
        });
    let registry = cargo_home.join("registry").join("src");
    let mut candidates = Vec::new();
    if let Ok(entries) = std::fs::read_dir(&registry) {
        for entry in entries.flatten() {
            let manifest = entry.path().join(format!("rmcp-{version}")).join("Cargo.toml");
            if manifest.is_file() {
                candidates.push(manifest);
            }
        }
    }
    let manifest = candidates.first().unwrap_or_else(|| {
        panic!(
            "rmcp-{version}'s own manifest is not extracted under {}; a build populates it, \
             so this is a red rather than a reason to skip the comparison",
            registry.display()
        )
    });
    let text = std::fs::read_to_string(manifest).expect("rmcp's manifest opens");
    let floor = text
        .lines()
        .find_map(|l| l.trim().strip_prefix("rust-version = "))
        .map(|v| v.trim_matches('"').to_string())
        .expect("rmcp's manifest declares a rust-version");
    (version, floor)
}

// ---------------------------------------------------------------------------
// R1.1 — THE CRATE STANDS ALONE. No workspace above it, and none inside it.
// ---------------------------------------------------------------------------
#[test]
fn the_crate_stands_alone() {
    let text = manifest_text();
    assert!(
        !text.contains("[workspace]"),
        "this crate declares no workspace of its own"
    );
    assert!(
        !text.contains("workspace = true") && !text.contains("workspace.dependencies"),
        "this crate inherits nothing from a workspace: {text}"
    );

    // ...and there is no workspace manifest above it to inherit from. Walk every
    // ancestor to the repository root.
    let mut dir = crate_root();
    while let Some(parent) = dir.parent().map(Path::to_path_buf) {
        if parent.join(".git").exists() {
            break;
        }
        let manifest = parent.join("Cargo.toml");
        assert!(
            !manifest.is_file(),
            "an ancestor manifest at {} would make this crate a workspace member",
            manifest.display()
        );
        dir = parent;
    }
}

// ---------------------------------------------------------------------------
// R1.1a (a) — the declared MSRV is the floor rmcp requires, READ from rmcp's own
// manifest rather than typed here.
// ---------------------------------------------------------------------------
#[test]
fn the_declared_msrv_is_the_floor_the_sdk_requires() {
    let declared = manifest_key("rust-version").expect("Cargo.toml declares rust-version");
    let (version, floor) = rmcp_msrv();
    assert_eq!(
        declared, floor,
        "this crate declares rust-version {declared} and rmcp {version} requires {floor}"
    );
}

// ---------------------------------------------------------------------------
// THE LAYOUT THIS SUITE IS RUNNING IN, asserted rather than inferred.
//
// The crate is published into a tree that carries `framework-rust/` beside
// `framework/` and NO client source and NO git repository — and the port
// manifest's declared test command is executed there, so a test that assumed
// this repository would red the publish census for a reason that is not a
// defect. Each row below states what it asserts in EACH layout; neither arm is
// a skip.
// ---------------------------------------------------------------------------
enum Layout {
    Source,
    Published,
}

fn layout() -> Layout {
    let framework = crate_root().join("..").join("framework").join("contract");
    assert!(
        framework.is_dir(),
        "the sibling framework module must resolve in either layout: {} is not a directory",
        framework.display()
    );
    let client = crate_root()
        .join("..")
        .join("..")
        .join("knowledge")
        .join("internal")
        .join("externalcollector")
        .join("contract");
    if client.is_dir() {
        Layout::Source
    } else {
        Layout::Published
    }
}

// ---------------------------------------------------------------------------
// R1.1a (b) — Cargo.lock is TRACKED in this repository, and SHIPPED in the
// published tree.
//
// `cargo new` writes a .gitignore of only /target, and the Rust LIBRARY
// convention of omitting a lockfile is one deliberate line away. The committed
// lockfile is what the vulnerability-alert surface follows, what `--locked`
// needs on a fresh checkout, and what the whole-surface leak census reads. In
// the published tree the observable is that it ARRIVED: the manifest's declared
// build and test commands both pass `--locked`, which errors by name against a
// tree with no lockfile.
// ---------------------------------------------------------------------------
#[test]
fn the_lockfile_is_tracked() {
    let root = crate_root();
    assert!(root.join("Cargo.lock").is_file(), "Cargo.lock exists");

    let Layout::Source = layout() else {
        return;
    };

    let output = Command::new("git")
        .arg("-C")
        .arg(&root)
        .args(["ls-files", "--error-unmatch", "Cargo.lock"])
        .output()
        .expect("git runs");
    assert!(
        output.status.success(),
        "Cargo.lock must be TRACKED, not merely present: git ls-files said {}{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
}

// ---------------------------------------------------------------------------
// R1.1a (c) — the manifest declares the licence.
//
// The crate carries NO LICENSE file of its own: the published root carries one,
// placed there by the publish sync, and every source file carries an SPDX
// header (tests/contract_pin.rs). This field is the third leg of that
// arrangement.
// ---------------------------------------------------------------------------
#[test]
fn the_manifest_declares_the_licence() {
    assert_eq!(
        manifest_key("license").as_deref(),
        Some("Apache-2.0"),
        "the crate declares its licence in the manifest"
    );
    let root = crate_root();
    for name in ["LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE"] {
        assert!(
            !root.join(name).exists(),
            "the crate carries no LICENSE file of its own; the published root carries one"
        );
    }
}

// ---------------------------------------------------------------------------
// The crate's build output is ignored here, and ABSENT from the published tree.
//
// The publish staging copies each shipped directory from DISK rather than from
// the index, so an unignored build tree on a developer machine would be staged
// for publication. Both arms below are that one hazard, observed where it can
// be observed.
// ---------------------------------------------------------------------------
#[test]
fn the_build_output_is_ignored() {
    let root = crate_root();

    let Layout::Source = layout() else {
        // IN THE PUBLISHED LAYOUT THERE IS NOTHING TO ASSERT ABOUT target/, and
        // the reason is that this very run creates one: the crate's declared
        // test command is executed in the staged tree, so cargo has written a
        // build directory before this line reads for it. The staging-time
        // observation belongs to the publish gate, which stages and inspects
        // before anything is built.
        //
        // So this arm asserts the NARROWING instead, which is falsifiable: the
        // sibling framework module's contract documents are here and the client
        // tree is not, which is what makes this the published layout rather
        // than a tree the row was wrong to skip.
        let framework = root.join("..").join("framework").join("contract");
        assert!(framework.join("collector_input.schema.json").is_file());
        assert!(framework.join("collector_output.schema.json").is_file());
        assert!(!root.join("..").join("..").join("knowledge").exists());
        return;
    };

    let status = Command::new("git")
        .arg("-C")
        .arg(&root)
        .args(["check-ignore", "-q", "target/debug"])
        .status()
        .expect("git runs");
    assert!(
        status.success(),
        "the crate's target/ must be ignored: the publish staging copies on-disk directories, \
         so an unignored build tree is a publish hazard rather than only clutter"
    );
}

// ---------------------------------------------------------------------------
// EVERY SOURCE FILE THE CRATE NEEDS IS TRACKED AND UNIGNORED.
//
// THIS ROW EXISTS BECAUSE ITS ABSENCE SHIPPED. The crate's binary targets are
// auto-discovered from `src/bin/`, the repository's ignore file carried a bare
// `bin/` that matches at ANY depth, and so `git add` skipped both binary sources
// in silence: the commit did not compile, and the publish staging — which prunes
// whatever this repository ignores — would have dropped them even had they been
// restored on disk.
//
// THE TWO ASSERTIONS THAT WERE ALREADY HERE COULD NOT SEE IT, and that is the
// generalizing lesson rather than a detail. The lockfile row asks git about ONE
// named file; the SPDX row walks the DISK, where the files were present. Neither
// can observe a file that exists locally and is absent from the index. This row
// asks the question in the direction that catches the whole class: is anything
// under src/ or tests/ ignored?
// ---------------------------------------------------------------------------
#[test]
fn no_source_file_the_crate_needs_is_ignored_or_untracked() {
    let root = crate_root();
    let Layout::Source = layout() else {
        // The published tree is not a git repository, so there is no index to
        // ask. What IS assertable there is the consequence: the binary sources
        // arrived. Both arms below are that same property, observed where each
        // can be.
        for name in ["sample-rs.rs", "conformance-stub.rs"] {
            assert!(
                root.join("src").join("bin").join(name).is_file(),
                "the published crate must carry src/bin/{name}, or it does not compile"
            );
        }
        return;
    };

    // (a) NOTHING under src/ or tests/ is ignored. This is the exact command the
    // publish staging uses to decide what to prune, so a non-empty answer here
    // is a file the publish would drop.
    let ignored = Command::new("git")
        .arg("-C")
        .arg(&root)
        .args([
            "ls-files",
            "--others",
            "--ignored",
            "--exclude-standard",
            "--directory",
            "--",
            "src",
            "tests",
        ])
        .output()
        .expect("git runs");
    let ignored = String::from_utf8_lossy(&ignored.stdout).to_string();

    // THE SAME-RUN KNOWN POSITIVE for the reader itself. A clean tree makes this
    // command answer empty, and so does a command that asks the wrong question —
    // which is how the defect this row exists for survived in the first place.
    // Plant a file under the one path this crate DOES ignore, confirm the reader
    // reports it, and remove it.
    let probe_dir = root.join("target");
    std::fs::create_dir_all(&probe_dir).expect("the probe directory is created");
    let probe = probe_dir.join(".ignored-probe");
    std::fs::write(&probe, b"probe").expect("the probe is written");
    let saw = Command::new("git")
        .arg("-C")
        .arg(&root)
        .args([
            "ls-files",
            "--others",
            "--ignored",
            "--exclude-standard",
            "--directory",
            "--",
            "target",
        ])
        .output()
        .expect("git runs");
    let saw = String::from_utf8_lossy(&saw.stdout).to_string();
    let _ = std::fs::remove_file(&probe);
    let _ = std::fs::remove_dir(&probe_dir);
    assert!(
        saw.contains("target"),
        "control: the reader must report an ignored path when there is one, or its silence \
         above means nothing. It answered {saw:?}"
    );

    assert!(
        ignored.trim().is_empty(),
        "these paths under src/ or tests/ are IGNORED, so `git add` skips them and the publish \
         staging prunes them:\n{ignored}"
    );

    // (b) AND every .rs file on disk is in the index. (a) catches an ignore rule;
    // this catches a file simply never added, which no ignore rule explains.
    let tracked = Command::new("git")
        .arg("-C")
        .arg(&root)
        .args(["ls-files", "--", "src", "tests"])
        .output()
        .expect("git runs");
    let tracked: Vec<String> = String::from_utf8_lossy(&tracked.stdout)
        .lines()
        .map(str::to_string)
        .collect();
    assert!(
        !tracked.is_empty(),
        "control: git listed no tracked file under src/ or tests/, which is a reader defect \
         rather than an empty crate"
    );

    fn rust_files(dir: &Path, root: &Path, out: &mut Vec<String>) {
        let Ok(entries) = std::fs::read_dir(dir) else {
            return;
        };
        for entry in entries.flatten() {
            let path = entry.path();
            if path.is_dir() {
                rust_files(&path, root, out);
            } else if path.extension().is_some_and(|e| e == "rs") {
                if let Ok(rel) = path.strip_prefix(root) {
                    out.push(rel.to_string_lossy().replace('\\', "/"));
                }
            }
        }
    }
    let mut on_disk = Vec::new();
    rust_files(&root.join("src"), &root, &mut on_disk);
    rust_files(&root.join("tests"), &root, &mut on_disk);
    on_disk.sort();
    assert!(
        on_disk.len() >= 20,
        "control: the walk found only {} Rust files, fewer than this crate has",
        on_disk.len()
    );
    let untracked = |files: &[String]| -> Vec<String> {
        files
            .iter()
            .filter(|f| !tracked.iter().any(|t| t == *f))
            .cloned()
            .collect()
    };
    assert_eq!(
        untracked(&on_disk),
        Vec::<String>::new(),
        "these files are on disk and NOT in the index; the commit would not carry them"
    );

    // THE SAME-RUN KNOWN POSITIVE. A clean tree makes the comparison above
    // answer empty whether or not it compares anything, which is the shape of
    // the defect this row exists for. Plant an untracked source file, confirm
    // the comparison names it, and remove it.
    let probe = root.join("src").join("untracked_probe_do_not_commit.rs");
    std::fs::write(&probe, b"// SPDX-License-Identifier: Apache-2.0\n").expect("the probe is written");
    let mut with_probe = on_disk.clone();
    with_probe.push("src/untracked_probe_do_not_commit.rs".to_string());
    let detected = untracked(&with_probe);
    let _ = std::fs::remove_file(&probe);
    assert_eq!(
        detected,
        vec!["src/untracked_probe_do_not_commit.rs".to_string()],
        "control: the comparison must name an untracked source file, or its silence above \
         means nothing"
    );

    // (c) THE TWO BINARY TARGETS BY NAME, because they are the ones this row was
    // written for and because a walk that found them by accident would not say so.
    for name in ["src/bin/sample-rs.rs", "src/bin/conformance-stub.rs"] {
        assert!(
            tracked.iter().any(|t| t == name),
            "{name} is a binary target cargo auto-discovers; without it in the index the crate \
             does not compile"
        );
    }
}

// ---------------------------------------------------------------------------
// BOTH LEAK INSTRUMENTS REACH THIS CRATE, and each says so with a known
// positive.
//
// WHY AN ASSERTION AND NOT A SENTENCE. Row R6.3 asks that the crate's files be
// shown present in the scanned set of both instruments — the pre-commit gate,
// which reads only the lines a commit ADDS, and the whole-surface census, which
// reads whole files — "each with a same-run known positive, so a green cannot
// mean scanned nothing". A clean run over a crate that was never in the corpus
// is indistinguishable from a clean run over one that was.
//
// THE INSTRUMENT IS THE SHIP-SET DECLARATION, not the banner. Both instruments
// derive their scan set from `sync-to-contrib.sh --print-ship-paths`, so the
// question "is this crate scanned" is answered by asking that declaration
// whether it covers this directory — and the planted positive below proves the
// matching is real rather than a substring coincidence.
// ---------------------------------------------------------------------------
#[test]
fn both_leak_instruments_cover_this_crate() {
    let Layout::Source = layout() else {
        // The instruments are repository machinery and are not published.
        return;
    };
    let repo = crate_root().join("..").join("..").join("..");

    let output = Command::new("bash")
        .arg(repo.join("scripts").join("sync-to-contrib.sh"))
        .arg("--print-ship-paths")
        .output()
        .expect("the ship-path declaration runs");
    assert!(
        output.status.success(),
        "the ship-path declaration failed: {}",
        String::from_utf8_lossy(&output.stderr)
    );
    let patterns: Vec<String> = String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(str::trim)
        .filter(|l| !l.is_empty())
        .map(str::to_string)
        .collect();
    assert!(
        !patterns.is_empty(),
        "the ship-path declaration printed nothing; that is a reader defect rather than an \
         empty ship set"
    );

    // Both instruments match a staged path against these patterns with a shell
    // `case`, where `*` spans slashes. So the question is whether some pattern's
    // prefix covers this crate's path.
    let covered = |path: &str| {
        patterns.iter().any(|p| {
            let prefix = p.trim_end_matches('*');
            !prefix.is_empty() && path.starts_with(prefix)
        })
    };

    let ours = "cmd/collectors/framework-rust/Cargo.toml";
    assert!(
        covered(ours),
        "no shipped path pattern covers {ours}; the leak instruments would not read this \
         crate at all. The declaration prints {patterns:?}"
    );
    // The nested files too — a pattern covering only the directory entry would
    // leave every source file unscanned.
    for nested in [
        "cmd/collectors/framework-rust/src/lib.rs",
        "cmd/collectors/framework-rust/src/bin/sample-rs.rs",
        "cmd/collectors/framework-rust/Cargo.lock",
        "cmd/collectors/framework-rust/README.md",
    ] {
        assert!(covered(nested), "no shipped path pattern covers {nested}");
    }

    // THE KNOWN POSITIVE FOR THE MATCHER ITSELF, in the same run: a path that is
    // NOT in the ship set is not reported as covered. Without it a matcher that
    // answered true for everything would pass every assertion above.
    for outside in [
        "cmd/knowledge/internal/tools/collect.go",
        "scripts/oss-leak-census.sh",
        "README.md",
    ] {
        assert!(
            !covered(outside),
            "control: {outside} is not in the contrib ship set and the matcher says it is"
        );
    }
}

// ---------------------------------------------------------------------------
// The port manifest ships and declares the four keys the collector censuses
// read. Its file name and key spellings are pinned in tests/pending_pins.rs.
// ---------------------------------------------------------------------------
#[test]
fn the_port_manifest_ships_and_declares_its_four_keys() {
    // THE SEPARATOR IS A TAB, not a space and not an equals sign: the contract
    // chose it so a build or test command can carry spaces with no quoting rule
    // for four readers in two languages to disagree about. A manifest written
    // with spaces parses as one key and no value.
    let text = std::fs::read_to_string(crate_root().join("collector-manifest.tsv"))
        .expect("the port manifest opens");
    // The DECLARED VALUES, read past the comment block: a prose paragraph that
    // mentions a flag is not a command that passes it.
    let declared: Vec<(&str, &str)> = text
        .lines()
        .map(str::trim)
        .filter(|l| !l.is_empty() && !l.starts_with('#'))
        .filter_map(|l| l.split_once('\t'))
        .map(|(k, v)| (k.trim(), v.trim()))
        .collect();
    for key in ["language", "build", "test", "ci_leg"] {
        assert!(
            declared.iter().any(|(k, _)| *k == key),
            "the manifest declares {key}: {declared:?}"
        );
    }
    assert_eq!(
        declared.iter().find(|(k, _)| *k == "language").map(|(_, v)| *v),
        Some("rust")
    );
    for (key, value) in &declared {
        if *key == "build" || *key == "test" {
            assert!(
                !value.contains("--offline"),
                "the declared {key} must not forbid the fetch that populates a cold machine; \
                 the no-network assertion belongs in the CI leg, which pre-fetches first: {value}"
            );
            assert!(
                value.contains("--locked"),
                "the declared {key} must pin the lockfile: {value}"
            );
        }
    }
}

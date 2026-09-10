// SPDX-License-Identifier: Apache-2.0

//! publish_trap.rs — the ONE STRING NO FILE THIS CRATE SHIPS MAY CONTAIN.
//!
//! WHAT THE TRAP IS. The publish staging rewrites the in-repo module path in Go
//! sources, module files and sums, and then asserts that the path SURVIVES
//! NOWHERE in the staged tree — and that survivor scan reads every file, not
//! only the ones the rewrite touched. So a README, a manifest, a comment or a
//! build artifact of this crate carrying the in-repo path aborts the publish for
//! the whole repository.
//!
//! WHY THE NEEDLE IS BUILT AT RUN TIME AND NEVER WRITTEN WHOLE. A test that
//! searched for the path would have to name it, and naming it would make this
//! file itself a survivor. Spelling it in adjacent pieces is NOT enough, and
//! that is a measured fact rather than a caution: a sibling port wrote its needle
//! as three concatenated literals, its language's compiler folded them into ONE
//! literal in the compiled bytecode, the staging copied that build artifact from
//! disk, and the survivor scan found the path in it. A join over separate strings
//! is a run-time call that no compiler folds, so neither this source nor anything
//! built from it carries the path.
//!
//! WHY IT READS BYTES AND PRUNES NOTHING. The same incident had a second half:
//! the port's own guard walked its directory with the build-output directory
//! pruned and read files as text, so it could not have seen the artifact even if
//! it had looked. This walk prunes nothing and compares bytes, so it observes the
//! same corpus the publish gate greps.

use std::path::{Path, PathBuf};

fn crate_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
}

/// in_the_source_repository reports whether this suite is running in this
/// repository or in the STAGED PUBLISHED LAYOUT the publish sync produces, and
/// ASSERTS the shape of whichever it found rather than inferring one from a
/// missing file.
///
/// IT MATTERS TO EXACTLY ONE THING HERE: the build tree. In this repository a
/// build tree inside the crate is a real publish hazard, because the staging
/// copies this directory from disk. In the staged layout the crate's own
/// declared test command is what CREATES one, so its absence there is not a
/// property anything can observe after the fact — the staging-time observation
/// is the publish gate's, and it happens before anything is built.
fn in_the_source_repository() -> bool {
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
        return true;
    }
    assert!(
        !crate_root().join("..").join("..").join("knowledge").exists(),
        "the client tree is neither fully present nor absent; this is a half-staged tree, \
         which is a red rather than a layout"
    );
    false
}

/// in_repo_module_prefix builds the in-repo collector module prefix at RUN TIME.
/// See the module doc: a concatenation of adjacent literals is foldable and this
/// is not.
fn in_repo_module_prefix() -> String {
    ["github.com", "fulminate-io", "knowledge", "cmd", "collectors"].join("/")
}

/// client_module_path is the SHORTER needle, and the one the isolation census
/// actually greps for: the client repository's module path itself, whether or
/// not a collector path follows it.
///
/// IT IS A SEPARATE NEEDLE BECAUSE IT CATCHES A WIDER CLASS. A port file naming
/// the client repository couples the port to this repository even where no
/// module path follows, and the publish's rewrite touches only Go files, so such
/// a line survives into the published tree. This guard carries both so the crate
/// reds on its own before either gate does.
fn client_module_path() -> String {
    ["github.com", "fulminate-io", "knowledge"].join("/")
}

/// every_file walks the crate directory with NOTHING pruned.
fn every_file(dir: &Path, out: &mut Vec<PathBuf>) {
    let Ok(entries) = std::fs::read_dir(dir) else {
        return;
    };
    for entry in entries.flatten() {
        let path = entry.path();
        match std::fs::symlink_metadata(&path) {
            Ok(meta) if meta.is_dir() => every_file(&path, out),
            Ok(meta) if meta.is_file() => out.push(path),
            _ => {}
        }
    }
}

/// contains reports whether haystack holds needle, over BYTES. A text read would
/// fail on a binary before it could answer.
fn contains(haystack: &[u8], needle: &[u8]) -> bool {
    haystack
        .windows(needle.len())
        .any(|window| window == needle)
}

// ---------------------------------------------------------------------------
// No file this crate ships carries the in-repo module path.
//
// The scan covers the whole directory, including any build output a developer's
// tree happens to hold, because the publish staging copies from DISK and the
// survivor scan reads every staged file.
// ---------------------------------------------------------------------------
#[test]
fn no_shipped_file_carries_the_in_repo_module_path() {
    let needle = in_repo_module_prefix();
    let needle = needle.as_bytes();

    let mut files = Vec::new();
    every_file(&crate_root(), &mut files);
    if !in_the_source_repository() {
        // IN THE STAGED LAYOUT ONLY, and for one reason: the build tree here
        // post-dates the staging by construction — the declared test command
        // created it — so it is not part of what was published and scanning it
        // would be scanning this run's own output. In THIS REPOSITORY nothing is
        // skipped, because there a developer's build tree is exactly what the
        // staging would copy, which is the class a sibling port's guard missed
        // by pruning.
        let target = crate_root().join("target");
        files.retain(|p| !p.starts_with(&target));
    }
    files.sort();
    assert!(
        files.len() >= 20,
        "the walk found only {} files, which is fewer than this crate ships; \
         a green over a walk that saw nothing is not a green",
        files.len()
    );
    // AND IT REACHED EVERY DIRECTORY THIS CRATE SHIPS, which is the claim the
    // count alone does not make: the sibling port's guard walked its tree with
    // the build-output directory PRUNED, so it could not have seen the artifact
    // that carried the needle even though its own count looked healthy.
    for required in ["src", "tests", "contract"] {
        assert!(
            files.iter().any(|p| p
                .strip_prefix(crate_root())
                .is_ok_and(|rel| rel.starts_with(required))),
            "the walk reached no file under {required}/, so it prunes something and cannot \
             observe the corpus the publish gate greps"
        );
    }

    // THE PLANTED POSITIVE CONTROL, in the same run and through the same reader:
    // the scan finds the needle when it is there. Without this, a reader that
    // silently matched nothing would pass every assertion below.
    assert!(
        contains(&[b"prefix: ".to_vec(), needle.to_vec()].concat(), needle),
        "control: the byte scan must find the needle when it is present"
    );
    // THE NEGATIVE CONTROL: the PUBLISHED repository's path is not the in-repo
    // one, so a document pointing a reader at knowledge-contrib is not a hit.
    // It is spelled as the URL a document would carry, which is also what the
    // isolation census exempts: what that census refuses is the path used AS a
    // module path, and a scheme separator in front of it is what distinguishes
    // the two.
    let published = ["https:/", "", "github.com", "fulminate-io", "knowledge-contrib"].join("/");
    assert!(
        !contains(published.as_bytes(), needle),
        "control: the published path is NOT the in-repo needle, so a document naming \
         knowledge-contrib is not a false positive"
    );

    // THE CLASSIFICATION IS EXERCISED DIRECTLY, on synthetic buffers, BEFORE it
    // is applied to the tree. Both needles find nothing in a clean crate by
    // construction, so a classifier that had stopped classifying would pass the
    // whole scan below in silence; these four cells are what make the zero
    // readable.
    assert_eq!(
        offence(format!("prefix {}/framework-rust", in_repo_module_prefix()).as_bytes()),
        Some(Offence::InRepoCollectorPath),
        "control: the in-repo collector module path is an offence"
    );
    assert_eq!(
        offence(format!("import {}/x", client_module_path()).as_bytes()),
        Some(Offence::ClientModulePath),
        "control: the client repository's module path, used as a path, is an offence"
    );
    assert_eq!(
        offence(format!("see https://{}", client_module_path()).as_bytes()),
        None,
        "control: the same path after a scheme separator is a DOCUMENTATION URL and is not an \
         offence — pointing a reader at the project is not coupling a port to it"
    );
    assert_eq!(
        offence(b"nothing to see here"),
        None,
        "control: ordinary text is not an offence"
    );

    let mut offenders = Vec::new();
    for path in &files {
        let Ok(bytes) = std::fs::read(path) else {
            panic!(
                "{} could not be read; a file the scan cannot open is a red, because the \
                 publish gate WILL read it",
                path.display()
            );
        };
        if let Some(kind) = offence(&bytes) {
            offenders.push(format!("{} ({})", path.display(), kind.describe()));
        }
    }
    assert!(
        offenders.is_empty(),
        "these files couple this port to the client repository and would abort the contrib \
         publish for the whole repository: {offenders:?}"
    );
}

/// find is [`contains`] returning the offset, so the caller can look at what
/// precedes the hit.
fn find(haystack: &[u8], needle: &[u8]) -> Option<usize> {
    haystack
        .windows(needle.len())
        .position(|window| window == needle)
}

/// Offence is what a file's bytes did wrong, if anything.
#[derive(Debug, PartialEq, Eq, Clone, Copy)]
enum Offence {
    /// The in-repo collector module prefix, which the publish rewrite touches
    /// only in Go files and the survivor scan then finds everywhere else.
    InRepoCollectorPath,
    /// The client repository's own module path, used as a path rather than as a
    /// documentation URL.
    ClientModulePath,
}

impl Offence {
    fn describe(self) -> &'static str {
        match self {
            Offence::InRepoCollectorPath => "the in-repo collector module path",
            Offence::ClientModulePath => "the client repository's module path, not as a URL",
        }
    }
}

/// offence classifies one file's bytes.
///
/// A `://` IMMEDIATELY BEFORE the client repository's path makes it a
/// DOCUMENTATION URL rather than a module reference — pointing a reader at the
/// project is not coupling a port to it — which is the same distinction the
/// isolation census draws. It is a function so the crate's own suite can drive
/// it on synthetic buffers, which is the only way a needle that finds nothing in
/// a clean tree can be shown to work at all.
fn offence(bytes: &[u8]) -> Option<Offence> {
    if contains(bytes, in_repo_module_prefix().as_bytes()) {
        return Some(Offence::InRepoCollectorPath);
    }
    let client = client_module_path();
    let at = find(bytes, client.as_bytes())?;
    const SCHEME: &[u8] = b"://";
    if at >= SCHEME.len() && &bytes[at - SCHEME.len()..at] == SCHEME {
        return None;
    }
    Some(Offence::ClientModulePath)
}

// ---------------------------------------------------------------------------
// This test file itself does not carry the needle, which is the half the sibling
// port's incident turned on: the guard became the survivor.
//
// It is a separate row because it is a separate claim, and because it is the one
// that reds if someone "simplifies" the run-time join into a literal.
// ---------------------------------------------------------------------------
#[test]
fn this_guard_does_not_carry_the_needle_it_searches_for() {
    let needle = in_repo_module_prefix();
    let source = std::fs::read(crate_root().join("tests/publish_trap.rs"))
        .expect("this test file opens");
    assert!(
        !contains(&source, needle.as_bytes()),
        "the guard must not spell the path it searches for; build it at run time"
    );

    // ...and the pieces ARE there, so the row above is about the assembled path
    // rather than about a file that mentions nothing.
    assert!(
        contains(&source, b"fulminate-io"),
        "control: the pieces are present, so the absence above is the JOINED path"
    );
}

// ---------------------------------------------------------------------------
// The crate's build output lives OUTSIDE this directory, so the publish staging
// has nothing of it to copy.
//
// The .gitignore entry stops a build tree being committed; it does not stop the
// staging copying one, because the staging reads the filesystem rather than the
// index. Keeping the target directory outside the crate is what actually closes
// it, and this row is where that is stated as an observable rather than as a
// habit.
// ---------------------------------------------------------------------------
#[test]
fn the_crate_directory_holds_no_build_output() {
    if !in_the_source_repository() {
        // See in_the_source_repository: the declared test command is what makes
        // a build tree here, so this row asserts the NARROWING instead, which is
        // falsifiable — the sibling module's contract documents are present and
        // the client tree is not.
        let framework = crate_root().join("..").join("framework").join("contract");
        assert!(framework.join("collector_input.schema.json").is_file());
        assert!(!crate_root().join("..").join("..").join("knowledge").exists());
        return;
    }
    let target = crate_root().join("target");
    assert!(
        !target.exists(),
        "{} exists: the publish staging copies this directory from disk, so a build tree \
         here is staged for publication and every file in it is read by the survivor scan. \
         Build with CARGO_TARGET_DIR pointing outside the crate.",
        target.display()
    );
}

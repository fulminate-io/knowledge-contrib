// SPDX-License-Identifier: Apache-2.0

//! conformance-stub — the TEST-ONLY provider the knowledge client's own dialing
//! tests drive against this port.
//!
//! IT IS NOT A COLLECTOR AND MUST NEVER BE REGISTERED AS ONE. Several of its
//! modes advertise deliberately non-conforming schemas, return deliberately
//! non-conforming results, or kill the process at a named point; that is what
//! makes it useful to the tests and useless to an operator.
//!
//! A CHILD WITH NO MODE EXITS LOUD rather than defaulting to one. The mode is
//! delivered through the entry's environment block, which is itself one of the
//! things under test, so its absence is the interesting failure and is reported
//! as one.
//!
//! STDOUT IS THE PROTOCOL STREAM. Every diagnostic below goes to stderr.

use knowledge_collector_framework::conformance::{
    run, Mode, MARKER_FILE_FLAG, STUB_MODE_ENV, STUB_TOOL_ENV,
};

#[tokio::main]
async fn main() {
    let Ok(name) = std::env::var(STUB_MODE_ENV) else {
        eprintln!(
            "conformance stub: {STUB_MODE_ENV} is UNSET — the entry's env block did not reach this child. \
             Exiting rather than guessing a mode."
        );
        std::process::exit(9);
    };
    let mode = match Mode::parse(&name) {
        Ok(mode) => mode,
        Err(e) => {
            eprintln!("{e}");
            std::process::exit(9);
        }
    };
    let configured_tool = std::env::var(STUB_TOOL_ENV).unwrap_or_default();

    // THE MARKER FILE RIDES ARGV, because a collector's environment is the
    // registration entry's block and nothing this process's parent exported
    // reaches it. An unrecognised argument is REFUSED rather than ignored: a
    // typo'd flag would otherwise leave the marker file empty and read as a seam
    // that never engaged.
    let mut args = std::env::args().skip(1);
    let mut marker_file: Option<String> = None;
    while let Some(arg) = args.next() {
        if arg == MARKER_FILE_FLAG {
            let Some(path) = args.next() else {
                eprintln!("conformance stub: {MARKER_FILE_FLAG} needs a path");
                std::process::exit(9);
            };
            marker_file = Some(path);
            continue;
        }
        eprintln!("conformance stub: unrecognised argument {arg:?}; this stub takes only {MARKER_FILE_FLAG} <path>");
        std::process::exit(9);
    }

    if let Err(e) = run(mode, &configured_tool, marker_file.as_deref()).await {
        eprintln!("conformance stub: {e}");
        std::process::exit(1);
    }
}

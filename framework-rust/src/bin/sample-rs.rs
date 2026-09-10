// SPDX-License-Identifier: Apache-2.0

//! sample-rs — the worked example: a custom knowledge collector that walks a
//! directory into a graph of directories and files.
//!
//! Everything it does lives in the library, in `sample.rs`, so the crate's own
//! suite can drive the walk directly. This file is the entry point and nothing
//! else.
//!
//! STDOUT IS THE PROTOCOL STREAM: this binary speaks MCP on stdin and stdout.
//! Any diagnostic goes to stderr, which is where the `eprintln!` below writes.

use knowledge_collector_framework::sample::SampleCollector;
use knowledge_collector_framework::serve::serve_stdio;

#[tokio::main]
async fn main() {
    if let Err(e) = serve_stdio(SampleCollector).await {
        eprintln!("sample-rs: {e}");
        std::process::exit(1);
    }
}

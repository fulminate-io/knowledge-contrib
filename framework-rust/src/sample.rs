// SPDX-License-Identifier: Apache-2.0

//! sample.rs — the WORKED EXAMPLE the crate's README walks through, and the
//! collector the end-to-end confirmation registers and collects.
//!
//! IT WALKS A DIRECTORY AND READS NO CREDENTIAL. That is deliberate rather than
//! minimal: a worked example an operator can run on their own laptop, against
//! any directory, without an account or a token, is the one that gets run. Its
//! entry names no environment variable and this file reads none.
//!
//! IT LIVES IN THE LIBRARY RATHER THAN ONLY IN `src/bin/`, so the crate's own
//! suite can drive its walk directly — which is what lets a test derive the
//! types the walk EMITS from the walk itself and compare them against the
//! vocabulary it DECLARES, instead of comparing one hand-written list against
//! another. `src/bin/sample-rs.rs` is the ten-line `main` over it.
//!
//! WHAT `max_depth` IS FOR AND WHY IT ASSERTS INCOMPLETENESS. A walk that stops
//! early has not enumerated the whole source, and the completeness assertion is
//! how it says so: `walk_complete: false` disables the server's deletion phase,
//! so a truncated walk that asserted COMPLETE would let the server treat every
//! file it did not reach as deleted. That is the one arm of the collector
//! contract that is invisible in a happy-path example, so the example carries it.
//!
//! `demo_panic` IS A DEMONSTRATION SWITCH AND IT SAYS SO. A production collector
//! never takes a parameter that makes it fail; this one does, because the other
//! arm an operator cannot see from a happy path is what a FAILED walk looks like
//! from the client's side — a refused collect naming the collector, rather than
//! an empty graph. It is documented in the README as such.

use std::collections::BTreeMap;
use std::fs;
use std::path::Path;

use schemars::JsonSchema;
use serde::Deserialize;

use crate::completeness::Completeness;
use crate::context::ForeignContext;
use crate::describe::{Behavior, Declaration, Vocabulary};
use crate::error::WalkError;
use crate::framework::{Collector, ToolSpec};
use crate::model::{Edge, Node, WalkResult};

/// NODE_TYPE_DIRECTORY is the node type this collector emits for a directory.
pub const NODE_TYPE_DIRECTORY: &str = "directory";
/// NODE_TYPE_FILE is the node type this collector emits for a file.
pub const NODE_TYPE_FILE: &str = "file";
/// EDGE_TYPE_CONTAINS is the edge type this collector emits from a directory to
/// what it holds.
pub const EDGE_TYPE_CONTAINS: &str = "contains";

/// SampleParams is what an operator's collect params carry.
#[derive(Debug, Clone, Default, Deserialize, JsonSchema)]
#[serde(deny_unknown_fields)]
pub struct SampleParams {
    /// The directory to walk. Required: a blank root names no source.
    #[serde(default)]
    pub root: String,
    /// How deep to descend. Zero means no limit; a walk that stops at this depth
    /// asserts an INCOMPLETE result carrying the reason.
    #[serde(default)]
    pub max_depth: u32,
    /// Panic inside the walk. A DEMONSTRATION SWITCH, so an operator can see
    /// what a failed collect looks like from the client. No production collector
    /// should carry one.
    #[serde(default)]
    pub demo_panic: bool,
    /// Emit one node whose type this collector's declaration does NOT name.
    ///
    /// ALSO A DEMONSTRATION SWITCH, and for the arm that has no other producer:
    /// the client refuses a collect carrying a node type the entry's declared
    /// vocabulary omits, naming the node and the offending value, and there is no
    /// way to drive that refusal from a collector that keeps its word. A
    /// production collector never carries one; this one does so the refusal can
    /// be observed end to end rather than described.
    #[serde(default)]
    pub demo_undeclared_node_type: bool,
}

/// UNDECLARED_NODE_TYPE is the type [`SampleParams::demo_undeclared_node_type`]
/// emits. It is deliberately NOT in the declared vocabulary.
pub const UNDECLARED_NODE_TYPE: &str = "undeclared_probe";

/// SampleCollector walks a directory into a graph of directories and files.
#[derive(Debug, Default, Clone)]
pub struct SampleCollector;

impl SampleCollector {
    /// declaration is what this collector says about itself: the vocabulary its
    /// walk emits, the environment it needs (none) and the foreign context it
    /// reads (none).
    pub fn declaration() -> Declaration {
        let mut node_type_fields = BTreeMap::new();
        node_type_fields.insert(
            NODE_TYPE_FILE.to_string(),
            vec!["file_path".to_string(), "metadata".to_string()],
        );
        node_type_fields.insert(
            NODE_TYPE_DIRECTORY.to_string(),
            vec!["file_path".to_string()],
        );
        Declaration {
            behavior: Behavior {
                // A directory graph is worth a summary and a vector, and it
                // syncs; the fields named are the ones this walk actually fills.
                summarizable: true,
                embeddable: true,
                syncable: true,
                embed_fields: vec!["summary".to_string()],
                summarize_fields: vec!["summary".to_string(), "file_path".to_string()],
                bm25_fields: vec!["symbol_name".to_string(), "file_path".to_string()],
            },
            walks_the_whole_source: true,
            emits_cross_graph_edges: false,
            reads_foreign_context: false,
            env: Vec::new(),
            foreign_context: Vec::new(),
            vocabulary: Vocabulary::new(
                vec![
                    NODE_TYPE_DIRECTORY.to_string(),
                    NODE_TYPE_FILE.to_string(),
                ],
                vec![EDGE_TYPE_CONTAINS.to_string()],
            ),
            node_type_fields,
        }
    }
}

impl Collector for SampleCollector {
    type Params = SampleParams;

    fn tool(&self) -> ToolSpec {
        ToolSpec::new(
            "collect",
            "walk a directory into a graph of directories and files",
        )
    }

    fn declaration(&self) -> Declaration {
        SampleCollector::declaration()
    }

    fn walk(
        &self,
        _id: &str,
        params: SampleParams,
        _foreign: &ForeignContext,
    ) -> Result<WalkResult, WalkError> {
        if params.demo_panic {
            panic!("sample collector: demo_panic was set, so this walk panicked deliberately");
        }
        if params.root.is_empty() {
            return Err("sample collector: params.root is empty; it names the directory to walk"
                .to_string()
                .into());
        }
        let root = Path::new(&params.root);
        if !root.is_dir() {
            return Err(format!(
                "sample collector: params.root {:?} is not a directory this process can read",
                params.root
            )
            .into());
        }

        let mut nodes = Vec::new();
        let mut edges = Vec::new();
        let mut truncated_at: Option<String> = None;
        walk_dir(
            root,
            0,
            params.max_depth,
            &mut nodes,
            &mut edges,
            &mut truncated_at,
        )?;

        if params.demo_undeclared_node_type {
            let mut node = Node::new(
                format!("{}#undeclared-probe", params.root),
                UNDECLARED_NODE_TYPE,
            );
            node.summary = "a node whose type this collector's declaration omits".to_string();
            nodes.push(node);
        }

        let complete = match truncated_at {
            None => Completeness::complete(),
            Some(path) => Completeness::incomplete(format!(
                "the walk stopped at max_depth {} and did not descend into {path}",
                params.max_depth
            )),
        };
        Ok(WalkResult::new(nodes, edges, complete))
    }
}

fn walk_dir(
    dir: &Path,
    depth: u32,
    max_depth: u32,
    nodes: &mut Vec<Node>,
    edges: &mut Vec<Edge>,
    truncated_at: &mut Option<String>,
) -> Result<(), WalkError> {
    let dir_id = dir.to_string_lossy().to_string();
    let mut dir_node = Node::new(dir_id.clone(), NODE_TYPE_DIRECTORY);
    dir_node.file_path = dir_id.clone();
    dir_node.summary = format!("directory {dir_id}");
    nodes.push(dir_node);

    if max_depth != 0 && depth >= max_depth {
        *truncated_at = Some(dir_id);
        return Ok(());
    }

    let mut entries: Vec<_> = fs::read_dir(dir)
        .map_err(|e| format!("sample collector: reading {}: {e}", dir.display()))?
        .collect::<Result<Vec<_>, _>>()
        .map_err(|e| format!("sample collector: reading an entry of {}: {e}", dir.display()))?;
    entries.sort_by_key(std::fs::DirEntry::path);

    for entry in entries {
        let path = entry.path();
        let child_id = path.to_string_lossy().to_string();
        let file_type = entry
            .file_type()
            .map_err(|e| format!("sample collector: stat of {}: {e}", path.display()))?;
        if file_type.is_dir() {
            edges.push(Edge::new(
                dir_id.clone(),
                child_id.clone(),
                EDGE_TYPE_CONTAINS,
            ));
            walk_dir(&path, depth + 1, max_depth, nodes, edges, truncated_at)?;
            continue;
        }
        let mut node = Node::new(child_id.clone(), NODE_TYPE_FILE);
        node.file_path = child_id.clone();
        node.symbol_name = path
            .file_name()
            .map(|n| n.to_string_lossy().to_string())
            .unwrap_or_default();
        if let Ok(meta) = entry.metadata() {
            node.metadata
                .insert("size_bytes".to_string(), meta.len().to_string());
        }
        nodes.push(node);
        edges.push(Edge::new(dir_id.clone(), child_id, EDGE_TYPE_CONTAINS));
    }
    Ok(())
}

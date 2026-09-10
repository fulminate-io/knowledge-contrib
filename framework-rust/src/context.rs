// SPDX-License-Identifier: Apache-2.0

//! context.rs — the DECLARED FOREIGN-GRAPH CONTEXT a collect may carry, as the
//! Rust shape a walk receives it in.
//!
//! IT IS A COPY OF THE CLIENT'S SHAPE AND THAT IS THE DESIGN, not an oversight.
//! The contract between the two sides is the checked-in JSON schema pair this
//! crate embeds a copy of; the serde names below are the contract's spellings
//! and they are what keep the two halves the same object.
//!
//! A COLLECTOR NEVER ASKS FOR THIS BLOCK AT RUN TIME. It is DECLARED, once, in
//! the operator's config entry — which graph families the collector needs, which
//! node types, which fields, which metadata keys — and the client fills it from
//! its own graphs before the call. A collector whose entry declares nothing
//! receives an empty block.
//!
//! WHAT A COLLECTOR CAN CONCLUDE FROM AN EMPTY BLOCK: nothing about the
//! operator's graphs unless its own entry declared the family. An absent field
//! means "not declared", never "not present in the graph".
//!
//! A DECLARED FAMILY IS ALWAYS PRESENT AS AN EMPTY ARRAY; A MISSING KEY MEANS
//! THE ENTRY NEVER ASKED. Those are different facts and a collector is entitled
//! to distinguish them. Go's accessor returns a nil slice for both and its doc
//! tells the caller to index the map directly when the difference matters;
//! [`ForeignContext::graphs`] returns an `Option` instead, so the distinction is
//! in the type rather than in a comment.
//!
//! IT IS KEYED BY GRAPH-TYPE NAME, NOT BY A FIXED SET OF FIELDS. The families a
//! client can supply are `code` plus whatever graph types the operator has
//! REGISTERED, which is not a set any type in this crate could enumerate.
//!
//! `deny_unknown_fields` IS DELIBERATELY ABSENT FROM THESE TYPES, unlike the
//! result vocabulary in `model.rs`. This block travels client-to-collector: the
//! client may grow a field before a collector is rebuilt, and refusing an
//! unknown key here would break every deployed collector on the day the client
//! adds one. The refusal belongs on the side the author controls, which is the
//! result.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

/// FAMILY_CODE is the one family name this crate can name as a constant: it is
/// the only supplyable family that is not an operator-registered graph type.
pub const FAMILY_CODE: &str = "code";

/// ForeignContext is the collect input's context block: the foreign-graph slices
/// this collector's registration declared it needs, keyed by graph-type name.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(transparent)]
pub struct ForeignContext(BTreeMap<String, Vec<ForeignGraph>>);

impl ForeignContext {
    /// from_map builds a block from its families. It exists for tests and for a
    /// host embedding this crate; a collector receives one, never builds one.
    pub fn from_map(families: BTreeMap<String, Vec<ForeignGraph>>) -> Self {
        ForeignContext(families)
    }

    /// is_empty reports whether this block carries no family at all, which is
    /// what a collector whose entry declares nothing receives.
    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }

    /// graphs returns the slice declared under one family name, or `None` when
    /// the entry did not declare it.
    ///
    /// `None` AND `Some(&[])` MEAN DIFFERENT THINGS: the first is "the entry
    /// never asked", the second is "the entry asked and the operator's store
    /// held no graph of that type".
    pub fn graphs(&self, family: &str) -> Option<&[ForeignGraph]> {
        self.0.get(family).map(Vec::as_slice)
    }

    /// declared reports whether the entry declared this family at all,
    /// regardless of how many graphs matched.
    pub fn declared(&self, family: &str) -> bool {
        self.0.contains_key(family)
    }

    /// families returns every declared family name, SORTED.
    ///
    /// Sorted because a collector that walks families and emits edges from them
    /// would otherwise emit them in an unspecified order, which makes one
    /// unchanged input produce a different result each run. The map is a
    /// `BTreeMap` so the order is the type's rather than a sort at every call.
    pub fn families(&self) -> Vec<&str> {
        self.0.keys().map(String::as_str).collect()
    }

    /// except returns every declared family's graphs EXCEPT those named,
    /// flattened, in sorted family order.
    ///
    /// IT EXISTS FOR THE COLLECTORS THAT CORRELATE ACROSS PROVIDERS. A log
    /// collector matches its streams against resource metadata and does not care
    /// which provider's graph a resource came from. Naming the families to
    /// EXCLUDE rather than to include is what keeps that working when an
    /// operator registers a provider the collector's author never heard of.
    pub fn except(&self, families: &[&str]) -> Vec<&ForeignGraph> {
        self.0
            .iter()
            .filter(|(name, _)| !families.contains(&name.as_str()))
            .flat_map(|(_, graphs)| graphs.iter())
            .collect()
    }
}

/// ForeignGraph is one graph's declared slice.
///
/// `graph_name` is always carried, even where the declaration asks for no nodes:
/// a predicate that is a membership test over graph names needs the names and
/// nothing else, and that is a legitimate declaration rather than an empty one.
/// The two arrays carry no skip-if-empty, matching the client's own shape.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct ForeignGraph {
    #[serde(default)]
    pub graph_name: String,
    #[serde(default)]
    pub nodes: Vec<ForeignNode>,
    #[serde(default)]
    pub edges: Vec<ForeignEdge>,
}

/// ForeignNode is one node of a declared slice, carrying the declared fields and
/// no others. A field the entry did not declare arrives empty, because it was
/// never sent.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ForeignNode {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub id: String,
    #[serde(rename = "type", default, skip_serializing_if = "String::is_empty")]
    pub node_type: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub symbol_name: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub file_path: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub content: String,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub metadata: BTreeMap<String, String>,
}

/// ForeignEdge is one edge of a declared slice. Endpoints are node ids in the
/// same graph.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ForeignEdge {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub from_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub to_id: String,
}

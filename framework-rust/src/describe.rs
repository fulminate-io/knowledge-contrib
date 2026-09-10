// SPDX-License-Identifier: Apache-2.0

//! describe.rs — the COLLECTOR'S OWN DECLARATION: what it needs, what it emits,
//! and which of those an operator has to supply.
//!
//! WHAT IT IS FOR. A collector knows its own vocabulary and its own inputs; an
//! operator writing a config entry by hand does not. The declaration is rendered
//! by a second MCP tool, and the client's `collector add` calls it and fills the
//! entry from what it reads, so an operator registers a collector without
//! transcribing its node types or guessing which environment variables it wants.
//!
//! THE THREE ENVIRONMENT CLASSES ARE A CLOSED SET: `path`, `selector`, `secret`.
//! The installer branches on exactly those three and fails on anything else, so
//! a class outside the set is refused HERE, by name, rather than reaching an
//! operator as an installer failure about a value they never chose.
//!
//! A SECRET'S VALUE IS NEVER PART OF A DECLARATION. The class is what is
//! declared; the value is the operator's and lives in their entry's env block.
//! Nothing in this module reads an environment variable.
//!
//! WHAT THE RENDERED DECLARATION MAY NOT CARRY: a `reason` key. The client's
//! config loader decodes an entry with unknown fields REFUSED, so a rendered
//! declaration carrying a key the entry schema does not define is refused by
//! name at load time. A declaration type may carry a reason for the collector
//! author's own benefit — [`EnvVar::reason`] does — and [`Declaration::render`]
//! drops it.
//!
//! WHAT IS PINNED TO A SIBLING TICKET AND NOT SETTLED HERE. The describe TOOL's
//! name, the third contract schema file that describes its output, and whether
//! the client re-verifies the declaration at every collect or only at add, are
//! all decided by the ticket that adds the describe tool to the client and to
//! the Go framework. This module builds and VALIDATES the declaration, which is
//! the half that does not depend on any of them; `tests/pending_pins.rs` carries
//! the three that do, each as a test that reds until the sibling lands.

use std::collections::BTreeMap;
use std::fmt;

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};

use crate::error::{Error, Result};

/// EnvClass is what an environment variable a collector needs is FOR. The set is
/// closed: the installer branches on exactly these three.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum EnvClass {
    /// A filesystem path the operator supplies.
    Path,
    /// A selector: a region, a project, an account, a namespace.
    Selector,
    /// A credential. Its VALUE never appears in a declaration.
    Secret,
    /// A variable the collector READS that an installed entry deliberately does
    /// NOT declare. Declaring it says so out loud, which is a different fact
    /// from the collector never having wanted it.
    NotCarried,
}

/// ENV_CLASSES is the closed set the contract's describe schema declares, in the
/// order a refusal names it.
pub const ENV_CLASSES: [&str; 4] = ["path", "selector", "secret", "not-carried"];

impl EnvClass {
    /// as_str is the wire spelling.
    pub fn as_str(&self) -> &'static str {
        match self {
            EnvClass::Path => "path",
            EnvClass::Selector => "selector",
            EnvClass::Secret => "secret",
            EnvClass::NotCarried => "not-carried",
        }
    }

    /// parse refuses a class outside the closed set, NAMING the class it was
    /// given and the three it accepts.
    pub fn parse(class: &str) -> Result<Self> {
        match class {
            "path" => Ok(EnvClass::Path),
            "selector" => Ok(EnvClass::Selector),
            "secret" => Ok(EnvClass::Secret),
            "not-carried" => Ok(EnvClass::NotCarried),
            other => Err(Error::new(format!(
                "framework: the environment class {other:?} is not one this contract defines; the classes are {}",
                ENV_CLASSES.join(", ")
            ))),
        }
    }
}

impl fmt::Display for EnvClass {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// EnvVar is one environment variable a collector needs, with its class.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EnvVar {
    /// name is the variable's name. Blank, or containing `=`, is refused: those
    /// are the two shapes the client's own env-block loader refuses.
    pub name: String,
    /// class is what the variable is for.
    pub class: EnvClass,
    /// reason is for the COLLECTOR AUTHOR's own documentation and is NEVER
    /// rendered: the client's entry schema does not define the key and refuses
    /// an entry that carries it.
    pub reason: Option<String>,
}

impl EnvVar {
    /// new builds a declaration for one variable.
    pub fn new(name: impl Into<String>, class: EnvClass) -> Self {
        EnvVar {
            name: name.into(),
            class,
            reason: None,
        }
    }

    /// with_reason attaches the author-facing reason.
    pub fn with_reason(mut self, reason: impl Into<String>) -> Self {
        self.reason = Some(reason.into());
        self
    }

    fn validate(&self) -> Result<()> {
        if self.name.is_empty() {
            return Err(Error::new(
                "framework: an environment declaration has a blank name; the client's env block refuses a blank key",
            ));
        }
        if self.name.contains('=') {
            return Err(Error::new(format!(
                "framework: the environment name {:?} contains '='; the client's env block refuses such a key",
                self.name
            )));
        }
        Ok(())
    }
}

/// ForeignFamilyDeclaration is one foreign graph family this collector wants a
/// slice of, and the shape of the slice.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ForeignFamilyDeclaration {
    /// family is the graph type: `code`, or any registered custom family.
    pub family: String,
    /// node_types names the node types to send.
    pub node_types: Vec<String>,
    /// all_node_types is the FAMILY-LEVEL SELECTOR: send every node type this
    /// family has. It is refused beside a non-empty [`Self::node_types`],
    /// because the two say different things and a declaration that says both
    /// says neither.
    pub all_node_types: bool,
    /// fields names the node fields to send.
    pub fields: Vec<String>,
    /// metadata_keys names the metadata keys to send.
    pub metadata_keys: Vec<String>,
}

impl ForeignFamilyDeclaration {
    /// new declares a family with an explicit node-type list.
    pub fn new(family: impl Into<String>, node_types: Vec<String>) -> Self {
        ForeignFamilyDeclaration {
            family: family.into(),
            node_types,
            ..Default::default()
        }
    }

    /// all declares a family with the family-level selector set.
    pub fn all(family: impl Into<String>) -> Self {
        ForeignFamilyDeclaration {
            family: family.into(),
            all_node_types: true,
            ..Default::default()
        }
    }

    fn validate(&self, index: usize) -> Result<()> {
        if self.family.is_empty() {
            // IT NAMES WHAT THE AUTHOR CAN FIND. A family declaration with a
            // blank family has no name to quote back, so the refusal quotes what
            // it DOES carry — its position and its node-type selection — which
            // is the only handle an author has on which of several declarations
            // is the offending one. Every other refusal in this file names the
            // offending value for the same reason.
            let selection = if self.all_node_types {
                "all_node_types".to_string()
            } else {
                format!("{:?}", self.node_types)
            };
            return Err(Error::new(format!(
                "framework: foreign-context declaration [{index}] names no family; it selects \
                 {selection}, and a declaration with no family names no graph type to read it from"
            )));
        }
        if self.all_node_types && !self.node_types.is_empty() {
            return Err(Error::new(format!(
                "framework: the foreign-context declaration for the family {:?} sets all_node_types AND names the node types {:?}; \
                 set the selector or list the types, never both",
                self.family, self.node_types
            )));
        }
        Ok(())
    }
}

/// Vocabulary is what a collector's walk EMITS: the node types and the edge
/// types, exhaustively.
///
/// DECLARING IT IS A PROMISE THE INGEST PATH ENFORCES. A collector that declares
/// a vocabulary and then emits a type outside it has its collect refused, naming
/// the node and the offending value. A collector that declares NOTHING is
/// accepted and the client says so once per collect. So an EMPTY vocabulary and
/// a declared one are different postures, not a filled and an unfilled field.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Vocabulary {
    pub node_types: Vec<String>,
    pub edge_types: Vec<String>,
}

impl Vocabulary {
    /// new builds a vocabulary from its two lists.
    pub fn new(node_types: Vec<String>, edge_types: Vec<String>) -> Self {
        Vocabulary {
            node_types,
            edge_types,
        }
    }

    /// is_declared reports whether this collector declared any vocabulary at all.
    pub fn is_declared(&self) -> bool {
        !self.node_types.is_empty() || !self.edge_types.is_empty()
    }

    /// covers_node_types reports every emitted node type this vocabulary does
    /// NOT name. An empty return is the passing case.
    pub fn uncovered_node_types<'a, I: IntoIterator<Item = &'a str>>(
        &self,
        emitted: I,
    ) -> Vec<String> {
        let mut missing: Vec<String> = emitted
            .into_iter()
            .filter(|t| !self.node_types.iter().any(|d| d == t))
            .map(str::to_string)
            .collect();
        missing.sort();
        missing.dedup();
        missing
    }

    /// uncovered_edge_types is [`Self::uncovered_node_types`] for edges.
    pub fn uncovered_edge_types<'a, I: IntoIterator<Item = &'a str>>(
        &self,
        emitted: I,
    ) -> Vec<String> {
        let mut missing: Vec<String> = emitted
            .into_iter()
            .filter(|t| !self.edge_types.iter().any(|d| d == t))
            .map(str::to_string)
            .collect();
        missing.sort();
        missing.dedup();
        missing
    }
}

/// Declaration is everything a collector says about itself.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Behavior {
    /// summarizable: the graph's nodes are worth an LLM summary.
    pub summarizable: bool,
    /// embeddable: the graph's nodes are worth a vector.
    pub embeddable: bool,
    /// syncable: the graph takes part in sync.
    pub syncable: bool,
    /// embed_fields, summarize_fields and bm25_fields name the node fields each
    /// pass reads. An empty list means the client's own default for that pass,
    /// which is a different posture from naming none.
    pub embed_fields: Vec<String>,
    pub summarize_fields: Vec<String>,
    pub bm25_fields: Vec<String>,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Declaration {
    /// behavior is the graph-level posture the client stores on the graph type.
    pub behavior: Behavior,
    /// walks_the_whole_source reports whether an ordinary run enumerates the
    /// entire source, which is what makes an incomplete assertion meaningful.
    /// It is the collector author's own note and is NOT rendered: the frozen
    /// describe document has no key for it.
    pub walks_the_whole_source: bool,
    /// emits_cross_graph_edges reports whether the walk sets a source or target
    /// graph on any edge. Author-facing, not rendered.
    pub emits_cross_graph_edges: bool,
    /// reads_foreign_context reports whether the walk reads the declared block.
    /// Author-facing, not rendered.
    pub reads_foreign_context: bool,
    /// env names every environment variable this collector needs.
    pub env: Vec<EnvVar>,
    /// foreign_context names every family this collector wants a slice of.
    pub foreign_context: Vec<ForeignFamilyDeclaration>,
    /// vocabulary is what the walk emits.
    pub vocabulary: Vocabulary,
    /// node_type_fields is the per-node-type field override: for a node type,
    /// the fields this collector fills.
    pub node_type_fields: BTreeMap<String, Vec<String>>,
}

impl Declaration {
    /// validate refuses every shape the contract does not admit, naming what it
    /// found.
    pub fn validate(&self) -> Result<()> {
        for var in &self.env {
            var.validate()?;
        }
        let mut seen = Vec::new();
        for var in &self.env {
            if seen.contains(&var.name) {
                return Err(Error::new(format!(
                    "framework: the environment name {:?} is declared twice",
                    var.name
                )));
            }
            seen.push(var.name.clone());
        }
        for (index, family) in self.foreign_context.iter().enumerate() {
            family.validate(index)?;
        }
        for node_type in self.node_type_fields.keys() {
            if !self.vocabulary.node_types.iter().any(|t| t == node_type) {
                return Err(Error::new(format!(
                    "framework: a per-node-type field override names the node type {node_type:?}, which this collector's vocabulary does not declare"
                )));
            }
        }
        Ok(())
    }

    /// render validates the declaration and returns the document the describe
    /// tool serves.
    ///
    /// THE SHAPE IS THE CONTRACT'S, NOT THIS CRATE'S. It is
    /// `contract/collector_describe.schema.json` verbatim: a required `behavior`
    /// object, required `node_types`, `edge_types` and `environment`, and the
    /// optional `context` and `node_type_overrides`. The crate's own suite
    /// validates what this returns against that schema, so a shape this function
    /// invented would red rather than reach a client.
    ///
    /// THE RENDERED DOCUMENT CARRIES NO `reason` KEY ANYWHERE. The client's
    /// config loader refuses an entry carrying a key its schema does not define,
    /// and the reason is the collector author's rather than the operator's.
    /// [`EnvVar::reason`] exists for the author and is dropped here.
    ///
    /// THE THREE AUTHOR-FACING BOOLEANS ARE NOT RENDERED EITHER, for the same
    /// reason: the frozen document has no key for them.
    pub fn render(&self) -> Result<Value> {
        self.validate()?;

        let mut behavior = Map::new();
        behavior.insert("summarizable".into(), Value::Bool(self.behavior.summarizable));
        behavior.insert("embeddable".into(), Value::Bool(self.behavior.embeddable));
        behavior.insert("syncable".into(), Value::Bool(self.behavior.syncable));
        if !self.behavior.embed_fields.is_empty() {
            behavior.insert("embed_fields".into(), string_array(&self.behavior.embed_fields));
        }
        if !self.behavior.summarize_fields.is_empty() {
            behavior.insert(
                "summarize_fields".into(),
                string_array(&self.behavior.summarize_fields),
            );
        }
        if !self.behavior.bm25_fields.is_empty() {
            behavior.insert("bm25_fields".into(), string_array(&self.behavior.bm25_fields));
        }

        let env: Vec<Value> = self
            .env
            .iter()
            .map(|v| {
                let mut m = Map::new();
                m.insert("name".into(), Value::String(v.name.clone()));
                m.insert("class".into(), Value::String(v.class.as_str().into()));
                Value::Object(m)
            })
            .collect();

        let mut doc = Map::new();
        doc.insert("behavior".into(), Value::Object(behavior));
        doc.insert("node_types".into(), string_array(&self.vocabulary.node_types));
        doc.insert("edge_types".into(), string_array(&self.vocabulary.edge_types));
        doc.insert("environment".into(), Value::Array(env));

        if !self.foreign_context.is_empty() {
            let mut context = Map::new();
            for family in &self.foreign_context {
                let mut m = Map::new();
                if family.all_node_types {
                    m.insert("all_node_types".into(), Value::Bool(true));
                } else {
                    m.insert("node_types".into(), string_array(&family.node_types));
                }
                if !family.fields.is_empty() {
                    m.insert("node_fields".into(), string_array(&family.fields));
                }
                if !family.metadata_keys.is_empty() {
                    m.insert("metadata_keys".into(), string_array(&family.metadata_keys));
                }
                context.insert(family.family.clone(), Value::Object(m));
            }
            doc.insert("context".into(), Value::Object(context));
        }

        if !self.node_type_fields.is_empty() {
            let mut overrides = Map::new();
            for (node_type, fields) in &self.node_type_fields {
                let mut m = Map::new();
                m.insert("embed_fields".into(), string_array(fields));
                overrides.insert(node_type.clone(), Value::Object(m));
            }
            doc.insert("node_type_overrides".into(), Value::Object(overrides));
        }

        Ok(Value::Object(doc))
    }
}

fn string_array(items: &[String]) -> Value {
    Value::Array(items.iter().cloned().map(Value::String).collect())
}

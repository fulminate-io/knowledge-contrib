// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// declaration.go — the DECLARE side of the foreign-graph context: the shape a
// collector's config entry carries under its `context` key, as the Go value a
// module builds it from.
//
// IT IS THE CLIENT'S OWN SHAPE, AND THE SHAPE IS THE WHOLE POINT. context.go
// carries the READ side — what a walk receives — and states why it is a copy of
// the client's types rather than an import. The declare side owed the same copy
// and did not have one, so each module invented its own: a SLICE of structs
// carrying a `graph` key and a `reason` key. The client's loader decodes
// `context` into a MAP keyed by family and calls DisallowUnknownFields, so every
// entry rendered from those shapes was refused at load — `json: cannot unmarshal
// array into Go struct field Entry.context`, and `unknown field "reason"` once
// the array was fixed. A refusal is not scoped to the offending entry: the
// loader fails the whole scoped file, so one shipped entry took every other
// collector the operator had registered down with it.
//
// SO THE FAMILY IS THE MAP KEY AND NOT A FIELD, and Reason is a GO FIELD THAT IS
// NEVER RENDERED. The reason a module needs an entry is worth reading and worth
// reviewing, so it stays on the value where a reviewer and a source-reading
// census can both see it; it is tagged `json:"-"` because the client has no
// field for it and refuses one by name.
//
// A DECLARED NODE TYPE IS A STRING THE PRODUCING COLLECTOR EMITS, spelled the
// way that collector spells it. The client's fill filters by EXACT STRING
// EQUALITY and issues one node browse per declared type, so there is no prefix,
// glob or family form: a type string that is nearly right selects exactly
// nothing, with no error anywhere, which is the defect this shape's arrival
// fixed in four modules at once.

// ForeignFamilyDeclaration is what one module declares it needs from one graph
// family. It is the client's FamilyDeclaration, field for field and tag for tag.
//
// AN EMPTY DECLARATION IS A REAL ONE AND NOT A DEGENERATE ONE: a family declared
// with no node types yields the graph NAMES and no nodes, which is the whole
// input a membership test over graph names needs. Asking for node fields,
// metadata keys or edge fields WITHOUT node types is a different thing and the
// client refuses it by name, because those fields would be carried on nodes that
// never enter the slice.
type ForeignFamilyDeclaration struct {
	// NodeTypes selects which node types enter the slice at all, by exact string
	// equality against the producing collector's own node type. Empty means no
	// nodes — the graph names alone — unless AllNodeTypes is set.
	NodeTypes []string `json:"node_types,omitempty"`
	// AllNodeTypes selects EVERY node of the family, whatever its type.
	//
	// IT IS ITS OWN FIELD RATHER THAN A REDEFINITION OF THE EMPTY LIST, because
	// the empty list already MEANS something: the graph names and no nodes, which
	// is the whole input a membership test over names needs. Reading it as "every
	// type" would silently turn every such declaration into a full drain of every
	// graph of the family.
	//
	// WHAT IT IS FOR: a family whose type vocabulary is open or too large to
	// name. A provider that emits one node type per resource kind cannot be
	// selected by a list a consuming module maintains — the list rots the day
	// that provider adds a resource, and a stale name selects nothing with no
	// error, which is the failure the declarations in this tree kept having.
	//
	// SETTING IT BESIDE A NON-EMPTY NodeTypes IS REFUSED BY THE CLIENT, by name:
	// the two say different things, and silently preferring one is the ambiguity
	// this spelling exists to remove.
	AllNodeTypes bool `json:"all_node_types,omitempty"`
	// NodeFields names the typed node fields carried on each node. `id` is
	// load-bearing wherever EdgeFields is declared: the edge read is pivoted on
	// the carried nodes' ids, so a declaration without it receives zero edges and
	// no error, which is why the client refuses that pair.
	NodeFields []string `json:"node_fields,omitempty"`
	// MetadataKeys names the metadata keys carried on each node. A key the node
	// does not hold is omitted rather than carried empty.
	MetadataKeys []string `json:"metadata_keys,omitempty"`
	// EdgeFields names the edge fields carried on each edge. Empty means no
	// edges, and no edges means no correlation this module can confirm.
	EdgeFields []string `json:"edge_fields,omitempty"`
	// PathBasenames narrows the code family's nodes to those whose file path has
	// one of these basenames. The client refuses it on any other family.
	PathBasenames []string `json:"path_basenames,omitempty"`
	// Reason records WHY this module needs the entry, so a reviewer reading an
	// operator's config can tell a necessary declaration from a broad one.
	//
	// IT IS NEVER RENDERED. The client's entry decoder has no field for it and
	// refuses an unknown key by name, taking the whole config file with it. The
	// text belongs in the module's README beside the block instead, and stays
	// here so the value carries its own justification in source.
	Reason string `json:"-"`
}

// ForeignContextDeclaration is a module's whole declaration, keyed by graph
// family: `code`, or a graph type name an operator registered a collector under.
//
// A FAMILY NAMES A COLLECTOR THE OPERATOR INSTALLED. The client supplies `code`
// plus the graph types registered on that daemon and REFUSES a family it cannot
// supply, naming it and listing the set it can — so an operator running fewer
// providers than an example declares deletes the entries they have no collector
// for rather than receiving an empty arm for them.
type ForeignContextDeclaration map[string]ForeignFamilyDeclaration

// Families returns the declared family names, sorted, so a renderer or a
// diagnostic reports them in one order across runs. A map's iteration order is
// randomized and a declaration is small, so the sort costs nothing worth
// measuring and removes a whole class of run-to-run difference.
func (d ForeignContextDeclaration) Families() []string {
	out := make([]string, 0, len(d))
	for family := range d {
		out = append(out, family)
	}
	slices.Sort(out)
	return out
}

// declaredEnvNamePattern is the shape an environment name must have. It is the
// installer's own rule (it refuses a table row whose name carries a character
// outside this set), checked here so a declaration that could never be installed
// fails the collector's own suite rather than an operator's install.
var declaredEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// The four environment classes. The class decides what an installer WRITES:
//
//   - [EnvClassPath] a literal from the installing shell, so a provider's
//     file-based credential chain resolves.
//   - [EnvClassSelector] a literal when set; the key is omitted entirely when
//     unset, because an empty selector selects the thing named by "".
//   - [EnvClassSecret] NOTHING, in any state — not the value and not a
//     reference to it, because a reference is expanded by the process serving
//     the collect and reaches the collector present-and-empty.
//   - [EnvClassNotCarried] NOTHING either, for a different reason: the collector
//     reads the name and an installed entry deliberately does not declare it —
//     a machine fact, a platform the installer does not target, or a non-default
//     deployment an operator configures by hand. Declare these rather than
//     omitting them: an operator whose variable is missing from the entry can
//     then tell a decision from an oversight.
const (
	EnvClassPath       = "path"
	EnvClassSelector   = "selector"
	EnvClassSecret     = "secret"
	EnvClassNotCarried = "not-carried"
)

// envClasses is the closed vocabulary, sorted so a refusal lists it stably.
var envClasses = []string{EnvClassNotCarried, EnvClassPath, EnvClassSecret, EnvClassSelector}

// Declaration is the whole document the describe tool returns.
//
// EVERY FIELD IS REQUIRED TO BE DELIBERATE. NodeTypes and EdgeTypes are the
// vocabularies the server refuses undeclared types against, so a collector that
// emits a type it did not declare fails its own collect by name — which is the
// point, and is why they are validated against nothing here: only the collector
// knows its vocabulary.
type Declaration struct {
	// Behavior is the graph-level behavior this collector suggests. All three
	// booleans are pointers and all three must be set: an omitted one is refused
	// rather than defaulted, because a silent false and a declared false are
	// different statements about a collector.
	Behavior BehaviorDeclaration `json:"behavior"`
	// NodeTypeOverrides is the per-node-type half of the cascade, keyed by node
	// type. Omit it when the graph-level behavior is the whole story.
	NodeTypeOverrides map[string]NodeTypeOverrideDeclaration `json:"node_type_overrides,omitempty"`
	// NodeTypes is every node type this collector emits.
	NodeTypes []string `json:"node_types"`
	// EdgeTypes is every edge type this collector emits. A collector that emits
	// no edges declares an empty slice.
	EdgeTypes []string `json:"edge_types"`
	// Environment is every environment variable this collector reads, each with
	// its class. NAMES ONLY: there is no value field and there must never be one.
	Environment []EnvDeclaration `json:"environment"`
	// Context is the foreign-graph context this collector needs, keyed by graph
	// family. The type is the DECLARE-side shape this file already carries, so a
	// module builds one value and both serves it on describe and renders it into
	// its own worked entry.
	Context ForeignContextDeclaration `json:"context,omitempty"`
}

// BehaviorDeclaration is the behavior half. The three booleans are POINTERS so
// "declared false" stays distinguishable from "never said", and [Declaration]
// refuses a document that never said.
type BehaviorDeclaration struct {
	// Summarizable is a SUGGESTION the add prints and does not apply.
	Summarizable *bool `json:"summarizable"`
	// Embeddable is a SUGGESTION, on the same terms.
	Embeddable *bool `json:"embeddable"`
	// Syncable costs no LLM call and is written into the entry as declared.
	Syncable *bool `json:"syncable"`
	// The three field lists name the node fields and metadata keys each text is
	// composed from, in order. An empty list means the client's own defaults.
	EmbedFields     []string `json:"embed_fields,omitempty"`
	SummarizeFields []string `json:"summarize_fields,omitempty"`
	Bm25Fields      []string `json:"bm25_fields,omitempty"`
}

// NodeTypeOverrideDeclaration is one node type's override of the graph-level
// behavior. An unset key means inherit.
type NodeTypeOverrideDeclaration struct {
	Summarizable    *bool    `json:"summarizable,omitempty"`
	Embeddable      *bool    `json:"embeddable,omitempty"`
	EmbedFields     []string `json:"embed_fields,omitempty"`
	SummarizeFields []string `json:"summarize_fields,omitempty"`
	Bm25Fields      []string `json:"bm25_fields,omitempty"`
}

// EnvDeclaration is one environment variable a collector reads.
type EnvDeclaration struct {
	// Name is the variable name.
	Name string `json:"name"`
	// Class is one of the three constants above.
	Class string `json:"class"`
	// Description is what this collector reads it for, shown by an installer.
	Description string `json:"description,omitempty"`
	// EmptySensitive declares that this collector tells the name PRESENT AND
	// EMPTY apart from ABSENT: it refuses such a value, branches on the name's
	// presence, or hands it to a dependency that does either.
	//
	// ABSENT MEANS FALSE, and a collector that marks nothing renders exactly what
	// it rendered before this field existed.
	//
	// WHAT IT GOVERNS IS A DOCUMENT, NOT AN ENTRY. A worked entry that shows
	// `${NAME:-}` hands the child that name present and empty, because the
	// reference is expanded by the process serving the collect and resolves to
	// the empty string when that process does not hold the name. For a collector
	// that treats empty as absent this is inert and the defaulted form is a
	// perfectly good example; for a marked name it is a broken collect, so the
	// installer's documentation gate refuses the defaulted form for marked names
	// and admits it for the rest. Only the collector knows which it is, which is
	// why the answer is declared here and derived from here rather than
	// transcribed into the gate.
	//
	// IT IS LEGAL ON EVERY CLASS, [EnvClassNotCarried] INCLUDED, and that pairing
	// is the reason it does not ride the class table: that table drops the
	// not-carried class by construction, and a name a collector reads but no
	// entry declares can still appear in a worked entry someone copies.
	EmptySensitive bool `json:"empty_sensitive,omitempty"`
}

// validate refuses a declaration this framework will not serve, naming the value
// that is wrong with it.
//
// IT RUNS WHERE THE COLLECTOR IS BUILT, not where it is called: NewServer
// refuses, so a collector whose declaration is malformed fails on its first line
// of output rather than on an operator's install. The rules are the ones the
// client's own gate would apply, checked on this side so the author sees them in
// their own test run.
func (d Declaration) validate() error {
	if d.Behavior.Summarizable == nil || d.Behavior.Embeddable == nil || d.Behavior.Syncable == nil {
		return fmt.Errorf(
			"framework: the declaration's behavior must set summarizable, embeddable and syncable explicitly; " +
				"an omitted one is not a default, it is a collector that never said (the two LLM axes are a suggestion the operator's flags override)")
	}
	if d.NodeTypes == nil {
		return fmt.Errorf(
			"framework: the declaration names no node_types; declare every node type this collector emits, " +
				"or an empty slice to declare that it emits none — a collect carrying an undeclared type is refused at ingest by name")
	}
	if d.EdgeTypes == nil {
		return fmt.Errorf(
			"framework: the declaration names no edge_types; declare every edge type this collector emits, or an empty slice")
	}
	if d.Environment == nil {
		return fmt.Errorf(
			"framework: the declaration names no environment; declare every variable this collector reads with its class, or an empty slice")
	}
	if err := d.validateVocabulary(); err != nil {
		return err
	}
	return d.validateEnvironment()
}

// validateVocabulary refuses an empty or duplicated type name. A blank entry
// would reach the registration record and refuse every node at ingest with a
// message naming nothing.
func (d Declaration) validateVocabulary() error {
	for _, pair := range []struct {
		label string
		types []string
	}{{"node_types", d.NodeTypes}, {"edge_types", d.EdgeTypes}} {
		seen := make(map[string]struct{}, len(pair.types))
		for _, t := range pair.types {
			if strings.TrimSpace(t) == "" {
				return fmt.Errorf("framework: the declaration's %s carries an empty type name", pair.label)
			}
			if _, dup := seen[t]; dup {
				return fmt.Errorf("framework: the declaration's %s names %q twice", pair.label, t)
			}
			seen[t] = struct{}{}
		}
	}
	return nil
}

// validateEnvironment refuses a name an installer could not write and a class
// outside the closed vocabulary.
func (d Declaration) validateEnvironment() error {
	seen := make(map[string]struct{}, len(d.Environment))
	for i, e := range d.Environment {
		if !declaredEnvNamePattern.MatchString(e.Name) {
			return fmt.Errorf(
				"framework: the declaration's environment[%d] name %q is not an environment variable name; "+
					"a name matches [A-Za-z_][A-Za-z0-9_]* and an installer refuses anything else", i, e.Name)
		}
		if !slices.Contains(envClasses, e.Class) {
			return fmt.Errorf(
				"framework: the declaration's environment name %q carries the class %q; the classes are %s",
				e.Name, e.Class, strings.Join(envClasses, ", "))
		}
		if _, dup := seen[e.Name]; dup {
			return fmt.Errorf("framework: the declaration's environment names %q twice; one name has one class", e.Name)
		}
		seen[e.Name] = struct{}{}
	}
	return nil
}

// EnvTable renders the declared environment as the installer's own line format:
// one row per name, `class name`, sorted by name within class order so one
// unchanged declaration produces one byte-identical table.
//
// THE FORMAT IS THE INSTALLER'S CONTRACT, not a convenience: install.sh reads
// these rows with `while read -r class name`, which is why the two fields are
// whitespace-separated and why nothing else may appear on a row.
func (d Declaration) EnvTable() []string {
	rows := make([]string, 0, len(d.Environment))
	// THE not-carried CLASS IS ABSENT FROM THIS TABLE BY CONSTRUCTION. The three
	// classes below are the installer's own closed vocabulary and an unknown
	// token fails its case arm; a name the collector reads but no entry carries
	// contributes no row rather than a fourth token the script would refuse.
	for _, class := range []string{EnvClassPath, EnvClassSelector, EnvClassSecret} {
		names := make([]string, 0, len(d.Environment))
		for _, e := range d.Environment {
			if e.Class == class {
				names = append(names, e.Name)
			}
		}
		slices.Sort(names)
		for _, n := range names {
			rows = append(rows, class+" "+n)
		}
	}
	return rows
}

// EmptySensitiveNames returns every declared name marked [EnvDeclaration].EmptySensitive,
// sorted, one per line in the installer's own line format — which here is one
// bare name, because the question the installer asks is a membership question.
//
// EVERY CLASS IS ELIGIBLE, which is what distinguishes this answer from
// [Declaration.EnvTable]. EnvTable drops [EnvClassNotCarried] by construction
// because install.sh's entry-writing case arm knows three tokens and fails a
// fourth by name; this answer feeds a DOCUMENTATION gate rather than an entry
// writer, and a name a collector reads but no entry declares can still appear in
// a worked entry an operator copies.
//
// AN EMPTY ANSWER IS THE CORRECT ANSWER for a collector that treats every name's
// empty value as absent, and most do. Nothing downstream may treat emptiness
// here as a broken reader the way it does for the class table — a table that
// came back empty means a collector that reads no environment at all, which
// cannot be true; a mark list that came back empty means a collector that
// discriminates on nothing, which is the common case.
//
// SORTED for the same reason EnvTable is: the answer is baked into a generated
// shell function whose drift gate diffs it, so an unsorted answer would report a
// change nobody made on every regeneration.
func (d Declaration) EmptySensitiveNames() []string {
	names := make([]string, 0, len(d.Environment))
	for _, e := range d.Environment {
		if e.EmptySensitive {
			names = append(names, e.Name)
		}
	}
	slices.Sort(names)
	return names
}

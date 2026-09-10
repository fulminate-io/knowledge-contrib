// SPDX-License-Identifier: Apache-2.0

// Package installentry renders the WORKED CONFIG ENTRY this collector ships in
// its documentation, in the format the client actually reads.
//
// THE ENTRY IS THE REGISTRATION RECORD. There is no registration call: a
// collector is installed by writing an entry into a JSON config file, and an
// entry removed is a collector gone at the next lookup. That makes the worked
// entry in the README a load-bearing artifact rather than an illustration — an
// operator copies it, and a key it gets wrong is a collector that does not load.
//
// WHY THE SHAPE IS DECLARED HERE RATHER THAN IMPORTED. The client's own record
// lives in an internal package of another module, which this one cannot import
// and must not require. So the shape is declared with the same key names and the
// decode below is STRICT, refusing any key outside them — the same posture the
// loader takes. What that buys is that the documented entry cannot drift from
// this declaration. What it cannot prove is that the far side still reads this
// shape; only installing the collector shows that, which is the live
// confirmation's job and is said here rather than implied.
//
// THIS COLLECTOR'S ENTRY CARRIES AN ENVIRONMENT BLOCK OF AT MOST ONE KEY. Its
// closure is three names: two spellings of a credential, which an installer
// writes in NO state, and one SELECTOR naming the instance. So the block holds
// the selector or it is absent entirely — and an operator on the hosted instance
// has no block at all.
package installentry

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/enventry"
)

// FileName is the basename of the config file, and the two scopes it lives in.
// The name is the same in both; the directory differs.
const (
	FileName      = "collectors.json"
	UserScopePath = "~/.knowledge/" + FileName
	// ProjectScopePath is relative to a repository root.
	ProjectScopePath = ".knowledge/" + FileName
)

// FamilyName is the entry's key, which IS the graph family the results land in.
//
// IT IS NOT `cicd`. That name is a BUILT-IN graph type and the client refuses to
// register it, so a collector could not claim it however it was spelled.
//
// AND IT IS NOT THE BARE PROVIDER NAME EITHER. A family registered under the
// provider's own one-word name collides with a compiled-in collector of that
// name, and the client's post-collect linker is keyed on exactly those names — so
// such a registration would have a linker run over a graph whose data it was
// never written for. This module names nothing else and attempts nothing else.
const FamilyName = "gitlab-ci"

// ToolName is the single MCP tool this collector serves. It must match what the
// binary advertises or the install dial fails.
const ToolName = "collect"

// commandPlaceholder is where the built binary lives. It is a placeholder like
// every other value here: an operator's path is their own. The basename is the
// one the published archive carries, so an operator who installed from a release
// and one who built from source write the same entry.
const commandPlaceholder = "/usr/local/bin/knowledge-collector-gitlab-ci"

// selfHostedPlaceholder is the instance the worked entry points at.
//
// IT IS A DOCUMENTATION PLACEHOLDER AND THE BLOCK IS OPTIONAL. An operator on the
// hosted instance deletes the whole environment block; one running their own
// replaces this host. It is shown rather than omitted because the selector is the
// one thing about this collector's entry a reader cannot guess, and a worked
// entry that never showed it would leave every self-hosted operator to find out
// by failing.
const selfHostedPlaceholder = "https://gitlab.example.com/"

// File is the whole config file: one object of named collectors.
type File struct {
	Collectors map[string]Entry `json:"collectors"`
}

// Entry is one registered collector. The key names mirror the client's own
// record exactly; see the package comment for why they are declared rather than
// imported.
type Entry struct {
	Type      string                      `json:"type"`
	Command   string                      `json:"command,omitempty"`
	Args      []string                    `json:"args,omitempty"`
	Env       map[string]string           `json:"env,omitempty"`
	URL       string                      `json:"url,omitempty"`
	Headers   map[string]string           `json:"headers,omitempty"`
	Tool      string                      `json:"tool"`
	Behavior  *Behavior                   `json:"behavior,omitempty"`
	NodeTypes map[string]NodeTypeOverride `json:"node_types,omitempty"`
}

// Behavior is the graph-level declaration that decides what the indexing
// pipeline does with this collector's graph.
//
// THE THREE BOOLEANS ARE POINTERS because an omitted key and an explicit false
// are different statements, and the loader treats them differently.
type Behavior struct {
	Syncable        *bool             `json:"syncable,omitempty"`
	Summarizable    *bool             `json:"summarizable,omitempty"`
	Embeddable      *bool             `json:"embeddable,omitempty"`
	EmbedFields     []string          `json:"embed_fields,omitempty"`
	SummarizeFields []string          `json:"summarize_fields,omitempty"`
	Bm25Fields      []string          `json:"bm25_fields,omitempty"`
	Extra           map[string]string `json:"extra,omitempty"`
}

// NodeTypeOverride is the per-node-type half of the same declaration. This
// collector ships none: it emits ONE node type with the kind in metadata, so
// there is nothing to override per type.
type NodeTypeOverride struct {
	Summarizable    *bool    `json:"summarizable,omitempty"`
	Embeddable      *bool    `json:"embeddable,omitempty"`
	EmbedFields     []string `json:"embed_fields,omitempty"`
	SummarizeFields []string `json:"summarize_fields,omitempty"`
	Bm25Fields      []string `json:"bm25_fields,omitempty"`
}

// NodeFieldsThisCollectorPopulates names the node fields this collector actually
// writes. The behavior block's three lists are checked against it, because a
// field list naming something these nodes never carry points the pipeline at
// nothing and reads as a declaration that works.
//
// It is deliberately NOT every field the contract defines: a CI/CD resource has
// no file path, no line range and no language, and listing those would make the
// check that reads this vacuous.
func NodeFieldsThisCollectorPopulates() []string {
	return []string{"symbol_name", "summary", "content", "source", "metadata"}
}

// Worked builds the entry this collector's documentation ships.
func Worked() File {
	enabled := true
	return File{Collectors: map[string]Entry{
		FamilyName: {
			Type:    "stdio",
			Command: commandPlaceholder,
			Tool:    ToolName,
			// THE SELECTOR AS A LITERAL, THE TOKEN AS A REFERENCE. The selector
			// names a host and is written out; the credential's VALUE belongs in
			// no file, so the entry names the variable and the process serving the
			// collect reads it from the operator's own environment at spawn.
			//
			// ONLY THE PRIMARY TOKEN NAME. The second is the FALLBACK, consulted
			// when the first is unset, so referencing both would demand that an
			// operator hold two variables to authenticate once. The installer
			// writes whichever of them the installing shell holds.
			Env: map[string]string{
				enventry.BaseURLVariable:      selfHostedPlaceholder,
				enventry.PrimaryTokenVariable: "${" + enventry.PrimaryTokenVariable + "}",
			},
			Behavior: &Behavior{
				// All three DECLARED rather than left to the default. Two of them
				// default to FALSE, so an entry with no block is collected and
				// walkable and never summarized or embedded; and a declaration an
				// operator can see and change is the point — this is the block
				// they edit to turn one off, and a block that is not there is one
				// they cannot find.
				Syncable:     &enabled,
				Summarizable: &enabled,
				Embeddable:   &enabled,

				// The three field lists have NO default at all. A graph whose
				// lists are empty is collected, readable by id and walkable, and
				// carries nothing worth matching into the text index.
				//
				// EMBED: the one-line summary and the resource's own name. The
				// content is a JSON document or a YAML pipeline definition, and
				// embedding one spends the vector on its punctuation.
				EmbedFields: []string{"summary", "symbol_name"},
				// SUMMARIZE: the content is what a summarizer needs — the
				// resource's actual configuration — with the name and the
				// existing summary for context.
				SummarizeFields: []string{"content", "symbol_name", "summary"},
				// BM25: all three. A keyword search for a project path, a variable
				// name or a runner tag finds it in the content and nowhere else,
				// which is what an operator searches a CI/CD graph for.
				Bm25Fields: []string{"symbol_name", "summary", "content"},
			},
		},
	}}
}

// Render serializes the worked entry as the document an operator copies.
func Render() (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	// The document is read by a person before it is read by a parser, so the
	// characters an operator's own values may contain are left alone rather than
	// escaped into an unreadable form.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(Worked()); err != nil {
		return "", fmt.Errorf("rendering the worked config entry: %w", err)
	}
	return buf.String(), nil
}

// Decode parses a config document STRICTLY, refusing any key outside the
// declared set — which is the loader's own posture, and the only way this
// module's documentation test asserts against the format rather than against its
// own generator.
func Decode(raw []byte) (File, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var file File
	if err := decoder.Decode(&file); err != nil {
		return File{}, fmt.Errorf("decoding a collector config document: %w", err)
	}
	return file, nil
}

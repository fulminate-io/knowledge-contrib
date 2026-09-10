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
package installentry

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/enventry"
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
const FamilyName = "gcp"

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
// collector ships none: its node type IS the resource type, so there are 49 of
// them and they all want the same treatment. A per-type override here would be
// 49 copies of the graph-level answer.
type NodeTypeOverride struct {
	Summarizable    *bool    `json:"summarizable,omitempty"`
	Embeddable      *bool    `json:"embeddable,omitempty"`
	EmbedFields     []string `json:"embed_fields,omitempty"`
	SummarizeFields []string `json:"summarize_fields,omitempty"`
	Bm25Fields      []string `json:"bm25_fields,omitempty"`
}

// commandPlaceholder is where the built binary lives. It is a placeholder like
// every other value here: an operator's path is their own.
const commandPlaceholder = "<HOME>/.knowledge/bin/knowledge-collector-gcp"

// toolName is the single MCP tool this collector serves, and it must match what
// the binary advertises or the install dial fails.
const toolName = "collect"

// NodeFieldsThisCollectorPopulates names the node fields this collector actually
// writes. The behavior block's three lists are checked against it, because a
// field list naming something these nodes never carry points the pipeline at
// nothing and reads as a declaration that works.
//
// It is deliberately NOT every field the contract defines: a cloud resource has
// no file path, no line range and no language, and listing those would make the
// check that reads this vacuous.
func NodeFieldsThisCollectorPopulates() []string {
	return []string{"symbol_name", "summary", "content", "source", "metadata"}
}

// Worked builds the entry this collector's documentation ships, for the named
// target operating system.
func Worked(goos string) File {
	enabled := true
	return File{Collectors: map[string]Entry{
		FamilyName: {
			Type:    "stdio",
			Command: commandPlaceholder,
			Tool:    toolName,
			Env:     envPlaceholders(goos),
			Behavior: &Behavior{
				// All three DECLARED rather than left to the default. The
				// default is the same, and a declaration an operator can see and
				// change is the point: this is the block they edit to turn one
				// off, and a block that is not there is one they cannot find.
				Syncable:     &enabled,
				Summarizable: &enabled,
				Embeddable:   &enabled,

				// The three field lists have NO default at all. A graph whose
				// lists are empty is collected, readable by id and walkable, and
				// carries nothing into the text index worth matching — which is
				// the outcome nobody installs a collector for.
				//
				// EMBED: the one-line summary and the resource's own name. The
				// content is a JSON document, and embedding a JSON document
				// spends the vector on its punctuation.
				EmbedFields: []string{"summary", "symbol_name"},
				// SUMMARIZE: the content is what a summarizer needs — the
				// resource's actual configuration — with the name and the
				// existing summary for context.
				SummarizeFields: []string{"content", "symbol_name", "summary"},
				// BM25: all three. A keyword search for an address, a label
				// value or a machine type finds it in the content and nowhere
				// else, which is exactly what an operator searches a cloud graph
				// for.
				Bm25Fields: []string{"symbol_name", "summary", "content"},
			},
		},
	}}
}

// Render serializes the worked entry as the document an operator copies.
func Render(goos string) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	// The document is read by a person before it is read by a parser, so the
	// characters an operator's own values may contain are left alone rather than
	// escaped into an unreadable form.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(Worked(goos)); err != nil {
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

// envPlaceholders is the environment block: every name the credential chain
// reads on this target, each with a placeholder for the operator's own value.
func envPlaceholders(goos string) map[string]string {
	names := enventry.Names(goos)
	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = "<" + name + ">"
	}
	return out
}

// SPDX-License-Identifier: Apache-2.0

package installentry_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/installentry"
)

// installentry_test.go — the worked entry, and the one thing about it that is
// unlike every other collector's.

// TestTheWorkedEntryCarriesTheTokenAsAReferenceAndNeverAsAValue is this
// collector's own row, and it is an assertion about the ENCODED document rather
// than about the Go value: a nil map and an empty one are the same Go value to a
// reader, and only one of them marshals away.
//
// BOTH NAMES IN THIS COLLECTOR'S CLOSURE ARE CREDENTIALS, so the block an
// installer writes carries no literal at all — the primary name references
// itself and the operator's own environment supplies the value at spawn. The two
// shapes this row rules out are the ones that fail silently: a VALUE, which puts
// the credential in a file, and a `${NAME:-}` DEFAULT, which resolves to the
// empty string in a serving process that does not hold the name and hands this
// collector a token that is present and empty.
func TestTheWorkedEntryCarriesTheTokenAsAReferenceAndNeverAsAValue(t *testing.T) {
	rendered, err := installentry.Render()
	if err != nil {
		t.Fatalf("rendering the worked entry: %v", err)
	}
	want := `"` + enventry.PrimaryTokenVariable + `": "${` + enventry.PrimaryTokenVariable + `}"`
	if !strings.Contains(rendered, want) {
		t.Errorf("the worked entry does not carry %s, so an operator copying it installs a "+
			"collector with no way to receive its token:\n%s", want, rendered)
	}
	if strings.Contains(rendered, ":-") {
		t.Errorf("the worked entry carries a DEFAULTED reference, which resolves to the empty "+
			"string in a serving process that does not hold the name:\n%s", rendered)
	}
	// THE SECOND NAME IS NOT WRITTEN. It is the FALLBACK, consulted only when the
	// primary is unset, so an entry naming both would refuse every collect on a
	// machine holding one of the two.
	if strings.Contains(rendered, enventry.FallbackTokenVariable) {
		t.Errorf("the worked entry names the fallback variable %q as well as the primary; the two "+
			"are alternatives, and referencing both demands the operator hold both:\n%s",
			enventry.FallbackTokenVariable, rendered)
	}

	// The known positive for the matcher: the same document DOES carry the keys
	// an entry is required to have, so this is a document with content rather
	// than an empty one.
	for _, want := range []string{`"type"`, `"command"`, `"tool"`, `"behavior"`} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the worked entry does not carry %s:\n%s", want, rendered)
		}
	}
}

// TestTheWorkedEntryIsKeyedByTheFamilyAndNotByTheBuiltInName.
func TestTheWorkedEntryIsKeyedByTheFamilyAndNotByTheBuiltInName(t *testing.T) {
	file := installentry.Worked()
	if _, ok := file.Collectors["github-actions"]; !ok {
		t.Errorf("the worked entry is not keyed by %q: %v", "github-actions", file.Collectors)
	}
	// `cicd` is a BUILT-IN graph type: the client refuses to register it, so a
	// collector cannot claim the name however it is spelled. This module names
	// nothing else and attempts nothing else.
	if _, ok := file.Collectors["cicd"]; ok {
		t.Error("the worked entry claims the built-in graph type name")
	}
	if len(file.Collectors) != 1 {
		t.Errorf("the worked entry declares %d collectors, want 1", len(file.Collectors))
	}
}

// TestTheWorkedEntryDeclaresBehaviorWithItsThreeFieldLists is R2's behavior
// clause: an entry with no block gets syncable true and the two LLM axes FALSE,
// so a graph registered without it is collected and walkable and never
// summarized or embedded.
func TestTheWorkedEntryDeclaresBehaviorWithItsThreeFieldLists(t *testing.T) {
	behavior := installentry.Worked().Collectors[installentry.FamilyName].Behavior
	if behavior == nil {
		t.Fatal("the worked entry carries no behavior block")
	}
	for name, value := range map[string]*bool{
		"syncable":     behavior.Syncable,
		"summarizable": behavior.Summarizable,
		"embeddable":   behavior.Embeddable,
	} {
		if value == nil || !*value {
			t.Errorf("the behavior block does not declare %s true", name)
		}
	}
	for name, list := range map[string][]string{
		"embed_fields":     behavior.EmbedFields,
		"summarize_fields": behavior.SummarizeFields,
		"bm25_fields":      behavior.Bm25Fields,
	} {
		if len(list) == 0 {
			t.Errorf("the behavior block leaves %s empty; nothing defaults it", name)
		}
		for _, field := range list {
			if !slices.Contains(installentry.NodeFieldsThisCollectorPopulates(), field) {
				t.Errorf("%s names %q, which this collector's nodes do not carry", name, field)
			}
		}
	}
}

// TestTheEntryDecodesThroughTheStrictShape, and a key outside the declared set is
// refused — which is the loader's own posture and the only reason the README gate
// asserts about the FORMAT rather than about this package's own generator.
func TestTheEntryDecodesThroughTheStrictShape(t *testing.T) {
	rendered, err := installentry.Render()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if _, err := installentry.Decode([]byte(rendered)); err != nil {
		t.Fatalf("this package's own rendering does not decode through its own strict shape: %v", err)
	}

	// THE CONTROL for the strictness: a key the loader does not declare must be
	// refused. Without it, a decode that ignored unknown keys would accept
	// anything and every assertion resting on it would prove nothing.
	if _, err := installentry.Decode([]byte(
		`{"collectors":{"github-actions":{"type":"stdio","tool":"collect","invented_key":true}}}`,
	)); err == nil {
		t.Fatal("the decode accepts a key the loader does not declare")
	}
}

// TestTheEntryNamesTheToolTheBinaryServes. A wrong pair here writes an entry that
// dials a provider which does not answer to it, and the failure surfaces at the
// operator's first collect rather than at install.
func TestTheEntryNamesTheToolTheBinaryServes(t *testing.T) {
	entry := installentry.Worked().Collectors[installentry.FamilyName]
	if entry.Tool != "collect" {
		t.Errorf("the entry names the tool %q; the binary serves the framework's default, "+
			"%q", entry.Tool, "collect")
	}
	if entry.Type != "stdio" {
		t.Errorf("the entry's transport is %q, want stdio", entry.Type)
	}
	if !strings.HasSuffix(entry.Command, "knowledge-collector-github-actions") {
		t.Errorf("the entry's command is %q; the published archive carries a binary called "+
			"knowledge-collector-github-actions, so an operator who installed from a release and "+
			"one who built from source must write the same entry", entry.Command)
	}
}

// TestTheRenderedDocumentIsTheJSONTheLoaderReads. The config file is JSON and the
// loader parses nothing else; a worked example in another serialization is the
// same failure as one with the wrong keys.
func TestTheRenderedDocumentIsTheJSONTheLoaderReads(t *testing.T) {
	rendered, err := installentry.Render()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	var any map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rendered), &any); err != nil {
		t.Fatalf("the rendered document is not JSON: %v", err)
	}
	if _, ok := any["collectors"]; !ok {
		t.Errorf("the rendered document has no top-level collectors object: %s", rendered)
	}
}

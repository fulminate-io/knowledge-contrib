// SPDX-License-Identifier: Apache-2.0

package installentry_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/installentry"
)

// installentry_test.go — the worked entry, and the one thing about it that is
// unlike every other collector's: it is the first MIXED-CLASS entry.

// TestTheWorkedEntryCarriesBothCredentialsAsReferencesAndNoValue is this
// collector's own row, and it is an assertion about the ENCODED document as well
// as the Go value: a nil map and an empty one are the same Go value to a reader,
// and only one of them marshals away.
//
// TWO OF THE THREE NAMES ARE CREDENTIALS, and both are written as bare
// references to their own names — this collector requires the pair together, so
// an entry referencing one alone documents a collector that cannot authenticate.
// A VALUE is what belongs nowhere in the file, and a `${NAME:-}` DEFAULT is
// worse than either: it is expanded by the process SERVING the collect, whose
// environment holds little, so it reaches this collector present and empty on
// every collect.
func TestTheWorkedEntryCarriesBothCredentialsAsReferencesAndNoValue(t *testing.T) {
	rendered, err := installentry.Render()
	if err != nil {
		t.Fatalf("rendering the worked entry: %v", err)
	}
	entry := installentry.Worked().Collectors[installentry.FamilyName]
	for name, class := range enventry.Classes() {
		if class != enventry.ClassSecret {
			continue
		}
		value, ok := entry.Env[name]
		if !ok {
			t.Errorf("the worked entry does not carry %q, so an operator copying it installs a "+
				"collector that cannot authenticate: both halves are required together:\n%s",
				name, rendered)
			continue
		}
		if want := "${" + name + "}"; value != want {
			t.Errorf("the worked entry sets %q to %q, want the bare reference %q", name, value, want)
		}
	}
	if strings.Contains(rendered, ":-") {
		t.Errorf("the worked entry carries a DEFAULTED reference, which reaches this collector "+
			"present and empty:\n%s", rendered)
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

// TestTheWorkedEntrysBlockCarriesTheCredentialsAndNotTheSelector.
//
// THE SELECTOR IS WHY THIS NEEDS SAYING. This collector's third name is a
// non-secret selector, and an installer writes it as a literal WHEN THE
// INSTALLING SHELL HAS IT SET. The worked entry documents the default
// installation, which does not, so the selector is ABSENT from the block — not
// present and empty, which this collector refuses by name.
func TestTheWorkedEntrysBlockCarriesTheCredentialsAndNotTheSelector(t *testing.T) {
	rendered, err := installentry.Render()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if !strings.Contains(rendered, `"env"`) {
		t.Errorf("the worked entry carries no env block, so it names neither credential:\n%s",
			rendered)
	}
	if strings.Contains(rendered, enventry.HistoryDepthVariable) {
		t.Errorf("the worked entry names the selector, which is written only when the installing "+
			"shell has it set:\n%s", rendered)
	}
}

// TestTheSelectorSetIsDerivedFromTheClasses rather than listed, so a name whose
// class changes cannot be left behind in a second list.
func TestTheSelectorSetIsDerivedFromTheClasses(t *testing.T) {
	selectors := installentry.SelectorNames()
	if !slices.Equal(selectors, []string{enventry.HistoryDepthVariable}) {
		t.Errorf("the selector set is %v, want exactly the one non-secret name", selectors)
	}
	for _, name := range selectors {
		if enventry.Classes()[name] != enventry.ClassSelector {
			t.Errorf("%q is in the selector set and is classed %q",
				name, enventry.Classes()[name])
		}
	}
	// The control: neither credential is in it, so the set is a filter that fired
	// rather than a copy of the whole closure.
	for _, credential := range []string{
		enventry.UsernameVariable, enventry.AppPasswordVariable,
	} {
		if slices.Contains(selectors, credential) {
			t.Errorf("the credential %q is in the set an installer may write", credential)
		}
	}
}

// TestTheWorkedEntryIsKeyedByTheFamilyAndNotByTheBuiltInName.
func TestTheWorkedEntryIsKeyedByTheFamilyAndNotByTheBuiltInName(t *testing.T) {
	file := installentry.Worked()
	if _, ok := file.Collectors[installentry.FamilyName]; !ok {
		t.Errorf("the worked entry is not keyed by %q: %v",
			installentry.FamilyName, file.Collectors)
	}
	if len(file.Collectors) != 1 {
		t.Errorf("the worked entry declares %d collectors, want 1", len(file.Collectors))
	}
}

// TestTheWorkedEntryDeclaresBehaviorWithItsThreeFieldLists is the behavior
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

// TestTheEntryDecodesThroughTheStrictShape, and a key outside the declared set
// is refused — which is the loader's own posture and the only reason the README
// gate asserts about the FORMAT rather than about this package's own generator.
func TestTheEntryDecodesThroughTheStrictShape(t *testing.T) {
	rendered, err := installentry.Render()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if _, err := installentry.Decode([]byte(rendered)); err != nil {
		t.Fatalf("this package's own rendering does not decode through its own strict shape: %v",
			err)
	}

	// THE CONTROL for the strictness: a key the loader does not declare must be
	// refused. Without it, a decode that ignored unknown keys would accept
	// anything and every assertion resting on it would prove nothing.
	if _, err := installentry.Decode([]byte(
		`{"collectors":{"bitbucket-pipelines":{"type":"stdio","tool":"collect","invented":true}}}`,
	)); err == nil {
		t.Fatal("the decode accepts a key the loader does not declare")
	}
}

// TestTheEntryNamesTheToolTheBinaryServes. A wrong pair here writes an entry
// that dials a provider which does not answer to it, and the failure surfaces at
// the operator's first collect rather than at install.
func TestTheEntryNamesTheToolTheBinaryServes(t *testing.T) {
	entry := installentry.Worked().Collectors[installentry.FamilyName]
	if entry.Tool != "collect" {
		t.Errorf("the entry names the tool %q; the binary serves the framework's default, %q",
			entry.Tool, "collect")
	}
	if entry.Type != "stdio" {
		t.Errorf("the entry's transport is %q, want stdio", entry.Type)
	}
	if !strings.HasSuffix(entry.Command, "knowledge-collector-bitbucket-pipelines") {
		t.Errorf("the entry's command is %q; the published archive carries a binary called "+
			"knowledge-collector-bitbucket-pipelines, so an operator who installed from a release "+
			"and one who built from source must write the same entry", entry.Command)
	}
}

// TestTheRenderedDocumentIsTheJSONTheLoaderReads. The config file is JSON and
// the loader parses nothing else; a worked example in another serialization is
// the same failure as one with the wrong keys.
func TestTheRenderedDocumentIsTheJSONTheLoaderReads(t *testing.T) {
	rendered, err := installentry.Render()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rendered), &document); err != nil {
		t.Fatalf("the rendered document is not JSON: %v", err)
	}
	if _, ok := document["collectors"]; !ok {
		t.Errorf("the rendered document has no top-level collectors object: %s", rendered)
	}
}

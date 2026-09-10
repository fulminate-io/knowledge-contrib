// SPDX-License-Identifier: Apache-2.0

package installentry_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/installentry"
)

// installentry_test.go — THE WORKED CONFIG ENTRY, asserted against the LOADER'S
// format rather than against its own generator.
//
// THE MISTAKE THIS FILE EXISTS TO NOT REPEAT. The first version of this module
// shipped an install section in one format and asserted it by checking the
// document contained its own generator's output. That proves the document and
// the generator agree and says nothing about whether either is what the consumer
// reads — and neither was: the entry was written in a different serialization
// from the one the client parses, and the test passed.
//
// So every assertion below is against an EXTERNAL expectation: the literal key
// names the loader declares, written out here, and a STRICT decode that refuses
// any key outside them. Neither is produced by the code under test.

// theLoaderKeys are the entry keys the client's own record declares, written out
// here rather than derived. The client's package is another module's internal
// package and cannot be imported; this list and the strict decode are what stand
// in for that import, and the live confirmation is what proves the far side
// still reads it.
var theLoaderKeys = []string{
	"type", "command", "args", "env", "url", "headers", "tool", "behavior", "node_types",
}

var theBehaviorKeys = []string{
	"syncable", "summarizable", "embeddable",
	"embed_fields", "summarize_fields", "bm25_fields", "extra",
}

func TestTheWorkedEntryDecodesStrictlyAgainstTheLoadersKeys(t *testing.T) {
	rendered, err := installentry.Render("linux")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// A STRICT decode into the loader's own shape: any key the loader does not
	// declare is refused, which is the loader's own posture.
	got, err := installentry.Decode([]byte(rendered))
	if err != nil {
		t.Fatalf("the worked entry does not decode against the loader's shape: %v", err)
	}
	entry, ok := got.Collectors["gcp"]
	if !ok {
		t.Fatalf("the worked entry is not keyed by the graph family: %v", got.Collectors)
	}
	if entry.Type != "stdio" {
		t.Errorf("entry type: got %q, want stdio", entry.Type)
	}
	if entry.Tool == "" {
		t.Error("the entry names no tool; the daemon would not know what to call")
	}
	if entry.Command == "" {
		t.Error("the entry names no command")
	}
}

// THE CONTROL for the strict decode: a key the loader does not declare must be
// REFUSED. Without it, a decode that ignored unknown keys would make the
// assertion above vacuous.
func TestTheStrictDecodeRefusesAKeyTheLoaderDoesNotDeclare(t *testing.T) {
	_, err := installentry.Decode([]byte(`{"collectors":{"gcp":{"type":"stdio","tool":"collect","invented":1}}}`))
	if err == nil {
		t.Fatal("a key outside the loader's declared set was accepted; the decode is not strict " +
			"and every assertion resting on it proves nothing")
	}
	if !strings.Contains(err.Error(), "invented") {
		t.Errorf("the refusal does not name the offending key: %v", err)
	}
}

// TestTheRenderedKeysAreTheLoadersKeys checks the SERIALIZED document rather
// than the Go value, because the document is what an operator copies and what
// the loader parses.
func TestTheRenderedKeysAreTheLoadersKeys(t *testing.T) {
	rendered, err := installentry.Render("linux")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var generic map[string]map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rendered), &generic); err != nil {
		t.Fatalf("the rendered entry is not the expected shape: %v", err)
	}
	entry, ok := generic["collectors"]["gcp"]
	if !ok {
		t.Fatalf("no gcp entry under collectors: %v", generic)
	}
	for key := range entry {
		if !slices.Contains(theLoaderKeys, key) {
			t.Errorf("the entry carries the key %q, which the loader does not declare", key)
		}
	}
}

// R2's BEHAVIOR CLAUSE. The ticket requires the collector to declare behavior so
// its graph is summarized, embedded and syncable. An absent block defaults the
// three BOOLEANS true, and defaults none of the three FIELD LISTS — a graph with
// no field lists is collected and walkable and carries nothing into the text
// index worth matching. So all six are named explicitly.
func TestTheEntryDeclaresBehaviorWithAllThreeFieldLists(t *testing.T) {
	rendered, err := installentry.Render("linux")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got, err := installentry.Decode([]byte(rendered))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	behavior := got.Collectors["gcp"].Behavior
	if behavior == nil {
		t.Fatal("the worked entry carries no behavior block, so nothing declares this graph " +
			"summarizable, embeddable or syncable and no field list names what is indexed")
	}
	for name, value := range map[string]*bool{
		"syncable":     behavior.Syncable,
		"summarizable": behavior.Summarizable,
		"embeddable":   behavior.Embeddable,
	} {
		if value == nil {
			t.Errorf("behavior.%s is absent; an absent flag defaults true, but the requirement is "+
				"that the entry DECLARES it", name)
			continue
		}
		if !*value {
			t.Errorf("behavior.%s is declared false", name)
		}
	}
	for name, list := range map[string][]string{
		"embed_fields":     behavior.EmbedFields,
		"summarize_fields": behavior.SummarizeFields,
		"bm25_fields":      behavior.Bm25Fields,
	} {
		if len(list) == 0 {
			t.Errorf("behavior.%s is empty; nothing defaults it, so the pipeline has no text to "+
				"work from for this graph", name)
		}
	}
}

// Every field the behavior block names must be a field this collector's nodes
// actually carry, or the pipeline is pointed at nothing.
func TestEveryBehaviorFieldIsAFieldTheseNodesCarry(t *testing.T) {
	got, err := installentry.Decode([]byte(mustRender(t)))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	behavior := got.Collectors["gcp"].Behavior
	for _, list := range [][]string{
		behavior.EmbedFields, behavior.SummarizeFields, behavior.Bm25Fields,
	} {
		for _, field := range list {
			if !slices.Contains(installentry.NodeFieldsThisCollectorPopulates(), field) {
				t.Errorf("the behavior block names the field %q, which this collector's nodes do "+
					"not carry", field)
			}
		}
	}
	// The control: the list of populated fields is not everything, so the check
	// above can fail.
	if slices.Contains(installentry.NodeFieldsThisCollectorPopulates(), "file_path") {
		t.Error("file_path is listed as populated; a cloud resource has no file path, and a list " +
			"that admitted every field would make the assertion above vacuous")
	}
}

func TestTheBehaviorKeysAreTheLoadersBehaviorKeys(t *testing.T) {
	var generic map[string]map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(mustRender(t)), &generic); err != nil {
		t.Fatalf("decoding generically: %v", err)
	}
	var behavior map[string]json.RawMessage
	if err := json.Unmarshal(generic["collectors"]["gcp"]["behavior"], &behavior); err != nil {
		t.Fatalf("the behavior block is not an object: %v", err)
	}
	for key := range behavior {
		if !slices.Contains(theBehaviorKeys, key) {
			t.Errorf("the behavior block carries %q, which the loader does not declare", key)
		}
	}
}

// The environment block travels inside the entry and stays the computed set.
func TestTheEntryCarriesTheComputedEnvironmentSet(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		got, err := installentry.Decode([]byte(mustRenderFor(t, goos)))
		if err != nil {
			t.Fatalf("%s: Decode: %v", goos, err)
		}
		env := got.Collectors["gcp"].Env
		want := enventry.Names(goos)
		if len(env) != len(want) {
			t.Errorf("%s: the entry declares %d names, the computed set has %d", goos, len(env), len(want))
		}
		for _, name := range want {
			if _, ok := env[name]; !ok {
				t.Errorf("%s: the entry does not declare %q", goos, name)
			}
		}
		// The same-run control on the target axis.
		other := map[string]string{"linux": "APPDATA", "windows": "HOME"}[goos]
		if _, wrong := env[other]; wrong {
			t.Errorf("%s: the entry declares %q, which belongs to the other target", goos, other)
		}
	}
}

// No value in the rendered entry is a real one. An operator copies this.
func TestTheRenderedEntryCarriesNoRealValue(t *testing.T) {
	rendered := mustRender(t)
	for _, tell := range []string{"/Users/", "/home/", "-----BEGIN", "AIza"} {
		if strings.Contains(rendered, tell) {
			t.Errorf("the rendered entry contains %q, which looks like a real value", tell)
		}
	}
}

func TestRenderIsStableAcrossCalls(t *testing.T) {
	if mustRender(t) != mustRender(t) {
		t.Error("two renders of the same target differ; the README would drift against itself")
	}
}

func mustRender(t *testing.T) string { return mustRenderFor(t, "linux") }

func mustRenderFor(t *testing.T, goos string) string {
	t.Helper()
	rendered, err := installentry.Render(goos)
	if err != nil {
		t.Fatalf("Render(%s): %v", goos, err)
	}
	return rendered
}

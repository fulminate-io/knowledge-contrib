// SPDX-License-Identifier: Apache-2.0

package installentry_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/installentry"
)

// installentry_test.go — the worked config entry, asserted against the shape the
// loader reads rather than against the generator that produced it.

// TestTheRenderedEntryDecodesStrictly is the round trip: what this package
// renders is a document the same strict decode accepts.
func TestTheRenderedEntryDecodesStrictly(t *testing.T) {
	rendered, err := installentry.Render()
	if err != nil {
		t.Fatalf("rendering the worked entry: %v", err)
	}

	file, err := installentry.Decode([]byte(rendered))
	if err != nil {
		t.Fatalf("the rendered entry does not decode against the loader's shape: %v", err)
	}
	entry, ok := file.Collectors[installentry.FamilyName]
	if !ok {
		t.Fatalf("the entry is not keyed by the graph family %q: %v",
			installentry.FamilyName, file.Collectors)
	}
	if entry.Type != "stdio" {
		t.Errorf("the entry's transport is %q, want stdio", entry.Type)
	}
	if entry.Tool != installentry.ToolName {
		t.Errorf("the entry names the tool %q, want %q", entry.Tool, installentry.ToolName)
	}
	if entry.Command == "" {
		t.Error("the entry names no command")
	}
}

// TestTheDecodeRefusesAnUndeclaredKey is THE CONTROL for every row that rests on
// the decode. Without it, a decoder that ignored unknown keys would accept a
// document the loader refuses and every assertion above would prove nothing.
func TestTheDecodeRefusesAnUndeclaredKey(t *testing.T) {
	if _, err := installentry.Decode([]byte(
		`{"collectors":{"gitlab-ci":{"type":"stdio","tool":"collect","invented_key":true}}}`,
	)); err == nil {
		t.Fatal("the decode accepts a key the loader does not declare")
	}
	// The known positive: the same decode accepts the declared shape.
	if _, err := installentry.Decode([]byte(
		`{"collectors":{"gitlab-ci":{"type":"stdio","tool":"collect"}}}`,
	)); err != nil {
		t.Fatalf("the decode refuses a valid entry: %v", err)
	}
}

// TestTheFamilyNameIsNeitherTheBuiltInTypeNorTheBareProviderName.
//
// Two names are unusable here and for different reasons: the built-in graph type,
// which the client refuses to register at all; and the provider's bare one-word
// name, which a compiled-in collector already holds and which the client's
// post-collect linker is keyed on — a registration under it would have a linker
// run over a graph whose data it was never written for.
func TestTheFamilyNameIsNeitherTheBuiltInTypeNorTheBareProviderName(t *testing.T) {
	if installentry.FamilyName != "gitlab-ci" {
		t.Errorf("the family name is %q", installentry.FamilyName)
	}
	for _, forbidden := range []string{"cicd", "gitlab"} {
		if installentry.FamilyName == forbidden {
			t.Errorf("the family name is %q, which this module may not claim", forbidden)
		}
	}
}

// TestTheEnvironmentBlockCarriesTheSelectorLiterallyAndTheTokenByReference.
//
// This collector is NOT the all-secret shape: one of its three names is a
// selector, written as a literal host, and one is the primary credential,
// written as a bare reference to its own name. No credential VALUE appears, the
// defaulted spelling appears nowhere, and the FALLBACK token name is absent —
// the two token names are alternatives, so referencing both would demand that an
// operator hold two variables to authenticate once.
func TestTheEnvironmentBlockCarriesTheSelectorLiterallyAndTheTokenByReference(t *testing.T) {
	entry := installentry.Worked().Collectors[installentry.FamilyName]

	if len(entry.Env) != 2 {
		t.Fatalf("the entry's environment block carries %d keys, want the instance selector and "+
			"the primary token reference: %v", len(entry.Env), entry.Env)
	}
	value, ok := entry.Env[enventry.BaseURLVariable]
	if !ok {
		t.Errorf("the environment block does not carry %q: %v", enventry.BaseURLVariable, entry.Env)
	}
	if value == "" {
		t.Error("the selector is written present and empty, which selects the thing named by the " +
			"empty string; an installer omits the key entirely instead")
	}
	if strings.HasPrefix(value, "${") {
		t.Errorf("the selector is written as a reference (%q); it names a host rather than a "+
			"secret, so it is written out", value)
	}
	token, ok := entry.Env[enventry.PrimaryTokenVariable]
	if !ok {
		t.Fatalf("the environment block does not carry %q, so an operator copying this entry "+
			"installs a collector with no way to receive its token: %v",
			enventry.PrimaryTokenVariable, entry.Env)
	}
	if want := "${" + enventry.PrimaryTokenVariable + "}"; token != want {
		t.Errorf("the token key holds %q, want the bare reference %q: a value belongs in no "+
			"config file, and the defaulted spelling arrives at the collector present and empty",
			token, want)
	}
	if _, ok := entry.Env[enventry.FallbackTokenVariable]; ok {
		t.Errorf("the entry references the fallback %q as well as the primary; the two are "+
			"alternatives", enventry.FallbackTokenVariable)
	}

	rendered, err := installentry.Render()
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if strings.Contains(rendered, ":-") {
		t.Errorf("the worked entry carries a DEFAULTED reference:\n%s", rendered)
	}
	// The classes agree with the block: the literal key is the selector and the
	// referenced key is the credential.
	classes := enventry.Classes()
	if classes[enventry.BaseURLVariable] != enventry.ClassSelector {
		t.Errorf("the key written literally is classed %q rather than a selector",
			classes[enventry.BaseURLVariable])
	}
	if classes[enventry.PrimaryTokenVariable] != enventry.ClassSecret {
		t.Errorf("the key written as a reference is classed %q rather than a secret",
			classes[enventry.PrimaryTokenVariable])
	}
}

// TestTheBehaviorBlockIsDeclaredWithItsThreeFieldLists. Two of the three booleans
// default to FALSE, so an entry with no block is collected and walkable and never
// summarized or embedded; and the three field lists have no default at all.
func TestTheBehaviorBlockIsDeclaredWithItsThreeFieldLists(t *testing.T) {
	entry := installentry.Worked().Collectors[installentry.FamilyName]
	behavior := entry.Behavior
	if behavior == nil {
		t.Fatal("the worked entry carries no behavior block, so an operator who copies it gets a " +
			"graph that is never summarized or embedded and three empty field lists")
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

// TestNoPerNodeTypeOverrideIsShipped. This collector emits ONE node type with the
// kind in metadata, so there is nothing to override per type — and a block naming
// a type it does not emit would point the pipeline at nothing.
func TestNoPerNodeTypeOverrideIsShipped(t *testing.T) {
	entry := installentry.Worked().Collectors[installentry.FamilyName]
	if len(entry.NodeTypes) != 0 {
		t.Errorf("the entry ships %d per-node-type overrides: %v", len(entry.NodeTypes), entry.NodeTypes)
	}
}

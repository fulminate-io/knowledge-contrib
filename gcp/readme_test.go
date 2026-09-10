// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/installentry"
)

// readme_test.go — THE DOCUMENTATION GATE, asserted against what the CONSUMER
// reads.
//
// THE MISTAKE THIS FILE REPLACES. The first version asserted that the README
// contained its own generator's output. That is an identity check: it proves the
// document and the generator agree and says nothing about whether either is what
// the client parses — and neither was. The document was in one serialization and
// the loader reads another, at a path the document never named, and the test
// passed.
//
// So the entry in the README is now EXTRACTED and DECODED through the same
// strict decode the loader uses, and the assertions are against key names
// written out rather than produced by the code under test. What this still
// cannot prove is that the far side has not changed its shape; that is the live
// confirmation's job, and it is said here rather than implied.

func readme(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	return string(raw)
}

// jsonFence extracts the first fenced JSON block from the document, which is the
// worked config entry an operator copies.
func jsonFence(t *testing.T, doc string) string {
	t.Helper()
	const open = "```json\n"
	_, rest, ok := strings.Cut(doc, open)
	if !ok {
		t.Fatal("the README carries no fenced JSON block; the worked config entry is the whole " +
			"install mechanism and an operator has nothing to copy")
	}
	body, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("the README's JSON fence is never closed")
	}
	return body
}

// TestTheReadmeEntryDecodesThroughTheLoadersOwnShape is the assertion the whole
// file exists for: the thing an operator copies is a document the loader can
// read, not merely one this module can generate.
func TestTheReadmeEntryDecodesThroughTheLoadersOwnShape(t *testing.T) {
	fence := jsonFence(t, readme(t))

	got, err := installentry.Decode([]byte(fence))
	if err != nil {
		t.Fatalf("the README's worked entry does not decode against the loader's shape: %v\n"+
			"An operator copying it would get a refusal rather than a collector.", err)
	}
	entry, ok := got.Collectors[installentry.FamilyName]
	if !ok {
		t.Fatalf("the worked entry is not keyed by the graph family %q: %v",
			installentry.FamilyName, got.Collectors)
	}
	if entry.Type != "stdio" {
		t.Errorf("the worked entry's transport is %q, want stdio", entry.Type)
	}
	if entry.Tool == "" {
		t.Error("the worked entry names no tool, so the daemon would not know what to call")
	}
	if entry.Command == "" {
		t.Error("the worked entry names no command")
	}

	// THE COMMAND IS THE ONE THE PUBLISHED ARCHIVE CARRIES, and this compares
	// the README against the code rather than against a second literal. The
	// release publishes one archive per collector per platform, each holding a
	// binary called knowledge-collector-<collector>, so an operator who
	// installed from a release and one who built from source write the same
	// entry. Nothing held these two together before: the README's command and
	// installentry's placeholder were edited to different names once and every
	// test stayed green, because the assertions above only ask that a command is
	// present.
	if entry.Command != installentry.Worked("linux").Collectors[installentry.FamilyName].Command {
		t.Errorf("the README's worked command is %q and the collector's own entry names %q; "+
			"an operator copying the README would name a binary the release does not publish",
			entry.Command, installentry.Worked("linux").Collectors[installentry.FamilyName].Command)
	}
}

// THE CONTROL for the extraction and the decode together: a fence carrying a key
// the loader does not declare must be refused. Without it, an extraction that
// returned nothing and a decode that ignored unknown keys would both pass the
// case above silently.
func TestTheReadmeGateRefusesAnEntryTheLoaderWouldRefuse(t *testing.T) {
	_, err := installentry.Decode([]byte(
		`{"collectors":{"gcp":{"type":"stdio","tool":"collect","invented_key":true}}}`))
	if err == nil {
		t.Fatal("the gate accepts a key the loader does not declare; every assertion resting on " +
			"it proves nothing")
	}
}

// TestTheReadmeNamesTheFileAndBothScopes. An entry in the right format at a path
// the reader has to guess is the same failure as an entry in the wrong format.
func TestTheReadmeNamesTheFileAndBothScopes(t *testing.T) {
	doc := readme(t)
	for _, want := range []string{
		installentry.FileName,
		installentry.UserScopePath,
		installentry.ProjectScopePath,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the README never names %q, so an operator cannot know where the entry goes", want)
		}
	}
}

// TestTheReadmeEntryDeclaresBehaviorWithItsThreeFieldLists is R2's behavior
// clause, asserted on the SHIPPED artifact rather than on the generator.
func TestTheReadmeEntryDeclaresBehaviorWithItsThreeFieldLists(t *testing.T) {
	got, err := installentry.Decode([]byte(jsonFence(t, readme(t))))
	if err != nil {
		t.Fatalf("decoding the README's entry: %v", err)
	}
	behavior := got.Collectors[installentry.FamilyName].Behavior
	if behavior == nil {
		t.Fatal("the README's worked entry carries no behavior block, so an operator who copies " +
			"it declares nothing and the three field lists stay empty")
	}
	for name, value := range map[string]*bool{
		"syncable":     behavior.Syncable,
		"summarizable": behavior.Summarizable,
		"embeddable":   behavior.Embeddable,
	} {
		if value == nil || !*value {
			t.Errorf("the README's behavior block does not declare %s true", name)
		}
	}
	for name, list := range map[string][]string{
		"embed_fields":     behavior.EmbedFields,
		"summarize_fields": behavior.SummarizeFields,
		"bm25_fields":      behavior.Bm25Fields,
	} {
		if len(list) == 0 {
			t.Errorf("the README's behavior block leaves %s empty; nothing defaults it", name)
		}
		for _, field := range list {
			if !slices.Contains(installentry.NodeFieldsThisCollectorPopulates(), field) {
				t.Errorf("%s names %q, which this collector's nodes do not carry", name, field)
			}
		}
	}
}

// TestTheReadmeEntryDeclaresEveryEnvironmentName iterates the README, not the
// generator. The previous version of this assertion iterated the generator's own
// output and asserted about it, which meant its name was not true.
func TestTheReadmeEntryDeclaresEveryEnvironmentName(t *testing.T) {
	doc := readme(t)
	got, err := installentry.Decode([]byte(jsonFence(t, doc)))
	if err != nil {
		t.Fatalf("decoding the README's entry: %v", err)
	}
	env := got.Collectors[installentry.FamilyName].Env

	for _, name := range enventry.Names("linux") {
		if _, ok := env[name]; !ok {
			t.Errorf("the README's entry does not declare %q, which the credential chain reads", name)
		}
	}
	// Nothing EXTRA either: a name an operator declares expecting it to do
	// something, and which nothing reads, is its own small lie.
	for name := range env {
		if !slices.Contains(enventry.Names("linux"), name) {
			t.Errorf("the README's entry declares %q, which is not in the computed set", name)
		}
	}
	// The deliberate omissions are absent from the SHIPPED document, read from
	// the document rather than from the generator.
	for omitted := range enventry.DeliberatelyOmitted {
		if _, present := env[omitted]; present {
			t.Errorf("the README's entry declares %q, which is deliberately omitted", omitted)
		}
	}
	// The other target's one differing name is named in prose.
	if !strings.Contains(doc, "APPDATA") {
		t.Error("the README does not tell a Windows operator which name replaces HOME")
	}
}

// TestTheReadmeCarriesNoRealValue is the disclosure check. A worked example that
// shipped a real path or a real token would be wrong for every reader at best
// and a credential at worst.
func TestTheReadmeCarriesNoRealValue(t *testing.T) {
	doc := readme(t)
	for _, tell := range []string{"/Users/", "/home/", "-----BEGIN", "AIza"} {
		if strings.Contains(doc, tell) {
			t.Errorf("the README contains %q, which looks like a real value rather than a placeholder",
				tell)
		}
	}
}

// TestTheReadmeDocumentsNoOtherSerialization is the regression pin for the
// format defect: the install section was written in a serialization the loader
// does not parse, and nothing noticed.
func TestTheReadmeDocumentsNoOtherSerialization(t *testing.T) {
	doc := strings.ToLower(readme(t))
	for _, wrong := range []string{"```toml", "```yaml", "```yml", "```ini"} {
		if strings.Contains(doc, wrong) {
			t.Errorf("the README carries a %s fence; the config file is JSON and the loader parses "+
				"nothing else", wrong)
		}
	}
}

// TestTheInstallingSectionPointsAtThePublishedArtifacts holds the published-
// artifact pointer, which the command-name guard above does not: that one binds
// the worked entry's command to this module's own constant, and this one binds
// the section that tells a reader where the released binary comes from. Until
// this test nothing observed that sentence, so a later edit could drop it with
// every gate in this module green.
func TestTheInstallingSectionPointsAtThePublishedArtifacts(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	doc := string(raw)

	const heading = "## Installing it"
	_, rest, ok := strings.Cut(doc, heading)
	if !ok {
		t.Fatalf("README.md carries no %q section; that section is where a reader is told how to get the binary", heading)
	}
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}

	for _, want := range []string{"knowledge-contrib", "install script"} {
		if !strings.Contains(rest, want) {
			t.Errorf("the %q section does not mention %q, so it does not tell a reader where the "+
				"released binaries live or how to get one:\n%s", heading, want, rest)
		}
	}
}

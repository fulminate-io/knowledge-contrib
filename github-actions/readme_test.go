// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/installentry"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/walk"
)

// readme_test.go — THE DOCUMENTATION GATE, asserted against what the CONSUMER
// reads.
//
// THE MISTAKE THIS FILE AVOIDS. The obvious version asserts that the README
// contains its own generator's output. That is an identity check: it proves the
// document and the generator agree and says nothing about whether either is what
// the client parses — and in a neighboring collector neither was, while the test
// stayed green.
//
// So the entry in the README is EXTRACTED and DECODED through the same strict
// decode the loader uses, and the assertions are against key names written out
// rather than produced by the code under test. What this still cannot prove is
// that the far side has not changed its shape; that is the live confirmation's
// job, and it is said here rather than implied.

func readme(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	// THE LENGTH FLOOR IS THE KNOWN POSITIVE. A missing README fails the read; a
	// TRUNCATED one would satisfy every "does not contain" assertion below and
	// fail none of them, so the floor is what distinguishes "the file is not
	// there" from "the file does not say it".
	if len(raw) < 2000 {
		t.Fatalf("README.md is %d bytes, too short to be this module's documentation; the "+
			"assertions below would pass or fail for the wrong reason", len(raw))
	}
	return string(raw)
}

// jsonFence extracts the first fenced JSON block, which is the worked config
// entry an operator copies.
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
	got, err := installentry.Decode([]byte(jsonFence(t, readme(t))))
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
	if entry.Tool != installentry.ToolName {
		t.Errorf("the worked entry names the tool %q; the binary serves %q",
			entry.Tool, installentry.ToolName)
	}

	// THE COMMAND IS THE ONE THE PUBLISHED ARCHIVE CARRIES, compared against the
	// code rather than against a second literal. The release publishes one
	// archive per collector per platform, each holding a binary called
	// knowledge-collector-<collector>, so an operator who installed from a
	// release and one who built from source write the same entry.
	want := installentry.Worked().Collectors[installentry.FamilyName].Command
	if entry.Command != want {
		t.Errorf("the README's worked command is %q and the collector's own entry names %q; an "+
			"operator copying the README would name a binary the release does not publish",
			entry.Command, want)
	}
}

// TestTheReadmeGateRefusesAnEntryTheLoaderWouldRefuse is THE CONTROL for the
// extraction and the decode together. Without it, an extraction that returned
// nothing and a decode that ignored unknown keys would both pass the case above
// silently.
func TestTheReadmeGateRefusesAnEntryTheLoaderWouldRefuse(t *testing.T) {
	if _, err := installentry.Decode([]byte(
		`{"collectors":{"github-actions":{"type":"stdio","tool":"collect","invented_key":true}}}`,
	)); err == nil {
		t.Fatal("the gate accepts a key the loader does not declare; every assertion resting on " +
			"it proves nothing")
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
			"it gets a graph that is never summarized or embedded and three empty field lists")
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

// TestTheReadmeEntryCarriesTheTokenReferenceAndNoValue is this collector's own
// row, asserted on the SHIPPED document.
//
// Both names in its closure are credentials, so the entry an operator copies
// carries the primary name REFERENCING ITSELF and no value anywhere. The three
// wrong shapes are ruled out separately, because each fails differently: a value
// puts the credential in a file; a `${NAME:-}` default resolves to the empty
// string in a serving process that lacks the name, which this collector reports
// as a missing token; and referencing BOTH names demands two variables to
// authenticate once.
func TestTheReadmeEntryCarriesTheTokenReferenceAndNoValue(t *testing.T) {
	doc := readme(t)
	fence := jsonFence(t, doc)

	if !strings.Contains(fence, `"env"`) {
		t.Errorf("the README's worked entry carries no env block, so an operator copying it "+
			"installs a collector with no way to receive its token:\n%s", fence)
	}
	want := fmt.Sprintf("%q: %q", enventry.PrimaryTokenVariable,
		"${"+enventry.PrimaryTokenVariable+"}")
	if !strings.Contains(fence, want) {
		t.Errorf("the README's worked entry does not carry %s:\n%s", want, fence)
	}
	if strings.Contains(fence, ":-") {
		t.Errorf("the README's worked entry carries a DEFAULTED reference, which arrives at this "+
			"collector present and empty:\n%s", fence)
	}
	if strings.Contains(fence, enventry.FallbackTokenVariable) {
		t.Errorf("the README's worked entry names the fallback %q beside the primary; the two are "+
			"alternatives:\n%s", enventry.FallbackTokenVariable, fence)
	}
	for _, name := range enventry.Names() {
		// THE PROSE NAMES BOTH, which is the known positive for the assertion
		// above: a document that mentioned neither would pass it while leaving an
		// operator with no idea what to set.
		if !strings.Contains(doc, name) {
			t.Errorf("the README never names %q, so an operator does not know which variable to "+
				"set", name)
		}
	}
	// AND NO CREDENTIAL KEY HOLDS ANYTHING BUT A REFERENCE TO ITS OWN NAME,
	// checked on the DECODED block rather than by string search: a value is a
	// value whatever it looks like, and the shape this round removed is exactly
	// "some other text under a credential key".
	decoded, err := installentry.Decode([]byte(fence))
	if err != nil {
		t.Fatalf("the README's worked entry does not decode: %v", err)
	}
	for key, value := range decoded.Collectors[installentry.FamilyName].Env {
		if !enventry.IsDeclared(key) {
			continue
		}
		if value != "${"+key+"}" {
			t.Errorf("the README's worked entry sets the credential key %q to %q; a credential key "+
				"carries a reference to its own name and nothing else", key, value)
		}
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
			t.Errorf("the README never names %q, so an operator cannot know where the entry goes",
				want)
		}
	}
}

// TestTheReadmeCarriesNoRealValue is the disclosure check. A worked example that
// shipped a real path or a real token would be wrong for every reader at best and
// a credential at worst.
func TestTheReadmeCarriesNoRealValue(t *testing.T) {
	doc := readme(t)
	for _, tell := range []string{"/Users/", "/home/", "-----BEGIN", "ghp_", "github_pat_"} {
		if strings.Contains(doc, tell) {
			t.Errorf("the README contains %q, which looks like a real value rather than a "+
				"placeholder", tell)
		}
	}
}

// TestTheReadmeDocumentsNoOtherSerialization. The config file is JSON and the
// loader parses nothing else; an install section written in another
// serialization is the same failure as one with the wrong keys.
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
// artifact pointer, which the command-name guard does not: that one binds the
// worked entry's command to this module's own constant, and this one binds the
// section that tells a reader where the released binary comes from.
//
// IT PINS MEANING RATHER THAN WORDING: the section must mention the published
// repository and the script, in whatever sentence.
func TestTheInstallingSectionPointsAtThePublishedArtifacts(t *testing.T) {
	doc := readme(t)
	const heading = "## Installing it"
	_, section, ok := strings.Cut(doc, heading)
	if !ok {
		t.Fatalf("README.md carries no %q section; that section is where a reader is told how to "+
			"get the binary", heading)
	}
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}

	for _, want := range []string{"knowledge-contrib", "install script"} {
		if !strings.Contains(section, want) {
			t.Errorf("the %q section does not mention %q, so it does not tell a reader where the "+
				"released binaries live or how to get one", heading, want)
		}
	}
	// And the sentence that says what the script writes for a credential: the
	// VALUE never, a reference to the variable when this shell holds the name.
	// This collector's whole environment closure is secret-class, so an operator
	// reading only this section still has to learn where the token comes from.
	if !strings.Contains(section, "writes no provider credential value") {
		t.Errorf("the %q section does not say that the install script writes no provider "+
			"credential, which is the one thing about this collector's installation that differs "+
			"from every other's", heading)
	}
}

// TestTheReadmeNamesEveryDeclaredKindAndRelationship closes the prose half of the
// vocabulary, which nothing compared to anything.
//
// THE EXPECTATION IS DERIVED FROM THE VOCABULARY RATHER THAN WRITTEN OUT. A list
// of kinds and edge types retyped here would go stale on the same change the
// README does, and on the same day; reading the declared vocabulary means a class
// added to the collector and not to the document is a red, which is the direction
// that matters. Nothing in this row pins HOW MANY there are: a count in prose is
// a second copy of the list that can disagree with it silently, and the two count
// words this module's README used to carry were both wrong the moment a class was
// added.
//
// WHAT IT CANNOT PROVE is that the sentence about a class is TRUE. That is a
// reader's job and a reviewer's; what it proves is that the document has not
// simply stopped mentioning something the collector produces.
func TestTheReadmeNamesEveryDeclaredKindAndRelationship(t *testing.T) {
	doc := readme(t)

	// THE KIND HALF MATCHES THE TABLE ROW, NOT THE WORD. Every kind but one is an
	// ordinary English word this document uses many times over — `repository`
	// twenty-three times, `environment` twenty-two — so a substring search was
	// satisfied by prose that says nothing about the vocabulary. Measured on this
	// tree: deleting the whole `user` table row AND the section describing the
	// people left the row green, because the word survived elsewhere.
	//
	// The row is the carrier because it is what a reader consults: a kind in the
	// first cell of a table row is the document DESCRIBING that kind, and a kind
	// mentioned in a sentence is not.
	for _, kind := range ghgraph.ResourceTypes() {
		row := regexp.MustCompile(`(?m)^\| ` + regexp.QuoteMeta(kind) + ` \|`)
		if !row.MatchString(doc) {
			t.Errorf("the README's kind table carries no row for %q, which this collector emits "+
				"and a consumer queries on. A mention in a sentence is not the table an operator "+
				"reads to find out what the graph contains.", kind)
		}
	}
	for _, edgeType := range ghgraph.EdgeTypes() {
		if !strings.Contains(doc, edgeType) {
			t.Errorf("the README never names the relationship %q, which this collector emits",
				edgeType)
		}
	}

	// THE KNOWN POSITIVE for the matcher: a relationship this collector declares
	// and does NOT emit is named too, as an absence the document states on
	// purpose. Without it a matcher that found every string in any document would
	// pass this row unchanged.
	if !strings.Contains(doc, ghgraph.EdgeRunsIn) {
		t.Errorf("the README never names %q, whose deliberate absence is a documented property "+
			"of this collector", ghgraph.EdgeRunsIn)
	}
	// AND NO COUNT WORD STANDS IN FOR THE LIST. These are the two the document
	// carried, each a second copy of a list it sits beside.
	for _, stale := range []string{"eleven resource kinds", "Six relationship types"} {
		if strings.Contains(doc, stale) {
			t.Errorf("the README carries %q. A count of a list the collector decides is a copy of "+
				"that list which nothing keeps true; name the classes instead", stale)
		}
	}
}

// TestTheReadmeCapTableCarriesTheRealDefaults closes the prose half of the same
// property the collect package pins as literals.
//
// THE TABLE IS OPERATOR-FACING AND NOTHING COMPARED IT TO ANYTHING. A reader
// plans a collect from those two numbers, and they were free to disagree with
// the code: measured on this tree, changing both defaults left every package
// green and the table silently false. So the document is read and the numbers in
// it are compared against the constants the walk actually applies — which is the
// one direction that has to hold, since the collect package separately pins
// those constants to the parity literals.
func TestTheReadmeCapTableCarriesTheRealDefaults(t *testing.T) {
	doc := readme(t)
	for _, row := range []struct {
		parameter string
		value     int
	}{
		{"max_runs", collect.DefaultMaxRuns},
		{"max_deployments", collect.DefaultMaxDeployments},
	} {
		// The row as the table writes it: the parameter in backticks, then the
		// default, then the ceiling.
		want := fmt.Sprintf("| `%s` | %d | %d |", row.parameter, row.value, walk.CapCeiling)
		if !strings.Contains(doc, want) {
			t.Errorf("the README's parameter table carries no row %q. An operator plans a collect "+
				"from that table, so a default it states and the walk does not apply is a number "+
				"a reader acts on and the collector ignores", want)
		}
	}

	// THE KNOWN POSITIVE for the matcher: a row this collector does NOT document
	// must be absent, so an assertion that passed against any document would not
	// pass here.
	if strings.Contains(doc, "| `max_runners` |") {
		t.Error("the README documents a parameter this collector does not take")
	}
}

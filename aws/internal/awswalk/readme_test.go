// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readme_test.go — THE SHIPPED DOCUMENTATION IS PART OF THE CONTRACT, so it is
// tested rather than trusted.
//
// The module's README is what an operator reads to write a config entry, and the
// entry's `env` block is this process's WHOLE environment. A name this collector
// reads and the README does not document is a name nobody knows to set, and the
// symptom is a setting that silently does nothing.
//
// THE FENCE IS THE SAME ONE THE ENVIRONMENT CENSUS USES, and for a narrower
// reason here: the README is inside this MODULE but outside this PACKAGE's
// directory, and the go tool records an opened name only when it resolves inside
// the tested package's own module root — which the README does, being at the
// module root. So this read is already tracked, and the fence below is for the
// dependency closure the census in the same file walks.

// readmePath is the module README, two directories up from internal/awswalk. It
// resolves inside this module's root, so the go tool records the read and an edit
// to it re-runs these tests.
const readmePath = "../../README.md"

func readREADME(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.FromSlash(readmePath))
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}
	if len(body) < 1000 {
		t.Fatalf("%s is %d bytes, which is too short to be this module's documentation; the assertions below "+
			"would pass vacuously on a stub", readmePath, len(body))
	}
	return string(body)
}

// TestREADME_DocumentsEveryEnvironmentNameThisCollectorReads is the first
// direction: nothing this collector reads goes undocumented.
func TestREADME_DocumentsEveryEnvironmentNameThisCollectorReads(t *testing.T) {
	fenceTestCacheOnPinnedModules(t)
	body := readREADME(t)

	var missing []string
	for _, name := range DocumentedEnvNames() {
		if !strings.Contains(body, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s documents %d fewer environment variables than this collector reads; missing: %v.\n"+
			"The entry's env block is the child's WHOLE environment, so a name nobody knows to set is a "+
			"setting that silently does nothing.", readmePath, len(missing), missing)
	}
}

// TestREADME_DoesNotShowAnEnvBlockOfEmptyValues is the CORRECTNESS point that
// decided the documentation's shape, asserted rather than left to review.
//
// Under the config-file contract a name PRESENT with an empty value is not the
// same as an absent one: `"AWS_PROFILE": ""` asks the SDK for a profile whose
// name is the empty string, which resolves nothing, where omitting it asks for
// the default profile. A README whose worked entry pasted all eighty-eight names
// with empty values would therefore be advice that breaks the credential chain.
func TestREADME_DoesNotShowAnEnvBlockOfEmptyValues(t *testing.T) {
	body := readREADME(t)

	// THE SUBJECT IS THE FENCED BLOCKS, not the prose. The paragraph explaining
	// this hazard necessarily QUOTES the shape it warns about, and a whole-file
	// scan flags that explanation — which it did on this test's first run. What an
	// operator copies is a code block, so that is what is checked.
	blocks := fencedBlocks(body)
	if len(blocks) == 0 {
		t.Fatal("the README carries no fenced code blocks, so this assertion would pass on nothing")
	}
	for _, block := range blocks {
		for _, bad := range []string{`"AWS_PROFILE": ""`, `"AWS_REGION": ""`, `"HOME": ""`} {
			if strings.Contains(block, bad) {
				t.Errorf("%s shows %s in a worked entry; a name present with an empty value is not the same as "+
					"an absent one, and this one breaks the credential chain", readmePath, bad)
			}
		}
	}
	// AND IT SAYS SO, because a reader following the example without the reason
	// will paste the full list the next time they need one more name.
	if !strings.Contains(body, "Set only what you are setting") {
		t.Errorf("%s does not state the present-with-an-empty-value hazard; the worked entry alone does not "+
			"stop a reader pasting the whole list", readmePath)
	}
}

// TestREADME_NamesTheToolAndTheEntryShape pins the two facts an operator cannot
// guess and cannot recover from getting wrong: an entry naming the wrong tool is
// refused at install, and an entry under the wrong name lands the results in a
// graph family nobody queries.
func TestREADME_NamesTheToolAndTheEntryShape(t *testing.T) {
	body := readREADME(t)
	if !strings.Contains(body, ToolName) {
		t.Errorf("%s does not name the served tool %q, which a config entry's `tool` field must carry exactly",
			readmePath, ToolName)
	}
	if !strings.Contains(body, "The entry name is the graph family") &&
		!strings.Contains(body, "entry name is the graph family") {
		t.Errorf("%s does not state that the entry NAME is the graph family; an operator who guesses lands "+
			"their results in a family nobody queries", readmePath)
	}
	if !strings.Contains(body, `"type": "stdio"`) {
		t.Errorf("%s shows no stdio entry; that is the transport this binary is installed as", readmePath)
	}
}

// TestREADME_CarriesNoTrackerOrNodeReferences guards the SHIPPED SURFACE rule: a
// collector module's README is written for a reader with none of this project's
// internal context.
func TestREADME_CarriesNoTrackerOrNodeReferences(t *testing.T) {
	body := readREADME(t)
	for _, bad := range []string{"FUL-", "ticket 4", "the prefill", "node id 0bd1446d"} {
		if strings.Contains(body, bad) {
			t.Errorf("%s carries %q, which means nothing to the reader this file is for", readmePath, bad)
		}
	}
}

// fencedBlocks returns the bodies of every fenced code block in a markdown
// document — the parts an operator copies rather than reads.
func fencedBlocks(body string) []string {
	parts := strings.Split(body, "```")
	var out []string
	// Odd-indexed segments are inside a fence; the first line of each is its
	// language tag and is dropped.
	for i := 1; i < len(parts); i += 2 {
		if _, rest, found := strings.Cut(parts[i], "\n"); found {
			out = append(out, rest)
		}
	}
	return out
}

// TestREADME_DocumentsTheOptInAxesRatherThanARequiredBlock is a documentation
// gate on what an operator has to know to get what they came for.
//
// WHAT IT USED TO GATE, and why it does not any more. A registered family whose
// entry declared no `embed_fields` composed its embed text from an empty list,
// was stamped with a terminal marker on every node, and never entered the text
// index — so the README had to tell the operator to hand-write a behavior block
// or ship them an unsearchable graph. Both halves of that are gone: an opted-in
// axis with no declared list takes a default field shape, and keyword search does
// not depend on the embed axis at all.
//
// WHAT IT GATES NOW. The surprising fact moved rather than disappeared. An
// operator who installs this collector and expects semantic search will not get
// it until they opt in, so the README has to say the axes are opt-in and name the
// flags that turn them on. A README that merely dropped the old paragraph would
// leave that silence unexplained.
func TestREADME_DocumentsTheOptInAxesRatherThanARequiredBlock(t *testing.T) {
	body := readREADME(t)

	blocks := fencedBlocks(body)
	entry := ""
	for _, b := range blocks {
		if strings.Contains(b, `"tool"`) && strings.Contains(b, `"collectors"`) {
			entry = b
			break
		}
	}
	if entry == "" {
		t.Fatal("the README shows no worked config entry, so this assertion would pass on nothing")
	}

	// THE WORKED ENTRY NO LONGER CARRIES THE BLOCK, which is the instruction this
	// change removed: copying it must produce a searchable graph with nothing
	// added.
	for _, field := range []string{"bm25_fields", "summarize_fields", "embed_fields"} {
		if strings.Contains(entry, field) {
			t.Errorf("the worked entry still declares %q; the install instruction is now that no behavior "+
				"block is needed, and an entry that shows one teaches the opposite", field)
		}
	}

	// AND THE PROSE SAYS WHAT IS OPT-IN AND HOW TO OPT IN, because an operator who
	// wants semantic search and finds none has to be told where to look.
	for _, phrase := range []string{"opt-in", "--embeddable", "--summarizable"} {
		if !strings.Contains(body, phrase) {
			t.Errorf("the README does not mention %q; an operator expecting semantic search would get "+
				"keyword search and no explanation", phrase)
		}
	}
}

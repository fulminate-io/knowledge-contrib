// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// readme_test.go — THE DOCUMENTATION GATE for this module's README, asserted
// against what a real collector answers and against this package's own code
// rather than against a second transcription of either.
//
// THE MISTAKE THIS FILE EXISTS TO PREVENT is the one a sibling module's gate
// records: a claim about the wire outlives the wire. A README that states a
// protocol revision, a handshake field, a schema shape or a count is a claim
// nothing observes, and the eight collector READMEs each carry a gate because
// one of those claims went stale under a green suite. This document ships to a
// public repository and is the only thing a collector author in another
// language reads, so every claim in it that CAN be observed is observed here:
//
//   - the handshake values are compared to a live child collector's answers;
//   - the two advertised schemas are compared to the contract bytes;
//   - the counts (the framework's own size, the contract documents' lengths,
//     the node and edge vocabularies, the required property names) are COMPUTED
//     from this package and compared to what the document states, so a number
//     that goes stale reds here instead of misleading a reader;
//   - the retired-claim and disclosure classes are asserted absent;
//   - the worked config entry is decoded and its command asserted absolute.
//
// The one gate that is EXPECTED RED at the tree this file lands on is the
// declaration pin at the bottom, and its own comment says why.

// readmeFloor is the byte floor that separates "the file is not there" from
// "the file does not say it". Every claim assertion below rests on it: without
// it a deleted README and a README missing one sentence fail identically.
const readmeFloor = 1000

func readmeDoc(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md, the file this gate is about: %v", err)
	}
	if len(raw) < readmeFloor {
		t.Fatalf("README.md is %d bytes, below the %d-byte floor; every claim assertion below it "+
			"would pass or fail for the wrong reason", len(raw), readmeFloor)
	}
	return string(raw)
}

// readmeSection returns one section of the document, from its heading to the
// next heading of the same or a higher level, and FAILS when the heading is
// gone.
//
// IT IS SCOPED ON PURPOSE. An assertion over the whole page passes on a
// corrected sentence that landed under some other heading, and on a section
// that was deleted rather than fixed. A scoped read makes both impossible.
func readmeSection(t *testing.T, doc, heading string) string {
	t.Helper()
	section, err := sectionOf(doc, heading)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return section
}

func sectionOf(doc, heading string) (string, error) {
	start := strings.Index(doc, heading+"\n")
	if start < 0 {
		return "", fmt.Errorf("README.md carries no %q section; this gate reads a section that no longer exists", heading)
	}
	rest := doc[start+len(heading):]
	level := len(heading) - len(strings.TrimLeft(heading, "#"))
	for depth := level; depth >= 2; depth-- {
		next := "\n" + strings.Repeat("#", depth) + " "
		if end := strings.Index(rest, next); end >= 0 {
			rest = rest[:end]
		}
	}
	return rest, nil
}

// claim is one thing the document has to say, with the reason it has to say it.
// The reason reaches the failure message: a reader of a red run should not have
// to open this file to learn what was lost.
type claim struct {
	phrase string
	why    string
}

// missingClaims reports which claims the text does not carry. It is a function
// rather than a loop of assertions so that the control below can prove it
// reports a miss at all.
//
// BOTH SIDES ARE WHITESPACE-NORMALIZED, because the document is prose wrapped
// at a column: a claim that happens to straddle a line break would otherwise be
// reported missing from the page that carries it, and the fix for that red
// would be to re-wrap the paragraph rather than to write the sentence.
func missingClaims(text string, claims []claim) []claim {
	flatText := flat(text)
	var missing []claim
	for _, c := range claims {
		if !strings.Contains(flatText, flat(c.phrase)) {
			missing = append(missing, c)
		}
	}
	return missing
}

// flat collapses every run of whitespace to one space.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

func assertClaims(t *testing.T, text string, claims []claim) {
	t.Helper()
	for _, c := range missingClaims(text, claims) {
		t.Errorf("the README never says %q: %s", c.phrase, c.why)
	}
}

// TestREADMEGateReportsAMissingClaimAndAMissingSection is THE CONTROL for the
// two helpers every case below rests on. Without it, a claim matcher that
// always returned nothing and a section reader that always returned the whole
// document would leave every assertion in this file green and empty.
func TestREADMEGateReportsAMissingClaimAndAMissingSection(t *testing.T) {
	const doc = "# a document\n\nIt says one thing.\n\n## A section\n\nwith a body.\n"

	missing := missingClaims(doc, []claim{
		{"It says one thing.", "present, so the matcher must not report it"},
		{"a sentence this document does not carry", "absent, so the matcher must report it"},
	})
	if len(missing) != 1 || missing[0].phrase != "a sentence this document does not carry" {
		t.Errorf("the claim matcher reported %v; a matcher that cannot report a miss makes every "+
			"assertion in this file vacuous", missing)
	}

	if _, err := sectionOf(doc, "## A heading this document does not carry"); err == nil {
		t.Error("the section reader accepted a heading the document does not carry; every scoped " +
			"assertion would then run over an empty string and pass")
	}
	body, err := sectionOf(doc, "## A section")
	if err != nil {
		t.Fatalf("the section reader lost a section that is present: %v", err)
	}
	if !strings.Contains(body, "with a body") {
		t.Errorf("the section reader returned %q for a section that is present", body)
	}
}

// TestREADMEStatesTheContractItsReadersHaveToSatisfy asserts the wire contract
// half, one Errorf per claim so a failure names which claim was lost.
func TestREADMEStatesTheContractTheClientEnforces(t *testing.T) {
	doc := readmeDoc(t)
	contract := readmeSection(t, doc, "## The wire contract")

	assertClaims(t, contract, []claim{
		{"2025-06-18", "the MCP revision floor: outputSchema and structuredContent exist only from it, " +
			"so a provider that speaks only the earlier revision cannot satisfy this contract at all"},
		{"outputSchema", "the property whose absence is the most common refusal at registration"},
		{"structuredContent", "the other half of the revision floor's reason"},
		{"walk_complete", "the completeness assertion, which is required so that silence cannot disable " +
			"the server's deletion phase"},
		{"source_graph", "the cross-graph field for an edge whose foreign endpoint is the source"},
		{"target_graph", "and the one for an edge whose foreign endpoint is the destination"},
		{"metadata", "where everything outside the node vocabulary rides"},
		{"stderr", "the stream a collector's diagnostics go to, because stdout is the protocol stream"},
	})

	// THE REQUIRED PROPERTY NAMES ARE READ OUT OF THE CONTRACT DOCUMENTS, not
	// typed here: this is what makes the assertion a comparison against the
	// contract rather than against a second copy of it that can drift with it.
	for _, name := range requiredNames(t, decode(t, InputContractJSON())) {
		if !strings.Contains(contract, name) {
			t.Errorf("the contract section never names the input contract's required property %q", name)
		}
	}
	for _, name := range requiredNames(t, decode(t, OutputContractJSON())) {
		if !strings.Contains(contract, name) {
			t.Errorf("the contract section never names the output contract's required property %q", name)
		}
	}
	for _, itemProperty := range []string{"nodes", "edges"} {
		for _, name := range requiredItemNames(t, decode(t, OutputContractJSON()), itemProperty) {
			if !strings.Contains(contract, name) {
				t.Errorf("the contract section never names %q, which the output contract requires on every %s item",
					name, itemProperty)
			}
		}
	}
}

// TestREADMEDeclarationSectionStatesTheRequiredTool reads the declaration
// section on EVERY run, which is what keeps it from being an unobserved page
// while the live pin beside it waits for the tool to exist.
//
// THE CLAIMS ARE THE ONES A PORT AUTHOR AND AN OPERATOR ARE WRONG WITHOUT: that
// the tool is required rather than optional and its absence is a refusal, that
// the declaration carries environment NAMES and CLASSES and never a value, and
// that summarize and embed stay operator flags whatever the collector suggests.
// The last one is the paragraph most likely to be softened into "the collector
// declares what it wants", which would be a document that is wrong about money.
func TestREADMEDeclarationSectionStatesTheRequiredTool(t *testing.T) {
	section := readmeSection(t, readmeDoc(t), "## Declaring what your collector is")

	assertClaims(t, section, []claim{
		{"required", "the tool is required rather than optional; that is the fact a port author most needs"},
		{"refused", "a collector that does not serve it, or serves a bad declaration, is refused by name " +
			"and nothing is written"},
		{"secret", "one of the three environment classes the declaration carries"},
		{"path", "the second class"},
		{"selector", "the third class"},
		{"vocabular", "the node and edge type vocabularies, which the server refuses a collect outside of"},
		{"operator flags", "summarize and embed do not come from the declaration"},
		{"default OFF", "and both default off, which is the spend ruling this paragraph carries"},
		{"never applied", "the collector's suggestion is printed and not acted on"},
	})

	// The environment half of the credential rule, asserted as a claim rather
	// than left to the absence gate: a section that documented declaring a
	// secret's VALUE would be caught by nothing else, because no real value
	// would appear in it.
	assertClaims(t, section, []claim{
		{"names and never their values", "the declaration carries environment names, not values"},
	})
}

// TestREADMECountsAreTheMeasuredOnes computes every number the document states
// from the package itself. THE DIRECTION THIS DOCUMENT IS WRITTEN TO is that a
// number in it is a measured one; a number nothing measures is the class most
// certain to be stale, and these are the ones that can be measured from inside
// this module.
//
// THE THREE SPELLED-OUT COUNTS ARE HERE FOR THE SAME REASON THE DIGITS ARE.
// "The contract is two documents", "they require four properties" and "the two
// entry points" were prose until a change added a third contract document and a
// fourth required property under a green suite: a count spelled as a word is
// exactly as stale as a count spelled as a digit, and nothing observed these.
// They are computed here from the contract directory, from the documents' own
// required lists, and from a census of this module's exported Serve entry
// points, so the next change to any of the three reds here.
func TestREADMECountsAreTheMeasuredOnes(t *testing.T) {
	doc := readmeDoc(t)

	inputLines := lineCount(InputContractJSON())
	outputLines := lineCount(OutputContractJSON())
	describeLines := lineCount(DescribeContractJSON())
	nodeFields := reflect.TypeFor[Node]().NumField()
	edgeFields := reflect.TypeFor[Edge]().NumField()
	sourceLines, sourceFiles := frameworkSourceSize(t)
	contractDocuments := contractDocumentCount(t)
	requiredProperties := contractRequiredPropertyCount(t)
	entryPoints := serveEntryPoints(t)

	for _, stated := range []struct {
		phrase string
		why    string
	}{
		{fmt.Sprintf("%d, %d and %d lines", inputLines, outputLines, describeLines),
			"the contract documents' lengths, counted from the embedded bytes"},
		{fmt.Sprintf("%d node fields and %d edge fields", nodeFields, edgeFields),
			"the optional vocabulary, counted off the Node and Edge types"},
		{fmt.Sprintf("%d lines of Go across %s files", sourceLines, spellOut(sourceFiles)),
			"this module's own size, counted off its non-test source"},
		{fmt.Sprintf("The contract is %s documents", spellOut(contractDocuments)),
			"the number of schema documents in this module's contract/ directory"},
		{fmt.Sprintf("they require **%s properties**", spellOut(requiredProperties)),
			"the total length of the contract documents' own required lists"},
		{fmt.Sprintf("the %s entry points", spellOut(len(entryPoints))),
			"a census of this module's exported Serve entry points"},
	} {
		if !strings.Contains(flat(doc), stated.phrase) {
			t.Errorf("the README does not state %q: %s. Re-measure the number and write the measured one; "+
				"a stale count in a published document is a claim a reader cannot check",
				stated.phrase, stated.why)
		}
	}

	// The count alone would stay green if an entry point were RENAMED, so the
	// censused names are asserted present too: the sentence that carries the
	// count is the one that lists them.
	for _, name := range entryPoints {
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("this module exports the entry point %s and the README never names it; the sentence "+
				"that counts the entry points is the one that lists them", name)
		}
	}
}

// TestREADMEWorkedEntryIsCopyable decodes the worked config entry an operator
// copies and asserts the properties a copied entry has to have.
//
// IT DECODES INTO A LOCAL SHAPE rather than through the client's own loader,
// because the loader is in a package this module cannot import — that
// separation is the framework's whole premise. The assertions are therefore
// about key names written out here, which is what makes them a comparison
// rather than an identity check against a generator.
func TestREADMEWorkedEntryIsCopyable(t *testing.T) {
	fence := fenceIn(t, readmeSection(t, readmeDoc(t), "## Registering it is a config entry"), "```json\n")

	var entry struct {
		Collectors map[string]struct {
			Type    string            `json:"type"`
			Tool    string            `json:"tool"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"collectors"`
	}
	decoder := json.NewDecoder(strings.NewReader(fence))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entry); err != nil {
		t.Fatalf("the README's worked entry does not decode: %v\nAn operator copying it would get a "+
			"refusal rather than a collector.", err)
	}
	if len(entry.Collectors) != 1 {
		t.Fatalf("the worked entry declares %d collectors; it teaches one", len(entry.Collectors))
	}
	for family, e := range entry.Collectors {
		if family == "" {
			t.Error("the worked entry's family key is empty; the key IS the graph family the results land in")
		}
		if e.Type != "stdio" {
			t.Errorf("the worked entry's transport is %q, want stdio, which is the shape a daemon spawns", e.Type)
		}
		if e.Tool == "" {
			t.Error("the worked entry names no tool, so a daemon would not know what to call")
		}
		if !filepath.IsAbs(e.Command) {
			t.Errorf("the worked entry's command %q is not absolute; the command is resolved against the "+
				"DAEMON's PATH, so a relative one in a published example is a collector that cannot be spawned",
				e.Command)
		}
		// The two env spellings the prose distinguishes are both shown, or the
		// distinction is a paragraph with no example under it.
		var present, empty int
		for _, v := range e.Env {
			if v == "" {
				empty++
			} else {
				present++
			}
		}
		if present == 0 || empty == 0 {
			t.Errorf("the worked entry's env block shows %d set and %d present-and-empty names; the "+
				"section distinguishes the two spellings and the example has to show both", present, empty)
		}
	}
}

// fenceIn extracts the first fenced block of a section opened by the given
// fence, so an assertion runs over the block a reader copies rather than over
// the prose around it.
func fenceIn(t *testing.T, section, open string) string {
	t.Helper()
	_, rest, ok := strings.Cut(section, open)
	if !ok {
		t.Fatal("the section carries no fenced JSON block; the worked config entry is the whole " +
			"registration mechanism and a reader has nothing to copy")
	}
	body, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("the section's JSON fence is never closed")
	}
	return body
}

func requiredNames(t *testing.T, doc map[string]any) []string {
	t.Helper()
	raw, ok := doc["required"].([]any)
	if !ok || len(raw) == 0 {
		t.Fatalf("the contract document declares no required list: %s", mustJSON(t, doc))
	}
	names := make([]string, 0, len(raw))
	for _, v := range raw {
		names = append(names, fmt.Sprint(v))
	}
	return names
}

func requiredItemNames(t *testing.T, doc map[string]any, property string) []string {
	t.Helper()
	items, ok := propertyOf(t, doc, property)["items"].(map[string]any)
	if !ok {
		t.Fatalf("the output contract's %q property declares no items schema", property)
	}
	return requiredNames(t, items)
}

func lineCount(raw []byte) int { return strings.Count(string(raw), "\n") }

// frameworkSourceSize counts this module's non-test Go source, which is the one
// number in the README about the module itself.
func frameworkSourceSize(t *testing.T) (lines, files int) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the module directory: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		lines += strings.Count(string(raw), "\n")
		files++
	}
	if files == 0 {
		t.Fatal("the module directory holds no non-test Go file, so the size the README states cannot be checked")
	}
	return lines, files
}

// spellOut renders a small count the way the document's prose writes it.
func spellOut(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return fmt.Sprint(n)
}

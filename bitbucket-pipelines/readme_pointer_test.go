// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/walk"
)

// readme_pointer_test.go — THE README'S CLAIMS ABOUT THE GRAPH POINT AT THE
// CODE'S OWN DECLARATIONS.
//
// The other two README gates check the INSTALL half: that the worked entry
// decodes through the loader's shape, that it names this collector's own family
// and tool, and that it carries no credential. Neither of them reads the half a
// consumer actually plans against — the resource kinds and the relationship
// types the document promises.
//
// A DOCUMENT AND A GENERATOR AGREEING PROVES NOTHING, which is why these
// assertions run in the other direction: the README is checked against the
// DECLARED VOCABULARY, and the vocabulary is checked against the emitted set by
// the parity suite. So a kind dropped from the code reds here as well as there,
// and a kind promised in prose that the code never declared reds here alone.

// TestTheReadmeNamesEveryDeclaredResourceType.
func TestTheReadmeNamesEveryDeclaredResourceType(t *testing.T) {
	doc := readme(t)
	for _, resourceType := range bbgraph.ResourceTypes() {
		if !strings.Contains(doc, resourceType) {
			t.Errorf("the README never names the resource kind %q, which this collector emits; a "+
				"consumer planning against the document would not know it is there", resourceType)
		}
	}
}

// TestTheReadmeNamesEveryDeclaredEdgeTypeAndTheOneItDoesNotEmit.
//
// THE ABSENT ONE IS NAMED TOO, and that is the point of saying so in the
// document as well as in the code: a consumer looking for the seventh CI/CD
// relationship gets an answer rather than an empty query, and a later change
// that started emitting it would contradict a sentence someone has to delete.
func TestTheReadmeNamesEveryDeclaredEdgeTypeAndTheOneItDoesNotEmit(t *testing.T) {
	doc := readme(t)
	for _, edgeType := range bbgraph.EdgeTypes() {
		if !strings.Contains(doc, edgeType) {
			t.Errorf("the README never names the relationship %q, which this collector emits",
				edgeType)
		}
	}
	if !strings.Contains(doc, bbgraph.EdgeTriggeredBy) {
		t.Errorf("the README never names %q, the one CI/CD relationship this provider does not "+
			"emit; its absence is a fact a consumer needs and this collector asserts",
			bbgraph.EdgeTriggeredBy)
	}
	if !strings.Contains(doc, "There is no "+bbgraph.EdgeTriggeredBy) {
		t.Errorf("the README names %q without saying it is NOT emitted, which reads as a promise "+
			"the graph does not keep", bbgraph.EdgeTriggeredBy)
	}
}

// TestTheReadmeNamesTheMetadataKeyAConsumerHasToKnowAbout. The unresolved-
// reference key is this collector's own — nothing in the contract or in a
// sibling declares it — so a consumer learns of it here or not at all.
func TestTheReadmeNamesTheMetadataKeyAConsumerHasToKnowAbout(t *testing.T) {
	doc := readme(t)
	for _, key := range []string{
		bbgraph.UnresolvedRefsKey,
		bbgraph.UnresolvedDeploymentsKey,
		bbgraph.UnresolvedRunsOnKey,
	} {
		if !strings.Contains(doc, key) {
			t.Errorf("the README never names the %q metadata key, which is where a step "+
				"reference that resolved to nothing is recorded", key)
		}
	}
	if !strings.Contains(doc, "resource_type") {
		t.Error("the README never names the `resource_type` metadata key, which is the field a " +
			"consumer filters this graph on")
	}
}

// TestTheReadmeDocumentsTheOneParameterAndSaysWhatIsNotOne. The history depth is
// the value most likely to be looked for as a parameter, because the provider
// this collector reproduces exposes it as an environment variable and nothing
// says so anywhere else.
func TestTheReadmeDocumentsTheOneParameterAndSaysWhatIsNotOne(t *testing.T) {
	doc := readme(t)
	if !strings.Contains(doc, "max_concurrency") {
		t.Error("the README never names the one collect parameter this collector takes")
	}
	if !strings.Contains(doc, "The history depth is NOT a parameter") {
		t.Error("the README does not say that the history depth is an environment selector " +
			"rather than a parameter, which is where a reader will look for it first")
	}
	// AND IT SAYS THE COLLECT ID IS THE WORKSPACE, which is the other thing a
	// caller has to know and the reason there is no workspace parameter.
	for _, want := range []string{"The `id` is the Bitbucket workspace", "no parameter naming a"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the README does not say %q", want)
		}
	}
}

// TestTheReadmeDocumentsTheThreeIncompleteClasses. They are what an operator
// reads when a collect comes back marked, and each has a different remedy — so a
// document naming two of the three sends someone to the wrong fix.
func TestTheReadmeDocumentsTheThreeIncompleteClasses(t *testing.T) {
	doc := readme(t)
	for _, want := range []string{"refused", "rate limited", "partial"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the README does not name the %q outcome", want)
		}
	}
	// AND WHAT A 404 MEANS, WHICH IS NOT ONE ANSWER. On a LISTING it is a URL
	// this walk could not read and it marks the collect; on the pipeline
	// definition FILE it is a repository with no pipeline and it does not. The
	// README said the second of those about both, and while it did, a
	// repository-scope variables path that 404'd on every request was documented
	// behavior rather than a defect. Both halves are asserted, because a document
	// carrying only the listing half would send an operator hunting a repository
	// that simply has no pipeline.
	for _, want := range []string{
		"A 404 on a listing is a partial read",
		"A 404 on a repository's pipeline definition is not",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the README does not say %q, so it does not tell an operator which 404 "+
				"marks a collect and which one does not", want)
		}
	}
}

// TestTheReadmeParameterTableCarriesTheNumbersTheWalkApplies.
//
// THE TABLE IS OPERATOR-FACING AND NOTHING COMPARED IT TO ANYTHING. A reader
// plans a collect from those two numbers and they were free to disagree with the
// code: measured on this tree, changing the default from 10 to 7 left all seven
// packages green and the table silently false, while the ceiling and the history
// depth both red. So the document is read and its numbers are compared against
// the constants the walk actually applies, which is why both are exported.
func TestTheReadmeParameterTableCarriesTheNumbersTheWalkApplies(t *testing.T) {
	doc := readme(t)

	want := fmt.Sprintf("| `%s` | %d | %d |",
		"max_concurrency", walk.DefaultConcurrency, walk.ConcurrencyCeiling)
	if !strings.Contains(doc, want) {
		t.Errorf("the README's parameter table carries no row %q. An operator plans a collect "+
			"from that table, so a default it states and the walk does not apply is a number a "+
			"reader acts on and the collector ignores", want)
	}

	// THE HISTORY DEPTH IS IN THE SAME CLASS even though it is not a parameter:
	// the README states the number, and the walk applies it.
	if !strings.Contains(doc, fmt.Sprintf("unset means %d", walk.DefaultHistoryDepth)) {
		t.Errorf("the README's environment table does not state the history depth as %d, which "+
			"is what the walk applies when the variable is unset", walk.DefaultHistoryDepth)
	}

	// THE KNOWN POSITIVE for the matcher: a row this collector does NOT document
	// must be absent, so an assertion that passed against any document would not
	// pass here.
	if strings.Contains(doc, "| `max_runs` |") {
		t.Error("the README documents a parameter this collector does not take")
	}
}

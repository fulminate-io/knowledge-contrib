// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readme_test.go — THE README'S CROSS-GRAPH SECTION IS A LOAD-BEARING
// STATEMENT, and until now nothing observed it.
//
// WHY IT GETS A GATE, MEASURED RATHER THAN SUPPOSED. The section carried a
// sentence describing a pending pin that "goes red the moment a target-graph
// field exists". That field landed a change ago and the pin was retired with it,
// and the sentence survived — because no test read this file. A reader of the
// README was told the collector was waiting for something it already had. The
// same class is now live twice over, since the source-graph field retires the
// held-emission paragraph as well.
//
// IT IS SCOPED TO THE SECTION, not to the whole page: an assertion over the
// whole file would pass on a corrected sentence that landed under some other
// heading, which is the failure a scoped read makes impossible.

// crossGraphSection returns the README's "## Cross-graph edges" section, from
// its heading to the next one.
func crossGraphSection(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("README.md")
	require.NoError(t, err, "reading README.md, the file this gate is about")

	const anchor = "## Cross-graph edges"
	start := strings.Index(string(raw), anchor)
	require.GreaterOrEqual(t, start, 0,
		"README.md carries no %q section — this gate reads a section that no longer exists", anchor)

	rest := string(raw)[start+len(anchor):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	return rest
}

// TestREADME_CrossGraphSectionDescribesTheEmittedShapes asserts both halves:
// what the section must now say, and that neither retired claim survives.
//
// PRESENCE ALONE PASSES on a page that says the right thing in one paragraph and
// the retired one in the next, which is exactly the shape a partial edit leaves
// behind — and exactly what happened here last time. Absence alone passes on a
// section that was deleted rather than corrected.
func TestREADME_CrossGraphSectionDescribesTheEmittedShapes(t *testing.T) {
	section := crossGraphSection(t)

	for _, want := range []struct{ phrase, why string }{
		{"All four are emitted",
			"the Helm shape is emitted now; a reader told three of four would look for a held shape that is not held"},
		{"source_graph",
			"and the field it uses, since which field a shape sets is the statement of which end is foreign"},
		{"target_graph",
			"beside the field the other three use"},
		{"is not emitted",
			"and the workload-to-repository shape that this collector does NOT emit, so a reader " +
				"comparing against the built-in linker is told about the gap rather than left to find it"},
	} {
		assert.Contains(t, section, want.phrase, want.why)
	}

	for _, retired := range []struct{ phrase, why string }{
		{"All five are emitted",
			"the workload-to-repository shape was dropped, so four shapes remain"},
		{"Four of the five are emitted",
			"the held shape was emitted when the source-graph field landed"},
		{"The fifth is computed and held",
			"the same paragraph, which described a state that ended with that field"},
		{"goes red the moment a target-graph field exists",
			"that pin was retired when the target-graph field landed, a change earlier; the sentence outlived it because nothing read this file"},
	} {
		assert.NotContains(t, section, retired.phrase,
			"the cross-graph section still carries the retired claim %q: %s", retired.phrase, retired.why)
	}
}

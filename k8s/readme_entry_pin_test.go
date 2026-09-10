// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// readme_entry_pin_test.go — THE README'S WORKED ENTRY IS THE ONE ExampleEntry
// RENDERS, byte for byte.
//
// WHY IT DID NOT EXIST AND WHAT THAT COST. The README said, above the block,
// "The entry below is what `ExampleEntry` generates". Nothing held it to that,
// and the block had drifted in THREE keys at once: it carried no `context` at
// all, its `env` block listed three names with literal path values against the
// nineteen `${VAR:-}` references the generator emits, and its `behavior` block
// carried three keys against six. So an operator copying the documented entry
// installed a collector that received no foreign-graph block, ran with a
// three-name environment, and registered a graph whose indexed field lists were
// never declared — under a sentence promising the opposite.
//
// A PIN BETWEEN TWO ARTIFACTS THAT BOTH DRIFT IS NOT ENOUGH, which is why this
// is one of a pair. This half holds README = ExampleEntry; the client-side
// loads-test holds ExampleEntry = a config file the client's own loader accepts.
// Either alone permits the state this collector shipped in — a documented entry
// and a generated entry that agreed with each other and that no loader would
// take.
func TestTheREADMEShipsTheGeneratedEntry(t *testing.T) {
	generated, err := ExampleEntryJSON("linux", "/home/you/.knowledge/bin/knowledge-collector-k8s")
	require.NoError(t, err)

	body, err := os.ReadFile("README.md")
	require.NoError(t, err, "this module's README could not be read")

	// THE KNOWN POSITIVE, in the same run: the README really was read and really
	// is this module's documentation. Without it a rename would leave this test
	// passing over an empty string.
	require.Greaterf(t, len(body), 2000,
		"README.md is %d bytes, too short to be this module's documentation", len(body))

	require.Containsf(t, string(body), generated,
		"the README's config entry is not the one ExampleEntry generates. Paste this in place of "+
			"the fenced json block:\n\n%s", generated)
}

// TestTheREADMEDescribesTheContextBlockItShips is the prose half. The `reason`
// each declaration carries is a Go field the entry never renders — the client's
// decoder has no field for it and refuses an unknown key by name, failing the
// whole config file — so the README is the only place an operator reads WHY the
// two entries are there.
func TestTheREADMEDescribesTheContextBlockItShips(t *testing.T) {
	body, err := os.ReadFile("README.md")
	require.NoError(t, err)
	text := string(body)

	for _, phrase := range []string{"WORKLOAD_IDENTITY", "DEPLOYS", "cloud-resource"} {
		require.Containsf(t, text, phrase,
			"the README ships a context block whose entries carry no reason on the wire, so it owes "+
				"the reader %q in prose", phrase)
	}
}

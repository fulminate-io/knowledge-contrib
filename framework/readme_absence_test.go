// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"regexp"
	"strings"
	"testing"
)

// readme_absence_test.go — the ABSENCE half of the README gate: the claims,
// serializations and disclosures the document must NOT carry.
//
// IT IS ITS OWN FILE for the reason readme_live_test.go is: one feature area
// per file, and readme_test.go had grown into the repository's file-length
// warning band. Nothing here changed when it moved.
//
// PRESENCE ALONE PASSES on a page that says the right thing in one paragraph
// and a retired thing in the next, which is exactly what a partial edit leaves
// behind. This half is what makes that shape a red.

// TestREADMECarriesNoRetiredWrongOrLeakedClaim is the absence half. Presence
// alone passes on a page that says the right thing in one paragraph and a
// retired thing in the next, which is exactly what a partial edit leaves behind.
//
// THE SECOND GROUP IS TIGHTER HERE THAN IN A MODULE README because this file is
// published to a public repository: the classes below are the ones the
// repository's own leak gate recognizes, written as classes rather than as one
// module's literals so that legitimate prose is not caught by them.
func TestREADMECarriesNoRetiredWrongOrLeakedClaim(t *testing.T) {
	doc := readmeDoc(t)

	for _, tell := range []claim{
		{"/Users/", "a real home path rather than a placeholder"},
		{"/home/", "the same, on the other platform"},
		{"-----BEGIN", "a key block; no credential material belongs in a published example"},
		{"AIza", "a provider API key prefix"},
	} {
		if strings.Contains(doc, tell.phrase) {
			t.Errorf("the README contains %q, which looks like %s", tell.phrase, tell.why)
		}
	}

	for _, wrong := range []string{"```toml", "```yaml", "```yml", "```ini"} {
		if strings.Contains(strings.ToLower(doc), wrong) {
			t.Errorf("the README carries a %s fence; the config file is JSON and the loader parses nothing else", wrong)
		}
	}

	// THE IN-REPO MODULE PATH IS THE ONE THAT BREAKS THE PUBLISH, not merely the
	// document: the sync rewrites module paths in Go and module files only,
	// while its post-rewrite survivor assertion scans every file — so this
	// string in this document aborts the publish naming this file.
	//
	// IT IS SPELLED IN TWO PIECES because that same rewrite would otherwise
	// rewrite THIS LINE. The published copy of this test would then hold the
	// PUBLISHED prefix here and fail against a document that correctly names it
	// — measured, not supposed: the standalone census runs this suite over a
	// staged tree and that is exactly what it reported.
	const inRepoModulePath = "github.com/fulminate-io/knowledge" + "/cmd/collectors"
	if strings.Contains(doc, inRepoModulePath) {
		t.Errorf("the README names the in-repo module path %q; the published document must name the "+
			"published path, and the publish aborts on this string rather than shipping it", inRepoModulePath)
	}

	// The refusal totals were a hand enumeration that no command measures. The
	// document states the CLASSES; a total in it would be a number a reader
	// cannot check and a maintainer cannot re-derive.
	for _, unmeasured := range []string{"28 refusals", "28 refusal", "~25"} {
		if strings.Contains(doc, unmeasured) {
			t.Errorf("the README states %q as if it were measured; no command in this repository "+
				"produces that number", unmeasured)
		}
	}

	for _, pattern := range []struct {
		re  *regexp.Regexp
		why string
	}{
		{regexp.MustCompile(`\b[A-Z]{2,}-[0-9]{2,}\b`), "a tracker key, which is an internal reference in a public document"},
		{regexp.MustCompile(`\b[0-9a-f]{32}\b`), "a bare node id, which resolves to nothing outside this organization"},
		{regexp.MustCompile(`(?i)\bthe prefill\b`), "an internal process word"},
	} {
		if hit := pattern.re.FindString(doc); hit != "" {
			t.Errorf("the README contains %q, which is %s", hit, pattern.why)
		}
	}
}

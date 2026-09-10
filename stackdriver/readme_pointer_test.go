// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"
)

// THE PUBLISHED-ARTIFACT POINTER IS HELD BY THIS TEST, and by nothing else.
//
// WHY IT EXISTS. This README's "Installing it" section tells a reader where the
// released binaries live: the knowledge-contrib repository, and the install
// script there that fetches an archive, verifies it against checksums.txt and
// installs nothing when the checksum is missing or wrong. That sentence is the
// only route a user has from this document to a working collector, and until
// this test nothing observed it — a later edit to the section could drop it and
// every gate in this module would stay green. A prose pointer with no test is a
// pointer that rots.
//
// WHAT IT DELIBERATELY DOES NOT ASSERT: the wording. It looks for the repository
// name and for the install script, so the paragraph can be rewritten freely and
// only its MEANING is pinned. The command name in the worked config entry is a
// different property with a different owner.
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

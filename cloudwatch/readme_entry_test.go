// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE README'S WORKED ENTRY AND THIS MODULE'S OWN ENTRY NAME THE SAME BINARY.
//
// WHY THIS EXISTS. The README's config block is what an operator copies, and
// until this file nothing held it to the code: the block's `command` and
// binaryName were two independent literals, and editing one and not the other
// left every test in this module green while the published README told a reader
// to name a binary the release does not publish. That is not hypothetical — the
// two spelled different names before the release contract fixed one, and the
// suite had nothing to say about it.
//
// WHAT THE RELEASE PUBLISHES. One archive per collector per platform, each
// holding a single binary called knowledge-collector-<collector>, so an operator
// who installed from a release and one who built from source write the same
// entry. binaryName is that name, and this test is what keeps the README's copy
// of it honest.
//
// IT READS THE FILE, not a rendering of it, because the README is the artifact
// that ships and a test over a re-rendered string would pass while the shipped
// file said something else.
func TestTheReadmeWorkedEntryNamesTheReleaseBinary(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	doc := string(raw)

	// The worked entry is the first fenced jsonc block: the README writes it as
	// ```jsonc because it carries comments, which is also why the decode below
	// strips them rather than handing the raw text to encoding/json.
	const open = "```jsonc\n"
	_, rest, ok := strings.Cut(doc, open)
	if !ok {
		t.Fatal("README.md carries no fenced jsonc block; the worked config entry is what an operator copies")
	}
	fence, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("the fenced jsonc block in README.md is not closed")
	}

	var stripped strings.Builder
	for line := range strings.SplitSeq(fence, "\n") {
		if idx := strings.Index(strings.TrimSpace(line), "//"); idx == 0 {
			continue
		}
		stripped.WriteString(line)
		stripped.WriteString("\n")
	}

	var file struct {
		Collectors map[string]struct {
			Command string `json:"command"`
		} `json:"collectors"`
	}
	if err := json.Unmarshal([]byte(stripped.String()), &file); err != nil {
		t.Fatalf("the README's worked entry does not decode as JSON: %v\nAn operator copying it would get a refusal rather than a collector.\n%s", err, stripped.String())
	}

	entry, ok := file.Collectors[entryName]
	if !ok {
		t.Fatalf("the worked entry is not keyed by the graph family %q: %v", entryName, file.Collectors)
	}
	// THE BASENAME, not the whole command, and the reason is the daemon rather
	// than a loosening. An entry's command is resolved ONCE with exec.LookPath
	// against the DAEMON's own PATH, which under a service manager is a handful
	// of system directories and never ~/.knowledge/bin — so a bare name in a
	// worked entry is an entry that cannot start, and the install script writes
	// the absolute path into ~/.knowledge/bin for exactly that reason. What this
	// gate is for is unchanged: the binary an operator is told to name is the one
	// the release publishes, whatever directory it sits in.
	if filepath.Base(entry.Command) != binaryName {
		t.Errorf("the README's worked command is %q, whose basename is %q, and this module's "+
			"binaryName is %q; an operator copying the README would name a binary the release "+
			"does not publish", entry.Command, filepath.Base(entry.Command), binaryName)
	}
	if !filepath.IsAbs(entry.Command) {
		t.Errorf("the README's worked command %q is not absolute; the daemon resolves it against "+
			"its OWN PATH, which does not include the directory collectors are installed into",
			entry.Command)
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

// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readme_entry_pin_test.go — THE README'S WORKED ENTRY IS THE ONE ExampleEntry
// RENDERS, byte for byte.
//
// WHY IT DID NOT EXIST. This module renders its entry in Go and had three README
// gates, and none of them compared the two: two assert the documented block
// decodes as JSON and names the release binary, and the third exercises
// ExampleEntry against the declared environment allowlist. So the generator and
// the documentation were each checked against something, and never against each
// other — the worst of both, because a field added to the generator is silently
// absent from the block an operator actually copies. That is exactly what the
// `context` declaration would have done: generated into ExampleEntry, missing
// from the README, and no test anywhere the wiser.
//
// THE ENV BLOCK IS NO LONGER ABBREVIATED, AND THAT IS THE POINT. The block used
// to list three of the declared names under a sentence saying so. An abbreviated
// example is a working collector only for an operator whose host needs exactly
// those three, and the entry IS the child process's whole environment — so the
// documented block now carries every name this collector declares.
//
// NO ASSERTION LIBRARY IS USED IN THIS MODULE, and that is a constraint rather
// than a style: this collector is PUBLISHED STANDALONE and the workspace's
// standalone census builds and tests it with the workspace off, so a test-only
// dependency here is a dependency its operators acquire. The whole suite uses the
// standard library and so do these rows.
func TestTheREADMEShipsTheGeneratedEntry(t *testing.T) {
	generated, err := ExampleEntryJSON(TargetPOSIX)
	if err != nil {
		t.Fatalf("rendering the example entry: %v", err)
	}

	body, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("this module's README could not be read: %v", err)
	}
	// THE KNOWN POSITIVE, in the same run: the README really was read and really
	// is this module's documentation. Without it a rename would leave this test
	// passing over an empty string.
	if len(body) < 2000 {
		t.Fatalf("README.md is %d bytes, too short to be this module's documentation", len(body))
	}

	if !strings.Contains(string(body), generated) {
		t.Fatalf("the README's config entry is not the one ExampleEntry generates. Paste this in place "+
			"of the fenced json block:\n\n%s", generated)
	}
}

// TestTheREADMEDescribesTheContextBlockItShips is the prose half. Each
// declaration's `reason` is a Go field the entry never renders — the client's
// decoder has no field for it and refuses an unknown key by name, failing the
// whole config file — so the README is the only place an operator reads why the
// entries are there, and why two families they might expect are absent.
func TestTheREADMEDescribesTheContextBlockItShips(t *testing.T) {
	body, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	text := string(body)

	for _, phrase := range []string{"EMITTED_BY", "resource_type", "azure", "gcp"} {
		if !strings.Contains(text, phrase) {
			t.Errorf("the README ships a context block whose entries carry no reason on the wire, so it "+
				"owes the reader %q in prose", phrase)
		}
	}
}

// TestTheGeneratedEntrysCommandIsAbsolute is the defect this module's new README
// pin found, kept as its own row.
//
// ExampleEntry rendered the bare `binaryName` as the entry's command while the
// README's worked block carried an absolute path, and nothing compared the two —
// so the generator had been emitting an entry that CANNOT START since it was
// written. The daemon resolves an entry's command once with exec.LookPath
// against its OWN PATH, which under a service manager is a handful of system
// directories and never the directory collectors are installed into. The README
// gate that caught it reads the checked-in block; this one reads the generator,
// so the property is pinned on the artifact that produces it.
func TestTheGeneratedEntrysCommandIsAbsolute(t *testing.T) {
	entry := ExampleEntry(TargetPOSIX, exampleCommand)
	if !filepath.IsAbs(entry.Command) {
		t.Errorf("the generated entry's command is %q, which is not absolute; the daemon resolves it "+
			"against its own PATH and would never find it", entry.Command)
	}
	if base := filepath.Base(entry.Command); base != binaryName {
		t.Errorf("the generated command's basename is %q and the release publishes %q", base, binaryName)
	}

	// THE PARAMETER IS REAL, not decoration: a caller installing to its own
	// directory gets its own path, which is why the command is not a constant
	// baked into the entry.
	if got := ExampleEntry(TargetPOSIX, "/opt/knowledge/"+binaryName).Command; got != "/opt/knowledge/"+binaryName {
		t.Errorf("the command parameter did not reach the entry: %q", got)
	}
}

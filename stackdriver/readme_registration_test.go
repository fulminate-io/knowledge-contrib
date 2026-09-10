// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"strings"
	"testing"
)

// readme_registration_test.go — THE README HAS TO TELL AN OPERATOR HOW TO
// REGISTER THIS COLLECTOR, and registration is a config file plus one command.
//
// This collector was built into the knowledge client until the built-in cloud
// and log collectors were removed. Nothing registers it now except an entry in a
// `collectors.json` file, so a README that documents the environment and the
// tool but never names the file or the command that writes it leaves the reader
// with no way to install what they just built.
//
// WHY BOTH HALVES ARE ASSERTED. The file name alone sends a reader to hand-edit
// JSON when a command exists; the command alone hides where the entry lands and
// which scope it lands in. Each half is checked separately so a failure names
// which one is missing.
//
// THE LENGTH FLOOR IS THE KNOWN POSITIVE. A missing README fails the read; a
// TRUNCATED one would satisfy the floor and fail the content assertions, so the
// floor is what distinguishes "the file is not there" from "the file does not
// say it".
func TestREADME_NamesTheConfigFileAndTheAddInvocation(t *testing.T) {
	body, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	text := string(body)
	if len(text) < 1000 {
		t.Fatalf("README.md is %d bytes, too short to be this module's documentation; "+
			"the assertions below would pass or fail for the wrong reason", len(text))
	}

	if !strings.Contains(text, "collectors.json") {
		t.Errorf("README.md never names `collectors.json`. Registration IS that file — an entry in " +
			"~/.knowledge/collectors.json or <repo>/.knowledge/collectors.json — and a reader who has " +
			"built the binary has nowhere to put it")
	}

	const verb = "knowledge collector add"
	if !strings.Contains(text, verb) {
		t.Errorf("README.md never shows the `%s` invocation. The entry can be hand-written, but the "+
			"command is what an operator reaches for first, and it is the same one `knowledge collector "+
			"list` prints for a legacy family", verb)
	}

	// The invocation has to be THIS collector's, not a copied neighbour's: the
	// family name and the tool name are what a reader types, and a wrong pair
	// writes an entry that dials a provider which does not answer to it.
	const (
		family = "stackdriver"
		tool   = "collect"
	)
	var found bool
	for line := range strings.SplitSeq(text, "\n") {
		if !strings.Contains(line, verb) {
			continue
		}
		// THE TRAILING SPACE ON THE TOOL NEEDLE IS LOAD-BEARING, and its absence
		// made this whole assertion vacuous for five of the eight collectors.
		// Every collector tool name in the tree begins with "collect", and five
		// modules use the framework default "collect" verbatim, so without the
		// delimiter "--tool collect" is a PREFIX of "--tool collect_aws" and a
		// copied neighbour's invocation satisfied the needle. Measured before the
		// fix: substituting a neighbour's tool into the README reddened aws,
		// k8s-logs and loki and left azure, cloudwatch, gcp, k8s and stackdriver
		// silent. With the delimiter the same mutation reds all eight, and every
		// add invocation in the tree already puts a space after the tool value,
		// so no README needed editing.
		if strings.Contains(line, "--tool "+tool+" ") && strings.Contains(line, " "+family+" ") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no `%s` line in README.md carries both this collector's tool (--tool %s) and its family "+
			"name (%s). A copied neighbour's invocation registers the wrong provider under the wrong family",
			verb, tool, family)
	}
}

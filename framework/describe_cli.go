// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"
	"io"
	"os"
)

// describe_cli.go — the DECLARATION, ANSWERED WITHOUT A SESSION.
//
// WHY A COLLECTOR ANSWERS THESE ON ARGV AT ALL, when it already serves the same
// facts on the describe tool: the installer that writes a collector's config
// entry is `#!/bin/sh`, has no JSON parser and cannot speak MCP, and it needs
// two of these facts BEFORE it can register anything — which environment names
// to pass and which tool to name. The alternative was a hand-written table per
// collector inside the installer, which is the thing this ticket removes. So the
// binary answers three line-oriented questions, and the installer's tables are
// generated from those answers.
//
// THE THIRD QUESTION IS NOT ONE THE INSTALLER NEEDS TO REGISTER ANYTHING; it is
// one the DOCUMENTATION gate needs. Which of a collector's names it tells
// present-and-empty apart from absent decides whether a worked entry may show a
// `${NAME:-}` reference for that name, and only the collector knows the answer.
//
// THE SWITCHES ARE MATCHED EXACTLY AND ONLY IN FIRST POSITION, mirroring
// --version and for the same reason: a config entry may carry args of its own,
// the daemon passes them to the collector verbatim, and a looser match would eat
// one of them and serve nothing.
//
// STDOUT IS THE PROTOCOL STREAM once a session starts, so these arms write and
// return BEFORE anything serves.

// The three argv switches. They are spelled with the tool's own name so a
// reader of an installer script can find what answers them.
const (
	describeEnvTableArg = "--describe-env-table"
	describeToolArg     = "--describe-tool"
	// describeEnvSensitiveArg answers WHICH DECLARED NAMES ARE EMPTY-SENSITIVE,
	// one bare name per line and nothing when none are.
	//
	// IT IS ITS OWN QUESTION RATHER THAN A COLUMN ON THE ENV TABLE, and the
	// reason is a consumer rather than taste: the printed class table is read by
	// a gate that greps a WHOLE LINE with a trailing anchor, so an appended
	// field stops matching whether it is populated or empty. Keeping the two
	// answers separate leaves the class table's readers untouched, and the
	// not-carried class — which the class table drops by construction — can
	// carry a mark here with nowhere else to put it.
	describeEnvSensitiveArg = "--describe-env-sensitive"
)

// describeQuery reports which of the three switches an argv asks for, if any.
// args is a whole argv, os.Args-shaped, so index 0 is the program name.
func describeQuery(args []string) string {
	if len(args) < 2 {
		return ""
	}
	switch args[1] {
	case describeEnvTableArg, describeToolArg, describeEnvSensitiveArg:
		return args[1]
	default:
		return ""
	}
}

// serveDescribeQuery answers one of the three argv questions, if that is what
// this argv is, and reports whether it answered.
//
// A MALFORMED DECLARATION IS AN ERROR HERE TOO, not an empty table. A generator
// reading an empty table would write an empty install row and the collector
// would install with no environment at all, which is the silent-narrowing shape
// the whole contract refuses; so the arm fails loudly with the same message
// NewServer would have given.
func serveDescribeQuery[P any](c Collector[P]) bool {
	query := describeQuery(os.Args)
	if query == "" {
		return false
	}
	if err := writeDescribeQuery(os.Stdout, query, c); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	return true
}

// writeDescribeQuery writes one query's answer to w. Split from the argv arm so
// the format is testable without a process.
func writeDescribeQuery[P any](w io.Writer, query string, c Collector[P]) error {
	if c == nil {
		return fmt.Errorf("framework: no collector was supplied")
	}
	switch query {
	case describeToolArg:
		fmt.Fprintln(w, defaultedToolName(c.Tool().Name))
		return nil
	case describeEnvTableArg:
		decl := c.Describe()
		if err := decl.validate(); err != nil {
			return err
		}
		for _, row := range decl.EnvTable() {
			fmt.Fprintln(w, row)
		}
		return nil
	case describeEnvSensitiveArg:
		decl := c.Describe()
		if err := decl.validate(); err != nil {
			return err
		}
		// NOTHING IS PRINTED WHEN NOTHING IS MARKED, and that is the answer rather
		// than a failure: most collectors treat every name's empty value as
		// absent. The declaration is still validated first, so a broken
		// declaration fails loudly here too instead of being reported as a
		// collector that discriminates on nothing.
		for _, name := range decl.EmptySensitiveNames() {
			fmt.Fprintln(w, name)
		}
		return nil
	default:
		return fmt.Errorf("framework: %q is not a describe query", query)
	}
}

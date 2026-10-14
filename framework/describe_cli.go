// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"
	"io"
	"os"
)

// describe_cli.go — the argv queries a collector answers before it serves.
//
// Each query is matched exactly, and only as the first argument (os.Args[1]).
// Its answer is written to stdout, one item per line, and the program returns
// before any session starts:
//
//	--describe-env-table      one `class name` row per declared variable
//	--describe-tool           the served collect tool's name
//	--describe-env-sensitive  one name per empty-sensitive variable
//	--describe-summary        the collector's ToolSpec.Summary

// The argv switches.
const (
	describeEnvTableArg = "--describe-env-table"
	describeToolArg     = "--describe-tool"
	// describeEnvSensitiveArg answers which declared names are empty-sensitive,
	// one bare name per line and nothing when none are.
	describeEnvSensitiveArg = "--describe-env-sensitive"
	// describeSummaryArg answers the collector's ToolSpec.Summary on one line,
	// an empty line when it is empty.
	describeSummaryArg = "--describe-summary"
)

// describeQuery reports which of the argv switches an argv asks for, if any.
// args is a whole argv, os.Args-shaped, so index 0 is the program name.
func describeQuery(args []string) string {
	if len(args) < 2 {
		return ""
	}
	switch args[1] {
	case describeEnvTableArg, describeToolArg, describeEnvSensitiveArg, describeSummaryArg:
		return args[1]
	default:
		return ""
	}
}

// serveDescribeQuery answers one of the argv questions, if that is what this
// argv is, and reports whether it answered. A malformed declaration is returned
// as the error NewServer would have returned, with nothing written.
func serveDescribeQuery[P any](c Collector[P]) (bool, error) {
	query := describeQuery(os.Args)
	if query == "" {
		return false, nil
	}
	if err := writeDescribeQuery(os.Stdout, query, c); err != nil {
		return true, err
	}
	return true, nil
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
	case describeSummaryArg:
		fmt.Fprintln(w, c.Tool().Summary)
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

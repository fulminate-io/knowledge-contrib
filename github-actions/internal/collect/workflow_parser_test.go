// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"slices"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
)

// workflow_parser_test.go — the two readers over a workflow's text, on every
// form they meet and on the forms they deliberately do not read.
//
// THE FORMS THEY DO NOT READ ARE PINNED AS TESTS rather than described in a
// comment, because "this parser does not handle X" is a claim about behavior and
// a claim about behavior belongs in a run. Each one names what it would take to
// change the answer, so a later author reads a decision rather than a gap.

func TestParseSecretRefsOverEveryForm(t *testing.T) {
	for _, row := range []struct {
		name       string
		definition string
		want       []string
	}{
		{"no references at all", "name: CI\non: push\n", nil},
		{"one reference", "run: echo ${{ secrets.API_KEY }}\n", []string{"API_KEY"}},
		{
			"a repeated reference is one name",
			"run: echo ${{ secrets.API_KEY }}\nrun: echo ${{ secrets.API_KEY }}\n",
			[]string{"API_KEY"},
		},
		{
			"several names keep first-appearance order",
			"run: ${{ secrets.B_KEY }}\nrun: ${{ secrets.A_KEY }}\n",
			[]string{"B_KEY", "A_KEY"},
		},
		{
			"whitespace inside the expression is tolerated",
			"run: ${{   secrets.API_KEY   }}\n",
			[]string{"API_KEY"},
		},
		{
			"a lower-case name is NOT a secret reference: the provider's own secret " +
				"names are upper-case, and matching a lower-case one would read a variable " +
				"reference as a secret",
			"run: ${{ secrets.api_key }}\n",
			nil,
		},
		{
			"a name starting with a digit is not read, for the same reason",
			"run: ${{ secrets.1KEY }}\n",
			nil,
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := collect.ParseSecretRefs(row.definition); !slices.Equal(got, row.want) {
				t.Errorf("ParseSecretRefs(%q) = %v, want %v", row.definition, got, row.want)
			}
		})
	}
}

func TestParseEnvironmentRefsOverEveryForm(t *testing.T) {
	for _, row := range []struct {
		name       string
		definition string
		want       []string
	}{
		{"no environment at all", "jobs:\n  build:\n    runs-on: ubuntu-latest\n", nil},
		{"the inline form", "    environment: production\n", []string{"production"}},
		{"the quoted inline form", `    environment: "staging"` + "\n", []string{"staging"}},
		{"the single-quoted inline form", "    environment: 'staging'\n", []string{"staging"}},
		{
			"a repeated name is one entry",
			"    environment: production\n    environment: production\n",
			[]string{"production"},
		},
		{
			"several names keep first-appearance order",
			"    environment: production\n    environment: staging\n",
			[]string{"production", "staging"},
		},
		{
			"THE BLOCK FORM IS NOT READ. Recognizing `environment:` followed by an " +
				"indented `name:` means tracking indentation across lines, and `name:` " +
				"appears under half a dozen unrelated blocks in a workflow — so a reader " +
				"that guessed would emit a deploy edge to every step name in the file",
			"    environment:\n      name: production\n",
			nil,
		},
		{
			"THE FLOW-MAPPING FORM IS NOT READ: a value that opens a mapping is skipped " +
				"rather than having a name read out of it",
			"    environment: {name: production}\n",
			nil,
		},
		{
			"AN EXPRESSION IS READ AS WRITTEN, and the resulting edge resolves to no " +
				"environment. The value is decided when the workflow RUNS and this " +
				"collector reads the file; dropping the edge would change the graph a " +
				"consumer already reads, and evaluating the expression is not something " +
				"this collector can do",
			"    environment: ${{ inputs.target }}\n",
			[]string{"${{ inputs.target }}"},
		},
		{
			"A QUOTED MAPPING IS READ AS A NAME, because the mapping guard is applied " +
				"before the quotes are stripped. It is the one input on which the two " +
				"possible orders disagree, and this is the order the graph a consumer " +
				"reads was built with",
			`    environment: "{name: production}"` + "\n",
			[]string{"{name: production}"},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := collect.ParseEnvironmentRefs(row.definition); !slices.Equal(got, row.want) {
				t.Errorf("ParseEnvironmentRefs(%q) = %v, want %v", row.definition, got, row.want)
			}
		})
	}
}

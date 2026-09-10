// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/walk"
)

// params_test.go — THE PROJECT ID AS INPUT, validated rather than passed through.
//
// The project id is the ONE value a caller supplies, and it is interpolated into
// every request path this collector builds. Passing a malformed one through
// means the caller learns their mistake as a wall of forty per-service errors,
// or worse as an enumeration that quietly matched nothing. Bad input errors
// here, once, naming what is wrong with it.
//
// THE RULES ARE THE PROVIDER'S OWN, not this collector's invention: 6 to 30
// characters, lowercase letters, digits and hyphens, starting with a letter and
// not ending with a hyphen.

func TestWalkRefusesAMalformedProjectID(t *testing.T) {
	for _, tc := range []struct {
		name    string
		project string
		wantIn  string
	}{
		{"too short", "abcde", "6 and 30"},
		{"too long", strings.Repeat("a", 31), "6 and 30"},
		{"leading digit", "1project", "start with a letter"},
		{"leading hyphen", "-project", "start with a letter"},
		{"trailing hyphen", "project-", "end with a hyphen"},
		{"uppercase", "MyProject", "lowercase"},
		{"underscore", "my_project", "lowercase"},
		{"dot", "my.project", "lowercase"},
		{"slash", "projects/mine", "lowercase"},
		{"space", "my project", "lowercase"},
		{"a whole resource name", "projects/my-project", "lowercase"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := walk.Collector{Enumerations: fixedEnumerations()}.
				Walk(t.Context(), "gcp-instance", walk.Params{Project: tc.project},
					framework.ForeignContext{})
			if err == nil {
				t.Fatalf("the malformed project id %q was walked anyway", tc.project)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("the refusal does not say what is wrong: got %q, want it to mention %q",
					err, tc.wantIn)
			}
			// Every refusal quotes the value, so a caller building the id
			// programmatically can see what it actually sent.
			if !strings.Contains(err.Error(), tc.project) && tc.project != "" {
				t.Errorf("the refusal does not quote the offending value: %v", err)
			}
		})
	}
}

// THE SAME-RUN CONTROL. Without it, a validator that refused everything would
// pass every case above. These are real project id shapes and all must be
// accepted.
func TestWalkAcceptsAWellFormedProjectID(t *testing.T) {
	for _, project := range []string{
		"my-project",
		"proj-a-123456",
		"a12345",
		strings.Repeat("a", 30),
		"fulminate-services",
		"  my-project  ", // surrounding space is trimmed, not refused
	} {
		got, err := walk.Collector{Enumerations: fixedEnumerations()}.
			Walk(t.Context(), "gcp-instance", walk.Params{Project: project},
				framework.ForeignContext{})
		if err != nil {
			t.Errorf("the well-formed project id %q was refused: %v", project, err)
			continue
		}
		if !got.Complete.IsComplete() {
			t.Errorf("%q: a walk with no enumerations should be complete", project)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0

package glclients_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glclients"
)

// glclients_test.go — the credential resolution and the instance selector, both
// driven through an injected lookup so no test mutates the process environment.
//
// EVERY ASSERTION ABOUT THE TOKEN IS ABOUT A NON-REVERSIBLE PROPERTY: which
// source was consulted, and whether a value was accepted at all. A test that
// compared the resolved token against a literal would put a credential-shaped
// value in a test failure's output for no gain.

// lookupFrom builds an environment reader over a fixed map, recording which names
// were asked for.
func lookupFrom(env map[string]string, asked *[]string) glclients.Lookup {
	return func(name string) (string, bool) {
		*asked = append(*asked, name)
		value, ok := env[name]
		return value, ok
	}
}

func TestTheTokenIsResolvedInOrderAndTheFailureNamesBothVariables(t *testing.T) {
	for _, row := range []struct {
		name        string
		env         map[string]string
		wantSource  string
		wantRefusal bool
	}{
		{
			name:       "the primary alone",
			env:        map[string]string{"GITLAB_TOKEN": "primary-value"},
			wantSource: "GITLAB_TOKEN",
		},
		{
			name:       "the fallback alone",
			env:        map[string]string{"GITLAB_PRIVATE_TOKEN": "fallback-value"},
			wantSource: "GITLAB_PRIVATE_TOKEN",
		},
		{
			name: "both, and the primary wins",
			env: map[string]string{
				"GITLAB_TOKEN": "primary-value", "GITLAB_PRIVATE_TOKEN": "fallback-value",
			},
			wantSource: "GITLAB_TOKEN",
		},
		{
			name:        "neither",
			env:         map[string]string{},
			wantRefusal: true,
		},
		{
			// PRESENT AND EMPTY IS A MISSING TOKEN. The transport preserves the
			// distinction — a config entry may declare a name with an empty value —
			// so this arm has to be written rather than assumed.
			name:        "both present and empty",
			env:         map[string]string{"GITLAB_TOKEN": "", "GITLAB_PRIVATE_TOKEN": ""},
			wantRefusal: true,
		},
		{
			name: "the primary present and empty, the fallback set",
			env: map[string]string{
				"GITLAB_TOKEN": "", "GITLAB_PRIVATE_TOKEN": "fallback-value",
			},
			wantSource: "GITLAB_PRIVATE_TOKEN",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			var asked []string
			got, err := glclients.Token(lookupFrom(row.env, &asked))

			if row.wantRefusal {
				if err == nil {
					t.Fatal("a missing token was accepted; an unauthenticated walk of a group " +
						"returns a fraction of what a member sees and would land as a small group " +
						"rather than a failed collect")
				}
				for _, want := range []string{"GITLAB_TOKEN", "GITLAB_PRIVATE_TOKEN"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("the refusal does not name %q: %v", want, err)
					}
				}
				// AND IT TELLS THEM THE RIGHT THING TO DO. The entry carries a
				// REFERENCE to the variable and the value stays in the serving
				// environment; an error instructing an operator to write the
				// token into the entry as a value tells them to do what every one
				// of this collector's documents forbids, and what the installer
				// will not write.
				if !strings.Contains(err.Error(), "${"+enventry.PrimaryTokenVariable+"}") {
					t.Errorf("the refusal does not show the reference form the entry carries: %v", err)
				}
				for _, forbidden := range []string{"with your own token as the value", "as the value"} {
					if strings.Contains(err.Error(), forbidden) {
						t.Errorf("the refusal instructs a literal credential value (%q): %v",
							forbidden, err)
					}
				}
				if got != "" {
					t.Error("a refused resolution still returned a value")
				}
				return
			}

			if err != nil {
				t.Fatalf("a token that is set was refused: %v", err)
			}
			// THE ASSERTION IS ON WHICH SOURCE WAS CONSULTED, never on the value.
			if got == "" {
				t.Fatal("the resolver returned an empty token and no error")
			}
			if row.wantSource == "GITLAB_TOKEN" && len(asked) != 1 {
				t.Errorf("the resolver consulted %v; the primary answered, so the fallback should "+
					"not have been read", asked)
			}
			if row.wantSource == "GITLAB_PRIVATE_TOKEN" && len(asked) != 2 {
				t.Errorf("the resolver consulted %v, want both names in order", asked)
			}
		})
	}
}

// TestTheTokenValueNeverAppearsInARefusal. A message that quoted what it read
// would put the operator's credential into whatever collects diagnostics.
func TestTheTokenValueNeverAppearsInARefusal(t *testing.T) {
	const sentinel = "sentinel-token-value"
	var asked []string
	// Present and empty for both, so the refusal arm runs — with a THIRD name set
	// to the sentinel, standing for anything else in the environment.
	_, err := glclients.Token(lookupFrom(map[string]string{
		"GITLAB_TOKEN": "", "GITLAB_PRIVATE_TOKEN": "", "SOMETHING_ELSE": sentinel,
	}, &asked))
	if err == nil {
		t.Fatal("the refusal arm did not run")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Errorf("the refusal quoted a value from the environment: %v", err)
	}
}

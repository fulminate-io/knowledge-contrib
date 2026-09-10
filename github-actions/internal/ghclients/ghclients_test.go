// SPDX-License-Identifier: Apache-2.0

package ghclients_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghclients"
)

// ghclients_test.go — the credential resolution, on every arm.
//
// THE ENVIRONMENT IS A PARAMETER RATHER THAN THE PROCESS'S OWN. A test that set
// and unset real environment variables would be a test that could not run beside
// another one, and it would leave the variable set for whatever ran next in the
// same process. Taking the lookup as a function makes all five arms independent.
//
// NO ASSERTION HERE READS A TOKEN VALUE BACK. What is asserted is which source
// was consulted and whether the resolution succeeded, which is what the behavior
// is; asserting the value would put a credential-shaped string in a test's
// failure output for no gain.

// lookupOf builds an environment reader over a fixed map, distinguishing a name
// that is ABSENT from one that is present and EMPTY — a distinction the transport
// preserves, so this collector has to answer for it.
func lookupOf(env map[string]string) ghclients.Lookup {
	return func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	}
}

func TestTheTokenResolutionOnEveryArm(t *testing.T) {
	for _, row := range []struct {
		name    string
		env     map[string]string
		wantErr bool
		// wantSource names which variable's value should have won. It is compared
		// against a SENTINEL planted in that variable, never against a credential.
		wantSource string
	}{
		{
			name:       "the primary variable alone",
			env:        map[string]string{"GITHUB_TOKEN": "primary-sentinel"},
			wantSource: "primary-sentinel",
		},
		{
			name:       "the fallback variable alone",
			env:        map[string]string{"GH_TOKEN": "fallback-sentinel"},
			wantSource: "fallback-sentinel",
		},
		{
			name: "both set: the primary wins",
			env: map[string]string{
				"GITHUB_TOKEN": "primary-sentinel",
				"GH_TOKEN":     "fallback-sentinel",
			},
			wantSource: "primary-sentinel",
		},
		{
			name:    "neither set",
			env:     map[string]string{},
			wantErr: true,
		},
		{
			name: "PRESENT AND EMPTY IS A MISSING TOKEN. The transport preserves the " +
				"difference between an absent name and one set to the empty string, so a " +
				"config entry can hand this collector the name without the secret — and " +
				"that is the same input as no token at all as far as the provider is " +
				"concerned",
			env:     map[string]string{"GITHUB_TOKEN": "", "GH_TOKEN": ""},
			wantErr: true,
		},
		{
			name: "the primary present and empty falls through to the fallback",
			env: map[string]string{
				"GITHUB_TOKEN": "",
				"GH_TOKEN":     "fallback-sentinel",
			},
			wantSource: "fallback-sentinel",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, err := ghclients.Token(lookupOf(row.env))
			if row.wantErr {
				if err == nil {
					t.Fatal("a missing token was resolved rather than refused")
				}
				// THE FAILURE NAMES BOTH VARIABLES. An operator reading it has to
				// know which names to set, and naming only one sends them to the
				// wrong half of the answer.
				for _, name := range enventry.Names() {
					if !strings.Contains(err.Error(), name) {
						t.Errorf("the refusal does not name %q: %v", name, err)
					}
				}
				// AND IT TELLS THEM THE RIGHT THING TO DO. The entry carries a
				// REFERENCE to the variable and the value stays in the serving
				// environment; an error instructing an operator to write the
				// token into the entry as a value tells them to do what every
				// one of this collector's documents forbids, and what the
				// installer will not write.
				if !strings.Contains(err.Error(), "${"+enventry.PrimaryTokenVariable+"}") {
					t.Errorf("the refusal does not show the reference form the entry carries: %v", err)
				}
				for _, forbidden := range []string{"with your own token as the value", "as the value"} {
					if strings.Contains(err.Error(), forbidden) {
						t.Errorf("the refusal instructs a literal credential value (%q): %v",
							forbidden, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("resolving the token: %v", err)
			}
			if got != row.wantSource {
				t.Errorf("the value came from the wrong variable: got %q, want the one planted "+
					"as %q", got, row.wantSource)
			}
		})
	}
}

// TestTheResolutionConsultsNothingButTheDeclaredNames is the closure assertion at
// the read site: a name the config entry does not declare is a name the child
// process does not have, so a reader that consulted a third one would fail on
// every correctly configured machine.
func TestTheResolutionConsultsNothingButTheDeclaredNames(t *testing.T) {
	var consulted []string
	_, _ = ghclients.Token(func(name string) (string, bool) {
		consulted = append(consulted, name)
		return "", false
	})

	if len(consulted) == 0 {
		t.Fatal("the resolution consulted no variable at all")
	}
	for _, name := range consulted {
		if !enventry.IsDeclared(name) {
			t.Errorf("the resolution consulted %q, which this collector's entry does not declare",
				name)
		}
	}
	// And it consulted BOTH, in order: a reader that stopped at the first would
	// work on half the machines it is installed on.
	if len(consulted) != 2 || consulted[0] != enventry.PrimaryTokenVariable ||
		consulted[1] != enventry.FallbackTokenVariable {
		t.Errorf("the resolution consulted %v, want %v in that order",
			consulted, enventry.Names())
	}
}

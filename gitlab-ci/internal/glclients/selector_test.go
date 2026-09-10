// SPDX-License-Identifier: Apache-2.0

package glclients_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glclients"
)

// selector_test.go — the INSTANCE SELECTOR, which is a choice of host and not a
// credential.
//
// IT IS SPLIT FROM THE TOKEN ARMS DELIBERATELY. The two are read from the same
// environment by the same lookup, and that is the whole reason to keep their
// assertions apart: a selector is written into a config file as a literal and a
// token never is, so a reader who conflates them writes the wrong install row.

// TestTheInstanceSelectorHasFourArms.
//
// UNSET AND SET-TO-THE-DEFAULT ARE INDISTINGUISHABLE, which is the arm that
// catches a naive rewrite: the source provider applies the base-URL option only
// when the value DIFFERS from the default, so a rewrite reading "if the variable
// is non-empty" would change what the SDK is handed for an operator who wrote the
// hosted URL out.
func TestTheInstanceSelectorHasFourArms(t *testing.T) {
	for _, row := range []struct {
		name, value string
		set         bool
		want        string
	}{
		{name: "unset", set: false, want: glclients.DefaultBaseURL},
		{name: "present and empty", value: "", set: true, want: glclients.DefaultBaseURL},
		{
			name:  "set to exactly the default, trailing slash and all",
			value: glclients.DefaultBaseURL, set: true, want: glclients.DefaultBaseURL,
		},
		{
			name:  "a self-hosted instance",
			value: "https://gitlab.example.com/", set: true,
			want: "https://gitlab.example.com/",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			env := map[string]string{}
			if row.set {
				env[enventry.BaseURLVariable] = row.value
			}
			var asked []string
			got, err := glclients.BaseURL(lookupFrom(env, &asked))
			if err != nil {
				t.Fatalf("resolving the selector: %v", err)
			}
			if got != row.want {
				t.Errorf("the base URL is %q, want %q", got, row.want)
			}
		})
	}
}

// TestASelectorCarryingUserinfoIsRefusedAndNotQuotedBack.
//
// A URL of the form https://user:token@host embeds a credential. The install
// script refuses to WRITE one; what this module owes is that it does not undo
// that refusal by dialing with one — and that its own message does not print the
// value back, because a refusal that quotes the URL puts the credential in a log.
func TestASelectorCarryingUserinfoIsRefusedAndNotQuotedBack(t *testing.T) {
	const embedded = "s3cr3t-embedded-token"
	var asked []string
	got, err := glclients.BaseURL(lookupFrom(map[string]string{
		enventry.BaseURLVariable: "https://someone:" + embedded + "@gitlab.example.com/",
	}, &asked))

	if err == nil {
		t.Fatalf("a base URL carrying userinfo was accepted and would be dialed with: %q", got)
	}
	if strings.Contains(err.Error(), embedded) {
		t.Errorf("the refusal quoted the embedded credential: %v", err)
	}
	if strings.Contains(err.Error(), "someone") {
		t.Errorf("the refusal quoted the userinfo: %v", err)
	}
	for _, want := range []string{enventry.BaseURLVariable, "gitlab.example.com"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q, so an operator cannot tell which value to "+
				"fix: %v", want, err)
		}
	}
}

// TestASelectorThatIsNotAURLIsRefused. Bad input always errors, and a value that
// names no host would otherwise be handed to the SDK to fail on later with a
// message about a request rather than about a setting.
func TestASelectorThatIsNotAURLIsRefused(t *testing.T) {
	for _, value := range []string{"gitlab.example.com", "not a url at all", "://missing-scheme"} {
		var asked []string
		if _, err := glclients.BaseURL(lookupFrom(map[string]string{
			enventry.BaseURLVariable: value,
		}, &asked)); err == nil {
			t.Errorf("the selector %q was accepted; it names no scheme or no host", value)
		}
	}
	// The known positive: a well-formed URL is accepted in the same run.
	var asked []string
	if _, err := glclients.BaseURL(lookupFrom(map[string]string{
		enventry.BaseURLVariable: "https://gitlab.example.com/",
	}, &asked)); err != nil {
		t.Errorf("a well-formed selector was refused: %v", err)
	}
}

// TestTheDefaultCarriesItsTrailingSlash. The selector is applied only when it
// differs from this string, so the two have to agree byte for byte with what the
// source provider wrote.
func TestTheDefaultCarriesItsTrailingSlash(t *testing.T) {
	if glclients.DefaultBaseURL != "https://gitlab.com/" {
		t.Errorf("the default base URL is %q, want the source provider's own literal",
			glclients.DefaultBaseURL)
	}
	if !strings.HasSuffix(glclients.DefaultBaseURL, "/") {
		t.Error("the default base URL has lost its trailing slash; the comparison that decides " +
			"whether an override is applied is exact")
	}
}

// TestTheProviderIsBuiltOnlyWithACredential drives the one function that reads
// the real process environment, which is the wiring every other row here bypasses
// by injecting a lookup.
//
// IT USES THE TESTING PACKAGE'S OWN SCOPED SETTER, so the values live for this
// test and are restored after it; nothing here reads or writes any environment
// outside this process.
func TestTheProviderIsBuiltOnlyWithACredential(t *testing.T) {
	t.Setenv(enventry.PrimaryTokenVariable, "")
	t.Setenv(enventry.FallbackTokenVariable, "")
	t.Setenv(enventry.BaseURLVariable, "")

	if _, _, err := glclients.API(t.Context()); err == nil {
		t.Fatal("the provider was built with no credential in the environment")
	}

	t.Setenv(enventry.PrimaryTokenVariable, "a-token-for-this-test-only")
	api, release, err := glclients.API(t.Context())
	if err != nil {
		t.Fatalf("the provider was refused with a token set: %v", err)
	}
	if release == nil {
		t.Error("the builder returned no release function; the walk's contract is that what it " +
			"opens it releases")
	} else {
		release()
	}
	for name, seam := range map[string]any{
		"groups": api.Groups, "runners": api.Runners, "environments": api.Environments,
		"deployments": api.Deployments, "pipelines": api.Pipelines, "files": api.Files,
		"variables": api.Variables,
	} {
		if seam == nil {
			t.Errorf("the %s seam was not wired; an enumeration through it would panic on the "+
				"first call rather than fail the collect", name)
		}
	}
}

// TestASelfHostedSelectorIsAcceptedByTheBuilder is the self-hosted arm end to
// end, driven with a SCRATCH URL and never with a credential.
func TestASelfHostedSelectorIsAcceptedByTheBuilder(t *testing.T) {
	t.Setenv(enventry.PrimaryTokenVariable, "a-token-for-this-test-only")
	t.Setenv(enventry.BaseURLVariable, "https://gitlab.example.com/")

	if _, _, err := glclients.API(t.Context()); err != nil {
		t.Fatalf("a self-hosted instance selector was refused: %v", err)
	}

	// And the userinfo refusal reaches this path too, which is what keeps the
	// install script's own guard from being undone here.
	t.Setenv(enventry.BaseURLVariable, "https://someone:secret@gitlab.example.com/")
	if _, _, err := glclients.API(t.Context()); err == nil {
		t.Fatal("a base URL carrying userinfo reached the client builder")
	}
}

// TestTheOverrideIsAppliedOnlyForAnInstanceOtherThanTheDefault. Whether the SDK
// is handed a base URL at all is not inspectable after the fact, so the decision
// is a predicate and this is what drives it.
func TestTheOverrideIsAppliedOnlyForAnInstanceOtherThanTheDefault(t *testing.T) {
	for _, row := range []struct {
		baseURL string
		want    bool
	}{
		{glclients.DefaultBaseURL, false},
		// The SAME host without the trailing slash IS an override, which is the
		// behavior the exact comparison produces and the source provider's own.
		{"https://gitlab.com", true},
		{"https://gitlab.example.com/", true},
	} {
		if got := glclients.OverridesDefault(row.baseURL); got != row.want {
			t.Errorf("OverridesDefault(%q) is %v, want %v", row.baseURL, got, row.want)
		}
	}
	// And an UNSET selector resolves to the default, so it applies no override —
	// which is the arm a naive "if the variable is non-empty" rewrite breaks.
	var asked []string
	resolved, err := glclients.BaseURL(lookupFrom(map[string]string{}, &asked))
	if err != nil {
		t.Fatalf("resolving an unset selector: %v", err)
	}
	if glclients.OverridesDefault(resolved) {
		t.Errorf("an unset selector resolved to %q, which applies an override", resolved)
	}
}

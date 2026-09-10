// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
)

// empty_sensitive_test.go — THIS COLLECTOR DISCRIMINATES ON NOTHING, and this is
// the run that says so rather than the assumption.
//
// WHAT THE CLAIM IS. For every environment name this collector declares, a value
// PRESENT AND EMPTY produces a byte-identical resolution to the name being
// ABSENT. That is what makes the defaulted `${NAME:-}` form safe in this
// collector's worked entry, which is the form its README ships and the ticket
// keeps; the describe declaration therefore marks no name, and the installer's
// documentation gate admits the entry.
//
// AN ABSENCE CLAIM NEEDS A CONTROL, AND ONE BLANKET CONTROL IS NOT ENOUGH. The
// AWS credential chain has STEPS, and a control that only moves one of them
// leaves every other step's zero unexplained: a sweep over fifty names through a
// resolver that stopped reading anything reports the same clean zero. So the
// controls below are PER CHAIN STEP — the environment provider, the
// shared-credentials file, web identity, the container endpoint and the
// shared-config profile — and each is a non-empty value that DOES move the
// resolution in the same run.
//
// WEB IDENTITY TAKES BOTH OF ITS NAMES, because the pair is what the provider
// actually needs: AWS_ROLE_ARN alone moves the resolved RoleARN field and
// nothing else, so a control written with it would observe a field rather than
// the step. Setting both is what makes this control the step's.
//
// THE THREE SECRET-CLASS NAMES ARE DRIVEN LIKE EVERY OTHER NAME AND NO VALUE OF
// ONE IS EVER REPORTED. Where a credential-shaped value is needed for a control,
// the assertion is on a NON-REVERSIBLE property — that the resolved field is
// non-empty and of the length the fixture set — never on the value itself.

// envConfigOutcome is what the AWS configuration package resolves from the
// process environment, reduced to a comparable value. It is the same entry point
// the collector's own chain uses, so a difference here is a difference the
// collector would see.
func envConfigOutcome(t *testing.T) string {
	t.Helper()
	cfg, err := config.NewEnvConfig()
	if err != nil {
		return "error: " + err.Error()
	}
	// Credential VALUES never enter this string. The two credential fields are
	// reduced to lengths, which is a non-reversible property and enough to observe
	// a change; every other field is a selector or a path.
	return fmt.Sprintf(
		"region=%s|profile=%s|creds(idlen=%d,secretlen=%d,tokenlen=%d,src=%s)|"+
			"sharedcreds=%v|sharedcfg=%v|roleARN=%s|roleSession=%s|tokenfile=%s|containerURI=%s|containerRelURI=%s|"+
			"cabundle=%s|maxattempts=%s|retrymode=%v|endpoint=%s",
		cfg.Region, cfg.SharedConfigProfile,
		len(cfg.Credentials.AccessKeyID), len(cfg.Credentials.SecretAccessKey), len(cfg.Credentials.SessionToken),
		cfg.Credentials.Source,
		cfg.SharedCredentialsFile, cfg.SharedConfigFile,
		cfg.RoleARN, cfg.RoleSessionName, cfg.WebIdentityTokenFilePath,
		cfg.ContainerCredentialsEndpoint, cfg.ContainerCredentialsRelativePath,
		cfg.CustomCABundle, cfg.SharedConfigProfile, cfg.RetryMode, cfg.BaseEndpoint)
}

// homeOutcome is the one name the AWS configuration package does not resolve
// itself: HOME reaches the chain through os.UserHomeDir, which the shared-config
// loader calls to find ~/.aws. It is measured on its own for that reason.
func homeOutcome() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "error: " + err.Error()
	}
	return "home: " + dir
}

// clearDeclared removes every declared name from the process environment for the
// duration of the test, restoring each afterwards, so each arm starts from the
// same baseline whatever the developer's shell holds. t.Setenv registers the
// restoration; the Unsetenv is what makes the baseline ABSENT rather than empty.
func clearDeclared(t *testing.T, names []string) {
	t.Helper()
	for _, name := range names {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

func TestNoDeclaredNameTellsEmptyApartFromAbsent(t *testing.T) {
	// THE TARGET IS NAMED, not taken from the runner. The declared set differs by
	// target — POSIX carries HOME and Windows carries USERPROFILE — so a sweep
	// taking the host's own OS measures a different denominator on a different
	// machine.
	const target = TargetPOSIX
	declared := DeclaredEnv(target)
	if len(declared) == 0 {
		t.Fatalf("this collector declares no names for %s; the sweep would pass vacuously", target)
	}
	clearDeclared(t, declared)

	envBaseline := envConfigOutcome(t)
	homeBaseline := homeOutcome()

	var differ []string
	for _, name := range declared {
		os.Setenv(name, "")
		got, gotHome := envConfigOutcome(t), homeOutcome()
		os.Unsetenv(name)
		if got != envBaseline || gotHome != homeBaseline {
			differ = append(differ, name)
		}
	}
	slices.Sort(differ)
	if len(differ) != 0 {
		t.Errorf("%d of %d declared names tell present-and-empty apart from absent: %v\n"+
			"this collector's worked entry renders the defaulted ${NAME:-} form for every declared name, so a "+
			"discriminating name means an operator who copies that entry gets a broken collect; either the name is "+
			"marked empty_sensitive in describe_env.go and the entry stops showing the defaulted form for it, or "+
			"the reader that started discriminating is the defect", len(differ), len(declared), differ)
	}

	// AND THE DECLARATION AGREES, which is the half the installer reads. The two
	// sides are derived from different places: the sweep above from the resolver,
	// this from describe_env.go.
	var marked []string
	for _, e := range describedEnvironment() {
		if e.EmptySensitive {
			marked = append(marked, e.Name)
		}
	}
	if len(marked) != 0 {
		t.Errorf("no declared name was measured to discriminate, but the declaration marks %v", marked)
	}

	// THE PER-CHAIN-STEP CONTROLS. Each is a NON-EMPTY value that must move the
	// outcome, in this run, through the same instrument the sweep used.
	t.Run("controls", func(t *testing.T) {
		dir := t.TempDir()
		for _, tc := range []struct {
			step string
			set  map[string]string
		}{
			{
				// The ENVIRONMENT PROVIDER step. The two halves are set together
				// because the provider requires both to produce a credential; the
				// assertion downstream reads their LENGTHS, never their values.
				step: "environment credential provider",
				set: map[string]string{
					"AWS_ACCESS_KEY_ID":     "AKIAIOSFODNN7EXAMPL",
					"AWS_SECRET_ACCESS_KEY": "0123456789012345678901234567890123456789",
				},
			},
			{step: "region selector", set: map[string]string{"AWS_REGION": "eu-west-2"}},
			{step: "shared-config profile", set: map[string]string{"AWS_PROFILE": "a-profile"}},
			{
				step: "shared-credentials file",
				set:  map[string]string{"AWS_SHARED_CREDENTIALS_FILE": filepath.Join(dir, "credentials")},
			},
			{
				// WEB IDENTITY TAKES BOTH NAMES, so the control observes the step
				// rather than one field of it.
				step: "web identity",
				set: map[string]string{
					"AWS_ROLE_ARN":                "arn:aws:iam::000000000000:role/example",
					"AWS_WEB_IDENTITY_TOKEN_FILE": filepath.Join(dir, "token"),
				},
			},
			{
				step: "container credential endpoint",
				set:  map[string]string{"AWS_CONTAINER_CREDENTIALS_FULL_URI": "http://169.254.170.2/v2/credentials/x"},
			},
		} {
			t.Run(tc.step, func(t *testing.T) {
				for k, v := range tc.set {
					os.Setenv(k, v)
				}
				got := envConfigOutcome(t)
				for k := range tc.set {
					os.Unsetenv(k)
				}
				if got == envBaseline {
					t.Errorf("control: the %s step did not move when its names were set to real values; "+
						"the sweep's zero for that step is unexplained", tc.step)
				}
			})
		}

		// The home arm's own control, through its own reader.
		t.Run("home directory", func(t *testing.T) {
			os.Setenv("HOME", dir)
			got := homeOutcome()
			os.Unsetenv("HOME")
			if got == homeBaseline {
				t.Error("control: HOME set to a real directory did not move os.UserHomeDir; " +
					"the sweep's zero for that name is unexplained")
			}
		})
	})
}

// TestTheAWSProfileNameSelectsTheDefaultProfileWhetherEmptyOrAbsent is the
// measurement the module comment about AWS_PROFILE rests on, driven on its own
// because the comment makes a claim about WHAT THE SDK DOES rather than about
// two strings being equal.
//
// The claim is that empty and absent both select the DEFAULT profile — not that
// the SDK ignores the name. An empty AWS_PROFILE leaves the resolved profile
// empty, and an empty profile is what the shared-config loader reads as
// `default`; an absent one does the same. A value that is neither selects that
// profile, which is the same-run control.
func TestTheAWSProfileNameSelectsTheDefaultProfileWhetherEmptyOrAbsent(t *testing.T) {
	clearDeclared(t, DeclaredEnv(TargetPOSIX))

	absent, err := config.NewEnvConfig()
	if err != nil {
		t.Fatalf("resolving with AWS_PROFILE absent: %v", err)
	}
	os.Setenv("AWS_PROFILE", "")
	empty, err := config.NewEnvConfig()
	os.Unsetenv("AWS_PROFILE")
	if err != nil {
		t.Fatalf("resolving with AWS_PROFILE present and empty: %v", err)
	}
	if absent.SharedConfigProfile != empty.SharedConfigProfile {
		t.Errorf("AWS_PROFILE absent resolves the profile %q and present-and-empty resolves %q; "+
			"the module comment says both select the default", absent.SharedConfigProfile, empty.SharedConfigProfile)
	}
	if absent.SharedConfigProfile != "" {
		t.Errorf("with AWS_PROFILE absent the resolved profile is %q, not empty; "+
			"the claim that an empty value selects the default rests on an empty resolved profile meaning `default`",
			absent.SharedConfigProfile)
	}
	// The same-run known positive: a real value DOES select that profile, so the
	// row above is not passing on a resolver that reads nothing.
	os.Setenv("AWS_PROFILE", "some-other-profile")
	set, err := config.NewEnvConfig()
	os.Unsetenv("AWS_PROFILE")
	if err != nil {
		t.Fatalf("resolving with AWS_PROFILE set: %v", err)
	}
	if set.SharedConfigProfile != "some-other-profile" {
		t.Errorf("control: AWS_PROFILE set to a real name resolved the profile %q", set.SharedConfigProfile)
	}
}

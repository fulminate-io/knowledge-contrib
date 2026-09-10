// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"golang.org/x/net/http/httpproxy"
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
// resolution has arms, and a control that only moves one arm leaves every other
// arm's zero unexplained: a sweep over nineteen names through a resolver that
// stopped reading anything reports the same clean zero. So the controls below are
// PER ARM — the in-cluster pair, the kubeconfig path, the home directory, and the
// proxy family separately — and each is a non-empty value that DOES move the
// result in the same run.
//
// TWO ARMS OF NAMES TAKE A DIFFERENT GROUND, AND THEY LIVE IN
// empty_sensitive_unmeasured_test.go rather than here. Some names are inert
// because a reader on this path treats empty as absent, which is what this file
// measures; others are inert STRUCTURALLY, because nothing on this path consults
// them in any state, and a third set has no live control available on any host
// this suite runs on. A test that reported all three on the same ground would be
// asserting something it did not measure, so the grounds are kept apart.

// resolutionOutcome drives this collector's own credential resolution and reduces
// the result to a comparable value.
func resolutionOutcome(t *testing.T) string {
	t.Helper()
	cred, err := resolveCredential("", defaultCredentialSources())
	if err != nil {
		return "error: " + err.Error()
	}
	host := ""
	if cred.Config != nil {
		host = cred.Config.Host
	}
	return fmt.Sprintf("resolved: context=%s host=%s", cred.ContextName, host)
}

// proxyOutcome drives the proxy family through the same resolver net/http uses,
// against a NON-LOOPBACK target.
//
// THE TARGET IS NOT LOOPBACK, AND THAT IS LOAD-BEARING. Go bypasses a configured
// proxy for loopback addresses, so a probe pointed at 127.0.0.1 reports "no
// proxy" whatever the environment holds and every control in this arm is dead.
func proxyOutcome(t *testing.T) string {
	t.Helper()
	cfg := httpproxy.FromEnvironment()
	return fmt.Sprintf("http=%s https=%s no=%s", cfg.HTTPProxy, cfg.HTTPSProxy, cfg.NoProxy)
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
	// THE TARGETS ARE NAMED, AND THERE ARE TWO. envAllowlist returns a different
	// set per target — measured, 17 on darwin, 19 on linux, 20 on windows — so a
	// sweep taking runtime.GOOS alone drives a different set on every machine and
	// leaves the linux-only names, the two unix trust roots, unswept on a
	// developer's laptop. That matters here specifically because those are the
	// names crypto/x509 reads on linux and not on darwin. So the sweep drives the
	// HOST's set, which is what this build can actually receive, UNIONED with
	// linux's, which is the release and CI target.
	//
	// THE ASSERTION IS A ZERO, NEVER A COUNT, which is what makes the union safe:
	// setting a name no reader on this platform consults is inert, so a name swept
	// on the wrong host adds coverage rather than a false positive, and no
	// expectation moves with the machine.
	declared := envAllowlist(runtime.GOOS)
	for _, name := range envAllowlist("linux") {
		if !slices.Contains(declared, name) {
			declared = append(declared, name)
		}
	}
	slices.Sort(declared)
	if len(declared) == 0 {
		t.Fatal("this collector declares no environment names; the sweep would pass vacuously")
	}
	clearDeclared(t, declared)

	// THE BACKGROUND RESOLVES A REAL CLIENT, and that is what every comparison
	// below is taken against. Without it each arm returns at the kubeconfig read
	// error, `clientcmd.NewDefaultClientConfig` is never reached, and a name that
	// moved a BUILT configuration and nothing else would be invisible — the zero
	// would be taken against a refusal string that could not have moved anyway.
	home := t.TempDir()
	kubeconfig := writeMinimalKubeconfigUnderHome(t, home)

	// THE BASELINE IS PER NAME, not one global one, and it has to be: the
	// background is itself supplied by two of the declared names. Comparing a
	// name's empty arm against a baseline in which that same name is SET would
	// compare set-versus-empty and report the wrong answer for the source names.
	// So each name is driven in both of its own states, against a background
	// carried by whichever of the two sources it does not occupy.
	outcomeFor := func(name string, present bool) (string, string) {
		for _, n := range declared {
			os.Unsetenv(n)
		}
		if name == "KUBECONFIG" {
			os.Setenv("HOME", home)
		} else {
			os.Setenv("KUBECONFIG", kubeconfig)
		}
		if present {
			os.Setenv(name, "")
		} else {
			os.Unsetenv(name)
		}
		return resolutionOutcome(t), proxyOutcome(t)
	}

	// The background's own control, taken through the same driver before any
	// comparison: with a name nothing in this module reads left absent, the
	// resolution reaches a built client. It is also the value the per-arm controls
	// below are compared against, so they and the sweep share one reference.
	credBaseline, proxyBaseline := outcomeFor("POD_NAMESPACE", false)
	assertResolvesTheFixture(t, "the sweep's background", credBaseline)

	var differ []string
	for _, name := range declared {
		absentCred, absentProxy := outcomeFor(name, false)
		emptyCred, emptyProxy := outcomeFor(name, true)
		// EVERY ARM MUST HAVE RESOLVED, or its zero is the weak one again. HOME is
		// the one name whose absent arm cannot: with KUBECONFIG supplying the
		// background it still resolves, which is why the row below drives HOME as
		// the SOLE source separately.
		assertResolvesTheFixture(t, name+" absent", absentCred)
		if absentCred != emptyCred || absentProxy != emptyProxy {
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
	// sides are derived from different places: the sweep above from the resolvers,
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

	// HOME AS THE SOLE SOURCE, driven on its own because the sweep above cannot.
	// The sweep's background is supplied through KUBECONFIG, which takes
	// precedence, so HOME's own two states are masked there. With KUBECONFIG
	// absent, HOME is the only source — and neither of its states can reach a
	// built client, because an absent HOME names no path at all. The row is
	// therefore a comparison of two REFUSALS, and it says so rather than
	// borrowing the resolved-path ground the rest of the sweep stands on.
	t.Run("HOME as the sole kubeconfig source", func(t *testing.T) {
		for _, n := range declared {
			os.Unsetenv(n)
		}
		absent := resolutionOutcome(t)
		os.Setenv("HOME", "")
		empty := resolutionOutcome(t)
		os.Unsetenv("HOME")
		if absent != empty {
			t.Errorf("HOME present-and-empty is not the same as absent when it is the only kubeconfig source\n"+
				"absent: %s\nempty:  %s", absent, empty)
		}
		// The same-run positive that makes the equality above mean something: a
		// REAL home does reach a built client through this same driver.
		os.Setenv("HOME", home)
		set := resolutionOutcome(t)
		os.Unsetenv("HOME")
		assertResolvesTheFixture(t, "HOME set to a home carrying the fixture", set)
	})

	// THE PER-ARM CONTROLS. Each is a NON-EMPTY value that must move the outcome,
	// in this run, through the same instrument the sweep used, and each is
	// compared against the SAME resolved-client baseline. Without them the zero
	// above is indistinguishable from a resolver that reads nothing.
	t.Run("controls", func(t *testing.T) {
		for _, tc := range []struct {
			arm    string
			set    map[string]string
			drop   []string
			out    func(*testing.T) string
			before string
		}{
			{
				// The in-cluster arm: either name alone reaches the half-declared
				// refusal, which is a different outcome from a built client.
				arm: "in-cluster service host", set: map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1"},
				out: resolutionOutcome, before: credBaseline,
			},
			{
				arm: "in-cluster service port", set: map[string]string{"KUBERNETES_SERVICE_PORT": "443"},
				out: resolutionOutcome, before: credBaseline,
			},
			{
				// The kubeconfig arm, moved by a path that does not exist.
				arm: "kubeconfig path", set: map[string]string{"KUBECONFIG": filepath.Join(t.TempDir(), "nope")},
				out: resolutionOutcome, before: credBaseline,
			},
			{
				// The home arm. It DROPS the background's KUBECONFIG first, because
				// that variable takes precedence and would otherwise keep the
				// outcome fixed however HOME moved — a control that cannot move is
				// the failure a control exists to prevent.
				arm: "home directory", set: map[string]string{"HOME": t.TempDir()},
				drop: []string{"KUBECONFIG"},
				out:  resolutionOutcome, before: credBaseline,
			},
			{
				// The proxy family, through its own resolver and against a
				// non-loopback target.
				arm: "proxy family", set: map[string]string{"HTTP_PROXY": "http://proxy.example.invalid:3128"},
				out: proxyOutcome, before: proxyBaseline,
			},
		} {
			t.Run(tc.arm, func(t *testing.T) {
				for _, n := range declared {
					os.Unsetenv(n)
				}
				os.Setenv("KUBECONFIG", kubeconfig)
				for _, k := range tc.drop {
					os.Unsetenv(k)
				}
				for k, v := range tc.set {
					os.Setenv(k, v)
				}
				got := tc.out(t)
				for k := range tc.set {
					os.Unsetenv(k)
				}
				t.Logf("%s arm: baseline=%q moved-to=%q", tc.arm, tc.before, got)
				if got == tc.before {
					t.Errorf("control: the %s arm did not move when its name was set to a real value; "+
						"the sweep's zero for that arm is unexplained", tc.arm)
				}
			})
		}
	})
}

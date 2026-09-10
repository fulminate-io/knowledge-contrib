// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"strings"
	"testing"
	"time"
)

// env_test.go — the environment this collector reads, and every refusal it
// owes.
//
// EVERY CASE SUPPLIES ITS OWN ENVIRONMENT rather than mutating the process's.
// os.Setenv is a process-global write that any parallel test observes, and a
// test that set LOKI_TLS_SKIP_VERIFY would change what an unrelated test's
// client dialed.

// TestNamesIsTheWholeSurfaceThisCollectorReads is the declaration census: the
// eighteen Loki names, the six proxy delegates and the two trust roots, and
// nothing else.
func TestNamesIsTheWholeSurfaceThisCollectorReads(t *testing.T) {
	got := Names()
	want := []string{
		"LOKI_USERNAME", "LOKI_PASSWORD", "LOKI_BEARER_TOKEN", "LOKI_BEARER_TOKEN_FILE",
		"LOKI_AUTH_HEADER", "LOKI_ORG_ID",
		"LOKI_CA_CERT_PATH", "LOKI_TLS_SKIP_VERIFY", "LOKI_CLIENT_CERT_PATH", "LOKI_CLIENT_KEY_PATH",
		"LOKI_HTTP_PROXY_URL", "LOKI_ENV_PROXY", "LOKI_HTTP_COMPRESSION", "LOKI_NO_CACHE",
		"LOKI_QUERY_TAGS", "LOKI_CLIENT_RETRIES", "LOKI_CLIENT_MIN_BACKOFF", "LOKI_CLIENT_MAX_BACKOFF",
		"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy",
		"SSL_CERT_FILE", "SSL_CERT_DIR",
	}
	if len(got) != len(want) {
		t.Fatalf("Names() has %d entries, want %d:\n got %v\nwant %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// LOKI_ADDR IS DELIBERATELY ABSENT: the endpoint is a tool parameter, and a
	// collector that also read it from the environment would give an operator
	// two places to set one thing.
	for _, name := range got {
		if name == "LOKI_ADDR" {
			t.Fatal("LOKI_ADDR is in the declared set; the address is a tool parameter")
		}
	}

	// The returned slice is a copy: mutating it must not reach the package.
	got[0] = "MUTATED"
	if Names()[0] != "LOKI_USERNAME" {
		t.Fatal("Names() hands out the package's own slice")
	}
}

// TestTheTrustRootsAreDeclaredUnconditionally. They are NOT gated on the proxy
// flag and NOT conditional on the platform: what the config entry carries is
// what the child receives, and the platform decides only whether the standard
// library consults the value.
func TestTheTrustRootsAreDeclaredUnconditionally(t *testing.T) {
	declared := map[string]bool{}
	for _, n := range Names() {
		declared[n] = true
	}
	for _, name := range []string{"SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if !declared[name] {
			t.Fatalf("%s is not declared", name)
		}
	}
	// The six proxy delegates are declared for the same reason: honoring
	// LOKI_ENV_PROXY without them would make the flag decide nothing, because
	// the child's environment is the entry's block and net/http reads them from
	// the process environment.
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		if !declared[name] {
			t.Fatalf("%s is not declared, but LOKI_ENV_PROXY is honored", name)
		}
	}
}

func TestSettingsDefaultsWithAnEmptyEnvironment(t *testing.T) {
	s, err := LoadSettings(mapLookup(nil))
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.AuthHeader != "Authorization" {
		t.Fatalf("AuthHeader = %q, want the default %q", s.AuthHeader, "Authorization")
	}
	if s.ClientRetries != 0 {
		t.Fatalf("ClientRetries = %d, want 0", s.ClientRetries)
	}
	if s.ClientMinBackoff != 500*time.Millisecond || s.ClientMaxBackoff != 5*time.Second {
		t.Fatalf("backoff defaults = %s..%s, want 500ms..5s", s.ClientMinBackoff, s.ClientMaxBackoff)
	}
	if s.TLSSkipVerify || s.EnvProxy || s.HTTPCompression || s.NoCache {
		t.Fatalf("a boolean defaulted to true: %+v", s)
	}
}

func TestSettingsReadEveryDeclaredValue(t *testing.T) {
	env := map[string]string{
		EnvUsername: "u", EnvPassword: "p", EnvAuthHeader: "X-Auth", EnvOrgID: "tenant-1",
		EnvCACertPath: "/ca.pem", EnvTLSSkipVerify: "true",
		EnvClientCertPath: "/c.pem", EnvClientKeyPath: "/k.pem",
		EnvHTTPProxyURL: "http://proxy:3128", EnvEnvProxy: "true",
		EnvHTTPCompression: "true", EnvNoCache: "1", EnvQueryTags: "team=platform",
		EnvClientRetries: "4", EnvClientMinBackoff: "10ms", EnvClientMaxBackoff: "1s",
	}
	s, err := LoadSettings(mapLookup(env))
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{EnvUsername, s.Username, "u"},
		{EnvPassword, s.Password, "p"},
		{EnvAuthHeader, s.AuthHeader, "X-Auth"},
		{EnvOrgID, s.OrgID, "tenant-1"},
		{EnvCACertPath, s.CACertPath, "/ca.pem"},
		{EnvTLSSkipVerify, s.TLSSkipVerify, true},
		{EnvClientCertPath, s.ClientCertPath, "/c.pem"},
		{EnvClientKeyPath, s.ClientKeyPath, "/k.pem"},
		{EnvHTTPProxyURL, s.HTTPProxyURL, "http://proxy:3128"},
		{EnvEnvProxy, s.EnvProxy, true},
		{EnvHTTPCompression, s.HTTPCompression, true},
		{EnvNoCache, s.NoCache, true},
		{EnvQueryTags, s.QueryTags, "team=platform"},
		{EnvClientRetries, s.ClientRetries, 4},
		{EnvClientMinBackoff, s.ClientMinBackoff, 10 * time.Millisecond},
		{EnvClientMaxBackoff, s.ClientMaxBackoff, time.Second},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Fatalf("%s produced %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestAMalformedValueErrorsNamingTheVariableAndTheValue is the bad-input arm.
// Each case would otherwise be a silent coercion: a boolean read as false, a
// count read as zero, a duration read as none.
func TestAMalformedValueErrorsNamingTheVariableAndTheValue(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		value string
	}{
		{EnvTLSSkipVerify, map[string]string{EnvTLSSkipVerify: "yes"}, "yes"},
		{EnvEnvProxy, map[string]string{EnvEnvProxy: "on"}, "on"},
		{EnvHTTPCompression, map[string]string{EnvHTTPCompression: "gzip"}, "gzip"},
		{EnvNoCache, map[string]string{EnvNoCache: ""}, ""},
		{EnvClientRetries, map[string]string{EnvClientRetries: "three"}, "three"},
		{EnvClientRetries, map[string]string{EnvClientRetries: "-1"}, "-1"},
		{EnvClientMinBackoff, map[string]string{EnvClientMinBackoff: "500"}, "500"},
		{EnvClientMaxBackoff, map[string]string{EnvClientMaxBackoff: "-1s"}, "-1s"},
	}
	for _, tc := range cases {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			_, err := LoadSettings(mapLookup(tc.env))
			if err == nil {
				t.Fatalf("%s=%q was accepted", tc.name, tc.value)
			}
			if !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("the error does not name the variable: %v", err)
			}
			if !strings.Contains(err.Error(), tc.value) {
				t.Fatalf("the error does not name the value: %v", err)
			}
		})
	}
}

// TestASetButEmptyBooleanIsRefusedRatherThanDefaulted is the cell that
// separates "never set" from "cleared". An operator who wrote
// LOKI_TLS_SKIP_VERIFY= in their entry meant something; taking the default
// there would decide it for them silently.
func TestASetButEmptyBooleanIsRefusedRatherThanDefaulted(t *testing.T) {
	if _, err := LoadSettings(mapLookup(map[string]string{EnvTLSSkipVerify: ""})); err == nil {
		t.Fatal("a set-but-empty boolean was accepted as the default")
	}
	// The control in the same run: the same name UNSET takes the default.
	s, err := LoadSettings(mapLookup(nil))
	if err != nil {
		t.Fatalf("LoadSettings with the name unset: %v", err)
	}
	if s.TLSSkipVerify {
		t.Fatal("the unset default is not false")
	}
}

// TestAHalfSetCredentialPairIsRefusedNamingTheMissingHalf covers every arm.
// Each one is a nil-auth request where the operator asked for an authenticated
// one, which is exactly what R4's fail-loud clause exists to prevent.
func TestAHalfSetCredentialPairIsRefusedNamingTheMissingHalf(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		missing string
	}{
		{"a username with no password", map[string]string{EnvUsername: "u"}, EnvPassword},
		{"a password with no username", map[string]string{EnvPassword: "p"}, EnvUsername},
		{"a client certificate with no key", map[string]string{EnvClientCertPath: "/c.pem"}, EnvClientKeyPath},
		{"a client key with no certificate", map[string]string{EnvClientKeyPath: "/k.pem"}, EnvClientCertPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadSettings(mapLookup(tc.env))
			if err == nil {
				t.Fatal("the half-set pair was accepted")
			}
			if !strings.Contains(err.Error(), tc.missing) {
				t.Fatalf("the error does not name the missing variable %s: %v", tc.missing, err)
			}
		})
	}

	// THE CONTROL: each pair, complete, must load. Without it "refused" would
	// not be distinguishable from "these names are never accepted".
	for _, env := range []map[string]string{
		{EnvUsername: "u", EnvPassword: "p"},
		{EnvClientCertPath: "/c.pem", EnvClientKeyPath: "/k.pem"},
	} {
		if _, err := LoadSettings(mapLookup(env)); err != nil {
			t.Fatalf("the complete pair %v was refused: %v", env, err)
		}
	}
}

// TestTwoSpellingsOfOneBearerTokenAreRefused. Which one is meant is
// undecidable, and picking one would silently ignore the other.
func TestTwoSpellingsOfOneBearerTokenAreRefused(t *testing.T) {
	_, err := LoadSettings(mapLookup(map[string]string{EnvBearerToken: "t", EnvBearerTokenFile: "/t"}))
	if err == nil {
		t.Fatal("both bearer-token spellings were accepted together")
	}
	for _, want := range []string{EnvBearerToken, EnvBearerTokenFile} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not name %s: %v", want, err)
		}
	}
}

// TestABackoffCeilingBelowItsFloorIsRefused covers the cross-field check.
func TestABackoffCeilingBelowItsFloorIsRefused(t *testing.T) {
	_, err := LoadSettings(mapLookup(map[string]string{EnvClientMinBackoff: "5s", EnvClientMaxBackoff: "1s"}))
	if err == nil {
		t.Fatal("a maximum backoff below the minimum was accepted")
	}
}

// TestLoadSettingsRefusesANilLookup is the API bad-input arm.
func TestLoadSettingsRefusesANilLookup(t *testing.T) {
	if _, err := LoadSettings(nil); err == nil {
		t.Fatal("a nil lookup was accepted")
	}
}

// TestASetButEmptyStringIsRefusedRatherThanTreatedAsUnset is the string half of
// the rule envBool already carried for booleans.
//
// THE REACHABLE DAMAGE, name by name, is why this is not a tidiness rule. An
// entry carrying `"LOKI_BEARER_TOKEN": ""` sent an UNAUTHENTICATED request and
// reported nothing; an empty LOKI_AUTH_HEADER silently took the Authorization
// default, so an operator behind a gateway that reserves that header saw their
// choice ignored; an empty LOKI_ORG_ID queried the default tenant. Each is a
// value the operator wrote into their own config entry and each was discarded.
//
// A name ABSENT from the entry keeps its old meaning: unset, no error. The
// control below is what separates the two.
func TestASetButEmptyStringIsRefusedRatherThanTreatedAsUnset(t *testing.T) {
	for _, name := range []string{
		EnvUsername, EnvPassword, EnvBearerToken, EnvBearerTokenFile, EnvAuthHeader, EnvOrgID,
		EnvCACertPath, EnvClientCertPath, EnvClientKeyPath, EnvHTTPProxyURL, EnvQueryTags,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadSettings(mapLookup(map[string]string{name: ""}))
			if err == nil {
				t.Fatalf("%s set to the empty string was treated as unset", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("the error does not name the variable: %v", err)
			}
			if !strings.Contains(err.Error(), "empty string") {
				t.Fatalf("the error does not say what is wrong: %v", err)
			}
		})
	}

	// THE CONTROL, in the same run: with EVERY one of those names ABSENT the
	// settings load. Without it, the eleven refusals above would also pass on a
	// LoadSettings that refused any environment at all.
	if _, err := LoadSettings(mapLookup(nil)); err != nil {
		t.Fatalf("an environment with none of those names set was refused: %v", err)
	}

	// AND THE DEFAULT SURVIVES for the one name that has one: an ABSENT
	// LOKI_AUTH_HEADER still takes Authorization, which is the behavior the
	// empty-string refusal must not have replaced.
	s, err := LoadSettings(mapLookup(map[string]string{EnvOrgID: "tenant-1"}))
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.AuthHeader != "Authorization" {
		t.Fatalf("AuthHeader = %q, want the default for an absent name", s.AuthHeader)
	}
}

// TestTwoEmptyNamesAreReportedByTheEarlierOne covers the guard that keeps the
// FIRST refusal rather than the last.
//
// WHY THE ORDER IS WORTH A CELL. Every string name is read in one pass and any
// of them can be empty, so with two wrong an operator gets one message and it
// decides where they look. Naming the earliest in the declared order makes the
// message stable: fixing the one it names and re-running walks the entry from
// the top, while naming whichever field happened to be assigned last would
// send them to an arbitrary line and change its answer if the struct's fields
// were ever reordered.
func TestTwoEmptyNamesAreReportedByTheEarlierOne(t *testing.T) {
	// LOKI_USERNAME is the first name LoadSettings reads and LOKI_QUERY_TAGS is
	// the last, so the pair is the widest span the declared order has.
	_, err := LoadSettings(mapLookup(map[string]string{EnvUsername: "", EnvQueryTags: ""}))
	if err == nil {
		t.Fatal("two empty names were accepted")
	}
	if !strings.Contains(err.Error(), EnvUsername) {
		t.Fatalf("the error does not name the EARLIER variable %s: %v", EnvUsername, err)
	}
	if strings.Contains(err.Error(), EnvQueryTags) {
		t.Fatalf("the error names the LATER variable %s as well; the first refusal is the one reported: %v",
			EnvQueryTags, err)
	}

	// THE SAME PAIR THE OTHER WAY ROUND still reports the earlier one, which is
	// what says the choice is the declared order rather than the map's
	// iteration or the order the test happened to write them in.
	_, err = LoadSettings(mapLookup(map[string]string{EnvQueryTags: "", EnvUsername: ""}))
	if err == nil {
		t.Fatal("two empty names were accepted")
	}
	if !strings.Contains(err.Error(), EnvUsername) {
		t.Fatalf("the error does not name the EARLIER variable %s: %v", EnvUsername, err)
	}

	// AND AN ADJACENT PAIR, so the cell is not satisfied by an implementation
	// that always reports whichever name sorts first or reads the credential
	// block before everything else.
	_, err = LoadSettings(mapLookup(map[string]string{EnvCACertPath: "", EnvClientCertPath: ""}))
	if err == nil {
		t.Fatal("two empty names were accepted")
	}
	if !strings.Contains(err.Error(), EnvCACertPath) {
		t.Fatalf("the error does not name the earlier of the two TLS paths (%s): %v", EnvCACertPath, err)
	}
	if strings.Contains(err.Error(), EnvClientCertPath) {
		t.Fatalf("the error names the later variable %s as well: %v", EnvClientCertPath, err)
	}
}

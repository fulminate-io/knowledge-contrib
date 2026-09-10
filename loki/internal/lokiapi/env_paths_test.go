// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// env_paths_test.go — the four PATH-VALUED names, and the distinction between a
// file that cannot be READ and one whose CONTENT is wrong.
//
// The two causes send an operator to different places, and two of these names
// have a downstream guard whose message also names the variable and the path —
// so an assertion that checked only for those two passes with the read error
// swallowed, and the operator is told their file "is empty" when it could not be
// opened at all.

// TestEachPathValuedNameErrorsNamingTheVariableAndThePath is the unreadable-path
// arm, over EVERY path-valued name this collector reads. Nothing upstream sees
// such a failure: the collector runs as a spawned child whose working directory
// is a temporary one and whose environment is exactly its config entry, so a
// mistyped path would otherwise become a transport failure the operator cannot
// trace back to what they wrote.
//
// EACH CASE ASSERTS THE ERROR'S OWN TEXT, not merely that the variable and the
// path appear in it. Two of these names have a DOWNSTREAM guard whose message
// also names both — an empty token file, and a file holding no PEM — so a test
// that asserted only "names the variable and the path" passes with the read
// error swallowed, and the operator is then told their file "is empty" when it
// was in fact unreadable. The read arm and the content arm are separate cells
// with distinct expected text for exactly that reason.
func TestEachPathValuedNameErrorsNamingTheVariableAndThePath(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.pem")
	// A DIRECTORY is the unreadable path this test uses: os.ReadFile refuses it
	// on every platform this module builds for, and unlike a chmod-000 file it
	// behaves the same whatever user the suite runs as.
	unreadable := filepath.Join(dir, "a-directory")
	if err := os.Mkdir(unreadable, 0o750); err != nil {
		t.Fatalf("creating the unreadable path: %v", err)
	}

	cases := []struct {
		name     string
		variable string
		env      map[string]string
		path     string
		wantText string
	}{
		{
			"a bearer-token file that does not exist", EnvBearerTokenFile,
			map[string]string{EnvBearerTokenFile: missing}, missing,
			"reading the bearer token named by",
		},
		{
			"a bearer-token path that cannot be read", EnvBearerTokenFile,
			map[string]string{EnvBearerTokenFile: unreadable}, unreadable,
			"reading the bearer token named by",
		},
		{
			"a certificate authority that does not exist", EnvCACertPath,
			map[string]string{EnvCACertPath: missing}, missing,
			"reading the certificate authority named by",
		},
		{
			"a certificate authority path that cannot be read", EnvCACertPath,
			map[string]string{EnvCACertPath: unreadable}, unreadable,
			"reading the certificate authority named by",
		},
		{
			"a client certificate that does not exist", EnvClientCertPath,
			map[string]string{EnvClientCertPath: missing, EnvClientKeyPath: missing}, missing,
			"loading the client certificate named by",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient("http://loki.example:3100", mustSettings(t, tc.env))
			assertNamesBoth(t, err, tc.variable, tc.path)
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("the error says %q, which does not distinguish a READ failure from a CONTENT failure; want it to contain %q",
					err.Error(), tc.wantText)
			}
		})
	}

	// The client-key half names its own variable in the same message.
	_, err := NewClient("http://loki.example:3100",
		mustSettings(t, map[string]string{EnvClientCertPath: missing, EnvClientKeyPath: missing}))
	if err == nil || !strings.Contains(err.Error(), EnvClientKeyPath) {
		t.Fatalf("the client-certificate error does not name %s: %v", EnvClientKeyPath, err)
	}
}

// TestAReadablePathSucceedsThroughTheSamePath is the control for every case
// above: without it, "errors" would not be distinguishable from "this name is
// always an error".
func TestAReadablePathSucceedsThroughTheSamePath(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte("  s3cr3t\n"), 0o600); err != nil {
		t.Fatalf("writing the token: %v", err)
	}
	s := mustSettings(t, map[string]string{EnvBearerTokenFile: tokenPath})
	c, err := NewClient("http://loki.example:3100", s)
	if err != nil {
		t.Fatalf("NewClient over a readable token file: %v", err)
	}
	// The assertion about the credential is a NON-REVERSIBLE property: that it
	// was read and trimmed, never the value.
	if c.bearer == "" {
		t.Fatal("the token file was read as empty")
	}
	if strings.ContainsAny(c.bearer, " \n\t") {
		t.Fatal("the token was not trimmed of surrounding whitespace")
	}
	if len(c.bearer) != len("s3cr3t") {
		t.Fatalf("the trimmed token is %d bytes, want %d", len(c.bearer), len("s3cr3t"))
	}
}

// TestAnEmptyTokenFileIsRefused covers the arm between "unreadable" and "read":
// a file that exists and holds nothing would otherwise become an empty
// Authorization header.
func TestAnEmptyTokenFileIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	_, err := NewClient("http://loki.example:3100", mustSettings(t, map[string]string{EnvBearerTokenFile: path}))
	if err == nil {
		t.Fatal("an empty token file was accepted")
	}
	assertNamesBoth(t, err, EnvBearerTokenFile, path)
	// AND IT SAYS THE FILE IS EMPTY, not that it could not be read. The two
	// causes send an operator to different places, and this is the half of the
	// pair that keeps the read arm above honest.
	if !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("the error says %q, which does not distinguish an EMPTY file from an unreadable one", err.Error())
	}
}

// TestACertificateAuthorityFileWithNoPEMIsRefused covers the parse arm, which
// is distinct from the read arm above.
func TestACertificateAuthorityFileWithNoPEMIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	_, err := NewClient("http://loki.example:3100", mustSettings(t, map[string]string{EnvCACertPath: path}))
	if err == nil {
		t.Fatal("a file holding no PEM certificate was accepted as a certificate authority")
	}
	assertNamesBoth(t, err, EnvCACertPath, path)
	if !strings.Contains(err.Error(), "holds no PEM certificate") {
		t.Fatalf("the error says %q, which does not distinguish a file with no PEM from an unreadable one", err.Error())
	}
}

func assertNamesBoth(t *testing.T, err error, name, path string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error for %s=%q", name, path)
	}
	if !strings.Contains(err.Error(), name) {
		t.Fatalf("the error does not name the variable %s: %v", name, err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("the error does not name the path %s: %v", path, err)
	}
}

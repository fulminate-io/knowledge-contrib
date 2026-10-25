// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubcollector_declare_test.go — the fixture's link-time declare option:
// -ldflags -X main.declare=<secret|none|chrome> selects the environment its
// describe tool declares.

// stubDeclaredEnv is one decoded environment entry of the stub's describe.
type stubDeclaredEnv struct {
	Name           string `json:"name"`
	Class          string `json:"class"`
	EmptySensitive bool   `json:"empty_sensitive"`
}

// stubDescribeEnvironment starts bin over stdio with env and returns the
// environment its describe tool declares.
func stubDescribeEnvironment(t *testing.T, bin string, env ...string) []stubDeclaredEnv {
	t.Helper()
	cmd := exec.Command(bin) //nolint:gosec // the test's own build.
	cmd.Env = env
	client := mcp.NewClient(&mcp.Implementation{Name: "stub-declare-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting over stdio: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: DescribeToolName, Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if res.IsError {
		t.Fatalf("describe: %s", resultTextOf(res))
	}
	var decl struct {
		Environment []stubDeclaredEnv `json:"environment"`
	}
	if err := json.Unmarshal([]byte(mustJSON(t, res.StructuredContent)), &decl); err != nil {
		t.Fatal(err)
	}
	return decl.Environment
}

// TestStubCollector_DeclareSecretIsTheDefaultEnvironment builds the stub with
// -X main.declare=secret and asserts the environment the unflagged build
// declares: STUB_TOKEN and each STUB_SECRETS name, all secret class.
func TestStubCollector_DeclareSecretIsTheDefaultEnvironment(t *testing.T) {
	bin := buildStubCollectorWith(t, "-ldflags", "-X main.declare=secret")
	got := stubDescribeEnvironment(t, bin, "STUB_SECRETS=A_SECRET")
	want := []stubDeclaredEnv{{Name: "STUB_TOKEN", Class: EnvClassSecret}, {Name: "A_SECRET", Class: EnvClassSecret}}
	if len(got) != len(want) {
		t.Fatalf("declare=secret declares %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("declare=secret entry %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestStubCollector_DeclareNoneDeclaresNoEnvironment builds the stub with
// -X main.declare=none and asserts its describe declares no variable, even
// with STUB_SECRETS set.
func TestStubCollector_DeclareNoneDeclaresNoEnvironment(t *testing.T) {
	bin := buildStubCollectorWith(t, "-ldflags", "-X main.declare=none")
	if got := stubDescribeEnvironment(t, bin, "STUB_SECRETS=A_SECRET"); len(got) != 0 {
		t.Errorf("declare=none declares %+v, want no environment entry", got)
	}
}

// TestStubCollector_DeclareChromeDeclaresTheChromeUserDataDir builds the stub
// with -X main.declare=chrome and asserts its describe declares exactly the
// browser collectors' two entries: KNOWLEDGE_CHROME_USER_DATA_DIR (path,
// empty-sensitive) and HOME (path).
func TestStubCollector_DeclareChromeDeclaresTheChromeUserDataDir(t *testing.T) {
	bin := buildStubCollectorWith(t, "-ldflags", "-X main.declare=chrome")
	got := stubDescribeEnvironment(t, bin, "STUB_SECRETS=A_SECRET")
	want := []stubDeclaredEnv{
		{Name: "KNOWLEDGE_CHROME_USER_DATA_DIR", Class: EnvClassPath, EmptySensitive: true},
		{Name: "HOME", Class: EnvClassPath},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("declare=chrome declares %+v, want exactly %+v", got, want)
	}
}

// TestStubCollector_DeclareUnknownValueIsRefusedAtStart builds the stub with
// -X main.declare=bogus and asserts the binary exits non-zero before serving,
// naming the value on stderr.
func TestStubCollector_DeclareUnknownValueIsRefusedAtStart(t *testing.T) {
	bin := buildStubCollectorWith(t, "-ldflags", "-X main.declare=bogus")
	dir := t.TempDir()
	stderrPath := filepath.Join(dir, "stderr")
	stderr, err := os.Create(stderrPath) //nolint:gosec // a t.TempDir() path.
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin) //nolint:gosec // the test's own build.
	cmd.Stdin = bytes.NewReader(nil)
	cmd.Stdout, cmd.Stderr = stderr, stderr
	runErr := cmd.Run()
	_ = stderr.Close()
	out, _ := os.ReadFile(stderrPath) //nolint:gosec // a t.TempDir() path.
	if runErr == nil {
		t.Fatalf("a stub built with declare=bogus exited 0; it must refuse the value at start (stderr %q)", out)
	}
	if !strings.Contains(string(out), `"bogus"`) {
		t.Errorf("the refusal does not name the value \"bogus\": %q", out)
	}
}

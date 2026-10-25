// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubcollector_test.go — the testdata/stubcollector fixture other suites
// drive as a real collector: what it reports, on both transports, and that it
// reports hashes and never a value.

// stubTokenValue carries characters JSON escapes and a non-ASCII rune.
const stubTokenValue = `stub"<&é-token`

// stubSourceDir is the fixture's source directory, relative to this package.
const stubSourceDir = "testdata/stubcollector"

// fenceTestCacheOnStubSources opens the fixture's Go sources from this test
// process, so `go test` keys this package's stored result on them, and fails
// when it opened none.
func fenceTestCacheOnStubSources(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(stubSourceDir)
	if err != nil {
		t.Fatalf("reading %s: %v", stubSourceDir, err)
	}
	opened := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := os.Open(filepath.Join(stubSourceDir, e.Name()))
		if err != nil {
			t.Fatalf("opening %s: %v", e.Name(), err)
		}
		_ = f.Close()
		opened++
	}
	if opened == 0 {
		t.Fatalf("test-cache fence: %s holds no Go source", stubSourceDir)
	}
}

// buildStubCollector builds the fixture into a scratch directory and returns
// the binary's path. The build's output goes to a file whose contents a
// failure reports.
func buildStubCollector(t *testing.T) string {
	t.Helper()
	return buildStubCollectorWith(t)
}

// buildStubCollectorWith builds the fixture with buildArgs passed to go build
// before the package path, and returns the binary's path.
func buildStubCollectorWith(t *testing.T, buildArgs ...string) string {
	t.Helper()
	fenceTestCacheOnStubSources(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "stubcollector")
	logPath := filepath.Join(dir, "build.log")
	logFile, err := os.Create(logPath) //nolint:gosec // a t.TempDir() path.
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"build", "-o", bin}, buildArgs...)
	cmd := exec.Command("go", append(args, "./"+stubSourceDir)...) //nolint:gosec // the test's own arguments.
	cmd.Stdout, cmd.Stderr = logFile, logFile
	runErr := cmd.Run()
	_ = logFile.Close()
	if runErr != nil {
		out, _ := os.ReadFile(logPath) //nolint:gosec // a t.TempDir() path.
		t.Fatalf("building %s: %v\n%s", stubSourceDir, runErr, out)
	}
	return bin
}

// stubSHA256 is the lowercase hex sha256 of v.
func stubSHA256(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// authorizationTransport adds one Authorization header to every request.
type authorizationTransport struct{ value string }

func (a authorizationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", a.value)
	return http.DefaultTransport.RoundTrip(r)
}

// stubCollect calls the fixture's collect tool for id and returns the decoded
// nodes and edges.
func stubCollect(t *testing.T, session *mcp.ClientSession, id string) (nodes, edges []any) {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: DefaultToolName, Arguments: map[string]any{"id": id, "params": map[string]any{"note": "n"}},
	})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if res.IsError {
		t.Fatalf("collect: %s", resultTextOf(res))
	}
	doc := envelopeOf(t, res)
	nodes, _ = doc["nodes"].([]any)
	edges, _ = doc["edges"].([]any)
	return nodes, edges
}

// resultTextOf is resultText tolerating a nil result.
func resultTextOf(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	return resultText(res)
}

// probeMetadata returns the probe node's metadata for id.
func probeMetadata(t *testing.T, nodes []any, id string) map[string]any {
	t.Helper()
	for _, n := range nodes {
		node, _ := n.(map[string]any)
		if node["id"] == "probe:"+id {
			md, _ := node["metadata"].(map[string]any)
			return md
		}
	}
	t.Fatalf("no probe:%s node among %v", id, nodes)
	return nil
}

// TestStubCollector_ReportsWhatReachedItAndNeverAValue drives the built fixture
// over stdio and over HTTP.
func TestStubCollector_ReportsWhatReachedItAndNeverAValue(t *testing.T) {
	bin := buildStubCollector(t)

	t.Run("stdio_reports_env_names_and_token_hash", func(t *testing.T) {
		cmd := exec.Command(bin) //nolint:gosec // the test's own build.
		cmd.Env = []string{"STUB_TOKEN=" + stubTokenValue}
		client := mcp.NewClient(&mcp.Implementation{Name: "stub-suite", Version: "v1"}, nil)
		session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			t.Fatalf("connecting over stdio: %v", err)
		}
		t.Cleanup(func() { _ = session.Close() })

		nodes, edges := stubCollect(t, session, "x")
		if len(nodes) != 2 || len(edges) != 1 {
			t.Fatalf("stubCollect(x) = %d nodes and %d edges, want 2 and 1", len(nodes), len(edges))
		}
		md := probeMetadata(t, nodes, "x")
		if md["env_names"] != "STUB_TOKEN" {
			t.Errorf("env_names = %q, want exactly the environment the stub was given", md["env_names"])
		}
		if md["stub_token_sha256"] != stubSHA256(stubTokenValue) {
			t.Errorf("stub_token_sha256 = %q, want the sha256 of the value it was given", md["stub_token_sha256"])
		}
		if md["note"] != "n" {
			t.Errorf("note = %q, want the params note", md["note"])
		}
		assertNoStubValue(t, mustJSON(t, nodes))
	})

	t.Run("http_reports_authorization_hash", func(t *testing.T) {
		url := startStubHTTP(t, bin)
		httpClient := &http.Client{Transport: authorizationTransport{value: "Bearer " + stubTokenValue}}
		t.Cleanup(httpClient.CloseIdleConnections)
		client := mcp.NewClient(&mcp.Implementation{Name: "stub-suite", Version: "v1"}, nil)
		session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: httpClient}, nil)
		if err != nil {
			t.Fatalf("connecting over HTTP: %v", err)
		}
		t.Cleanup(func() { _ = session.Close() })

		nodes, _ := stubCollect(t, session, "y")
		md := probeMetadata(t, nodes, "y")
		if md["authorization_sha256"] != stubSHA256("Bearer "+stubTokenValue) {
			t.Errorf("authorization_sha256 = %q, want the sha256 of the header it was sent", md["authorization_sha256"])
		}
		if _, ok := md["stub_token_sha256"]; ok {
			t.Errorf("stub_token_sha256 is reported although STUB_TOKEN was not in the environment")
		}
		assertNoStubValue(t, mustJSON(t, nodes))
	})

	t.Run("no-describe_serves_collect_alone", func(t *testing.T) {
		cmd := exec.Command(bin, "--no-describe") //nolint:gosec // the test's own build.
		cmd.Env = []string{}
		client := mcp.NewClient(&mcp.Implementation{Name: "stub-suite", Version: "v1"}, nil)
		session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			t.Fatalf("connecting over stdio: %v", err)
		}
		t.Cleanup(func() { _ = session.Close() })
		listed, err := session.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range listed.Tools {
			names = append(names, tool.Name)
		}
		if !slices.Equal(names, []string{DefaultToolName}) {
			t.Errorf("tools = %v, want only %q", names, DefaultToolName)
		}
	})

	t.Run("version_answers_framework_banner", func(t *testing.T) {
		out, err := exec.Command(bin, "--version").Output() //nolint:gosec // the test's own build.
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(out)); got != "knowledge-collector "+DefaultToolName+" "+version {
			t.Errorf("--version = %q", got)
		}
	})
}

// TestStubCollector_DeclaresStubSecretsAndWritesTheStartReport drives the
// built fixture over stdio with STUB_SECRETS naming two further secrets, one of
// them set, and STUB_REPORT naming a file.
func TestStubCollector_DeclaresStubSecretsAndWritesTheStartReport(t *testing.T) {
	bin := buildStubCollector(t)
	report := filepath.Join(t.TempDir(), "report.json")
	cmd := exec.Command(bin) //nolint:gosec // the test's own build.
	cmd.Env = []string{"STUB_SECRETS=A_SECRET,B_SECRET", "A_SECRET=" + stubTokenValue, "STUB_REPORT=" + report}
	client := mcp.NewClient(&mcp.Implementation{Name: "stub-suite", Version: "v1"}, nil)
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
		Environment []struct{ Name, Class string } `json:"environment"`
	}
	if err := json.Unmarshal([]byte(mustJSON(t, res.StructuredContent)), &decl); err != nil {
		t.Fatal(err)
	}
	classes := map[string]string{}
	for _, e := range decl.Environment {
		classes[e.Name] = e.Class
	}
	for _, name := range []string{"STUB_TOKEN", "A_SECRET", "B_SECRET"} {
		if classes[name] != string(EnvClassSecret) {
			t.Errorf("%s is declared %q, want secret", name, classes[name])
		}
	}

	nodes, _ := stubCollect(t, session, "z")
	md := probeMetadata(t, nodes, "z")
	if md["sha256_A_SECRET"] != stubSHA256(stubTokenValue) {
		t.Errorf("sha256_A_SECRET = %q, want the sha256 of the value it was given", md["sha256_A_SECRET"])
	}
	if _, ok := md["sha256_B_SECRET"]; ok {
		t.Errorf("sha256_B_SECRET is reported although B_SECRET was not in the environment")
	}

	info, err := os.Stat(report)
	if err != nil {
		t.Fatalf("the start report was not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the report's mode is %v, want 0600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(report) //nolint:gosec // a t.TempDir() path.
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		SHA256 map[string]string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"A_SECRET": stubSHA256(stubTokenValue)}; !maps.Equal(got.SHA256, want) {
		t.Errorf("report sha256 = %v, want %v", got.SHA256, want)
	}
	assertNoStubValue(t, string(raw))
}

// startStubHTTP starts the fixture with --http on a free loopback port and
// returns its URL once it has printed it. Its stderr goes to a file whose
// contents a failure reports.
func startStubHTTP(t *testing.T, bin string) string {
	t.Helper()
	stderrPath := filepath.Join(t.TempDir(), "stderr.log")
	stderr, err := os.Create(stderrPath) //nolint:gosec // a t.TempDir() path.
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--http", "127.0.0.1:0") //nolint:gosec // the test's own build.
	cmd.Env = []string{}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = stderr.Close()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		logged, _ := os.ReadFile(stderrPath) //nolint:gosec // a t.TempDir() path.
		t.Fatalf("the stub printed no address: %v\nstderr:\n%s", err, logged)
	}
	url, ok := strings.CutPrefix(strings.TrimSpace(line), "listening ")
	if !ok {
		t.Fatalf("unexpected first line %q", line)
	}
	return url
}

// assertNoStubValue fails when text carries stubTokenValue raw or JSON-escaped.
func assertNoStubValue(t *testing.T, text string) {
	t.Helper()
	escaped := strings.Trim(mustJSON(t, stubTokenValue), `"`)
	if strings.Contains(text, stubTokenValue) || strings.Contains(text, escaped) {
		t.Errorf("the result carries the token value: %s", text)
	}
}

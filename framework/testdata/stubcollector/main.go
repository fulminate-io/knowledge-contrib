// SPDX-License-Identifier: Apache-2.0

// Command stubcollector is a test fixture collector built on the framework. It
// reports, in one node's metadata, what reached it: the names in its own
// environment, and the sha256 hex of its STUB_TOKEN value, of each secret
// STUB_SECRETS names, and of the Authorization header it was sent. It never
// reports a value.
//
// STUB_SECRETS is a comma-separated list of variable names the stub declares as
// secret-class beside STUB_TOKEN. When STUB_REPORT names a file, every start
// writes {"env_names": [...], "sha256": {NAME: hex}} for the declared secrets
// present in its environment to that file at mode 0600.
//
// Usage:
//
//	stubcollector [--no-describe] [--http <addr>]
//	stubcollector --version
//
// With no flag it serves its collect and describe tools over stdio. --http
// serves them over streamable HTTP on addr and prints "listening http://<host:port>/"
// as its first line on stdout once bound. --no-describe serves the collect tool
// alone. Each collect returns two nodes, probe:<id> and peer:<id>, and one
// reaches edge from the first to the second.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// tokenEnv is the secret-class variable the stub declares and reports.
const tokenEnv = "STUB_TOKEN"

// secretsEnv lists, comma-separated, further secret-class names the stub
// declares.
const secretsEnv = "STUB_SECRETS"

// reportEnv names the file each start writes its report to.
const reportEnv = "STUB_REPORT"

// params is the stub's collect params: an optional note copied into the probe
// node's metadata.
type params struct {
	Note string `json:"note,omitempty"`
}

// stub is the fixture collector. lastAuthorization holds the most recent
// Authorization header the HTTP arm received.
type stub struct {
	mu                sync.Mutex
	lastAuthorization string
}

func (s *stub) Tool() framework.ToolSpec {
	return framework.ToolSpec{Name: framework.DefaultToolName, Description: "test fixture: reports what reached it"}
}

func (s *stub) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior:    framework.BehaviorDeclaration{Summarizable: new(false), Embeddable: new(false), Syncable: new(true)},
		NodeTypes:   []string{"probe", "peer"},
		EdgeTypes:   []string{"reaches"},
		Environment: declaredSecrets(),
	}
}

// declaredSecrets declares STUB_TOKEN and each name STUB_SECRETS lists as
// secret-class.
func declaredSecrets() []framework.EnvDeclaration {
	names := secretNames()
	out := make([]framework.EnvDeclaration, 0, len(names))
	for _, name := range names {
		out = append(out, framework.EnvDeclaration{Name: name, Class: framework.EnvClassSecret, Description: "a credential the stub reports by its sha256"})
	}
	return out
}

// secretNames returns STUB_TOKEN followed by the names STUB_SECRETS lists.
func secretNames() []string {
	names := []string{tokenEnv}
	for n := range strings.SplitSeq(os.Getenv(secretsEnv), ",") {
		if n = strings.TrimSpace(n); n != "" && n != tokenEnv {
			names = append(names, n)
		}
	}
	return names
}

// secretHashes yields each declared secret present in the environment with
// the sha256 hex of its value.
func secretHashes() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for _, name := range secretNames() {
			if v, ok := os.LookupEnv(name); ok && !yield(name, sha256Hex(v)) {
				return
			}
		}
	}
}

// writeReport writes the start report to the file STUB_REPORT names, when it
// names one.
func writeReport() error {
	path := os.Getenv(reportEnv)
	if path == "" {
		return nil
	}
	raw, err := json.Marshal(map[string]any{"env_names": environmentNames(), "sha256": maps.Collect(secretHashes())})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600) //nolint:gosec // STUB_REPORT is a path the test running the stub chose.
}

func (s *stub) Walk(_ context.Context, id string, p params, _ framework.ForeignContext) (framework.Result, error) {
	metadata := map[string]string{"env_names": strings.Join(environmentNames(), ",")}
	for name, hash := range secretHashes() {
		if name == tokenEnv {
			metadata["stub_token_sha256"] = hash
			continue
		}
		metadata["sha256_"+name] = hash
	}
	s.mu.Lock()
	if s.lastAuthorization != "" {
		metadata["authorization_sha256"] = sha256Hex(s.lastAuthorization)
	}
	s.mu.Unlock()
	if p.Note != "" {
		metadata["note"] = p.Note
	}
	return framework.Result{
		Nodes: []framework.Node{
			{ID: "probe:" + id, Type: "probe", Metadata: metadata},
			{ID: "peer:" + id, Type: "peer"},
		},
		Edges:    []framework.Edge{{FromID: "probe:" + id, ToID: "peer:" + id, Type: "reaches"}},
		Complete: framework.Complete(),
	}, nil
}

// recordAuthorization returns next with each request's Authorization header
// stored on s first.
func (s *stub) recordAuthorization(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("Authorization"); v != "" {
			s.mu.Lock()
			s.lastAuthorization = v
			s.mu.Unlock()
		}
		next.ServeHTTP(w, r)
	})
}

// environmentNames returns the names in this process's environment, sorted.
func environmentNames() []string {
	var names []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// sha256Hex is the lowercase hex sha256 of v.
func sha256Hex(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// options is the parsed command line.
type options struct {
	noDescribe bool
	httpAddr   string
}

// parseOptions reads [--no-describe] [--http <addr>].
func parseOptions(args []string) (options, error) {
	var o options
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--no-describe":
			o.noDescribe = true
		case "--http":
			if i+1 >= len(args) {
				return options{}, errors.New("--http needs an address")
			}
			i++
			o.httpAddr = args[i]
		default:
			return options{}, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	return o, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "stubcollector:", err)
		os.Exit(1)
	}
}

// run serves the stub as the command line asks.
func run() error {
	s := &stub{}
	if err := writeReport(); err != nil {
		return err
	}
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		return framework.ServeStdio[params](context.Background(), s)
	}
	o, err := parseOptions(os.Args[1:])
	if err != nil {
		return err
	}
	srv, err := framework.NewServer[params](s)
	if err != nil {
		return err
	}
	if o.noDescribe {
		srv.RemoveTools(framework.DescribeToolName)
	}
	if o.httpAddr == "" {
		return srv.Run(context.Background(), &mcp.StdioTransport{})
	}
	return serveHTTP(s, srv, o.httpAddr)
}

// serveHTTP serves srv over streamable HTTP on addr, printing the bound
// address first.
func serveHTTP(s *stub, srv *mcp.Server, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf("listening http://%s/\n", listener.Addr())
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	server := &http.Server{Handler: s.recordAuthorization(handler), ReadHeaderTimeout: 10 * time.Second}
	return server.Serve(listener)
}

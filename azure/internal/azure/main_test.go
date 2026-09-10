// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// main_test.go — the goroutine-leak gate, and the CHILD-PROCESS MODES this
// package's process-level tests re-exec into.
//
// WHY A CHILD PROCESS AT ALL. Two of this collector's properties are properties
// of a PROCESS rather than of a function: what its environment contains, which
// is decided entirely by the entry that spawned it, and whether its served tool
// answers over stdin and stdout. Neither can be observed in the test's own
// process, whose environment is the developer's and whose stdio is the test
// runner's. So the test binary re-execs ITSELF with a controlled environment,
// which is the same shape the client's own provider tests use, and it is a real
// child process for the same reason.
//
// THE GATE HAS AN EMPTY ALLOWLIST. A leaked goroutine does not fail the test
// that leaked it; it fails whatever runs after it, or nothing at all until it
// becomes a resource exhaustion in a long-lived collector. An entry is added
// only when a measured failure names the goroutine and its lifetime is
// justified in writing; none has been needed.

// childModeEnv names the variable that puts a re-exec into a child mode. It is
// read ONLY by the test binary: nothing in the collector reads it, so a
// collector built from this module has no such mode.
const childModeEnv = "AZURE_COLLECTOR_TEST_CHILD_MODE"

// The child modes.
const (
	// childModeServe serves the collector over stdio, so the parent can drive
	// it as a real MCP provider.
	childModeServe = "serve"
	// childModeProbeHTTP makes one HTTP request from inside the child and
	// reports whether it succeeded, so a test can observe what the child's OWN
	// environment does to its transport. The proxy and trust-store variables
	// are read by the Go runtime beneath this collector rather than by any of
	// its code, and they are resolved ONCE PER PROCESS at the first request,
	// so the only place their effect is visible is a child whose whole
	// environment was decided before it started.
	childModeProbeHTTP = "probe-http"
	// childModeReportEnv reports what the child's environment holds for each
	// name it is asked about, one name per line.
	//
	// IT REPORTS PER-NAME LOOKUPS AND NEVER SERIALIZES THE WHOLE ENVIRONMENT.
	// A probe that dumped its environment would put whatever the developer's
	// shell holds into the test output, which is the one thing a test about
	// environments must not do.
	childModeReportEnv = "report-env"
)

// childNamesEnv carries the names the reporting child is asked about, as a JSON
// array, so the parent decides the question and the child answers only it.
const childNamesEnv = "AZURE_COLLECTOR_TEST_CHILD_NAMES"

func TestMain(m *testing.M) {
	switch os.Getenv(childModeEnv) {
	case childModeServe:
		runServeChild()
		return
	case childModeReportEnv:
		runReportEnvChild()
		return
	case childModeProbeHTTP:
		runProbeHTTPChild()
		return
	}
	frameworktest.VerifyNoGoroutineLeaks(m)
}

// runServeChild serves this collector over stdio with a walk that needs no
// Azure subscription: the subject is the PROCESS and its protocol, not the
// Azure API.
func runServeChild() {
	collector := &Collector{
		newCredential: func(context.Context) (azureCredential, error) { return fakeCredential{}, nil },
		buildSubs: func(azureCredential, string) []subCollector {
			return []subCollector{staticSub{name: "child-walk", result: childWalkResult()}}
		},
	}
	if err := framework.ServeStdio(context.Background(), collector); err != nil {
		fmt.Fprintf(os.Stderr, "child: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// runReportEnvChild prints one line per requested name: `NAME=present:<value>`
// when the child's environment holds it and `NAME=absent` when it does not.
//
// PRESENT-AND-EMPTY IS DISTINGUISHABLE FROM ABSENT, which is the whole point:
// an operator who declares a name and leaves it blank has done something
// different from one who did not declare it, and a probe that reported both as
// empty could not tell the two apart.
func runReportEnvChild() {
	var names []string
	if raw := os.Getenv(childNamesEnv); raw != "" {
		if err := json.Unmarshal([]byte(raw), &names); err != nil {
			fmt.Fprintf(os.Stderr, "child: decoding the requested names: %v\n", err)
			os.Exit(1)
		}
	}
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			fmt.Printf("%s=present:%s\n", name, value)
			continue
		}
		fmt.Printf("%s=absent\n", name)
	}
	os.Exit(0)
}

// childURLEnv carries the URL the probing child requests, and childProbeAgain
// asks it to repeat the request after mutating its own environment, which is
// how the once-per-process memoization is observed.
const (
	childURLEnv      = "AZURE_COLLECTOR_TEST_CHILD_URL"
	childProbeAgain  = "AZURE_COLLECTOR_TEST_CHILD_PROBE_AGAIN"
	childProbeMutate = "AZURE_COLLECTOR_TEST_CHILD_PROBE_MUTATE"
)

// runProbeHTTPChild requests the given URL and prints one line per attempt:
// `ok:<status>` or `err:<message>`.
func runProbeHTTPChild() {
	url := os.Getenv(childURLEnv)
	attempt := func() {
		resp, err := http.Get(url) //nolint:gosec,noctx // a URL the test supplies to its own server
		if err != nil {
			fmt.Printf("err:%v\n", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		fmt.Printf("ok:%d\n", resp.StatusCode)
	}
	attempt()
	if os.Getenv(childProbeAgain) != "" {
		// Mutating the variable AFTER the first request must change nothing:
		// the resolution is memoized once per process, which is why the entry
		// that spawned the child is what decides it.
		if mutate := os.Getenv(childProbeMutate); mutate != "" {
			name, value, _ := strings.Cut(mutate, "=")
			_ = os.Setenv(name, value)
		}
		attempt()
	}
	os.Exit(0)
}

// childWalkResult is the walk the serving child performs: one resource and one
// relationship, enough for the parent to tell this provider's answer from an
// empty one.
func childWalkResult() subResult {
	return subResult{
		resources: []resource{{
			id:           vmID,
			name:         "vm1",
			resourceType: rtVM,
			region:       "westeurope",
			metadata:     map[string]string{"vmSize": "Standard_D2s_v3"},
		}},
		edges: []edge{{from: vmID, to: subnetID, relation: edgeUsesSubnet}},
	}
}

// staticSub is a subcollector that returns a fixed result, or a fixed error.
type staticSub struct {
	name   string
	result subResult
	err    error
}

func (s staticSub) Name() string { return s.name }

func (s staticSub) Collect(context.Context) (subResult, error) { return s.result, s.err }

// testBinary resolves this test binary's own path, for the re-execs below.
//
// IT IS os.Executable RATHER THAN os.Args[0], which is the sibling framework
// module's idiom and is the correct one twice over: os.Args[0] is whatever the
// caller typed, which need not be a path at all, and a taint analysis reads it
// as attacker-influenced input to a command.
func testBinary(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving this test binary: %v", err)
	}
	return self
}

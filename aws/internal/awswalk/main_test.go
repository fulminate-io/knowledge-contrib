// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// main_test.go — the goroutine-leak gate, and the switch that turns this test
// binary into a real stdio collector.
//
// THE GOLEAK ALLOWLIST IS EMPTY, taken from frameworktest so this module gets the
// same gate every other package in the workspace has by one line. It matters here
// specifically: the account walk is a bounded-concurrency fan-out over forty
// service walks, and a walk that abandons a goroutine on a cancelled context or
// an erroring service leaks silently — the per-test symptom is nothing at all,
// and the failure lands on whatever runs next or on a long-lived collector
// process weeks later.
//
// AN ENTRY ADDED TO THAT ALLOWLIST LATER MUST NAME THE GOROUTINE and say why its
// lifetime legitimately exceeds the test that started it. This package needs
// none.
//
// THE STDIO PROVIDER IS THIS TEST BINARY, RE-EXECED, for the same reason the
// client's own host tests re-exec theirs: the thing under test on the environment
// arm is a real child PROCESS with its own environment, and a child that is this
// binary needs no compile step and no module scaffolding in a temp directory.
//
// THE CHILD WRITES NOTHING TO STDOUT BUT JSON-RPC: stdout is the protocol stream,
// and a stray print corrupts the framing into an opaque handshake failure. The one
// diagnostic below goes to stderr.

// envProbeModeVar switches this binary from "run tests" to "be a collector that
// answers environment lookups".
const envProbeModeVar = "KN_AWS_ENV_PROBE"

func TestMain(m *testing.M) {
	if os.Getenv(envProbeModeVar) != "" {
		serveEnvProbeOverStdio()
		return
	}
	frameworktest.VerifyNoGoroutineLeaks(m)
}

// serveEnvProbeOverStdio runs the environment-probe collector on the framework's
// real stdio entry point and never returns.
func serveEnvProbeOverStdio() {
	if err := framework.ServeStdio(context.Background(), envProbeCollector{}); err != nil {
		fmt.Fprintf(os.Stderr, "env probe collector: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// envProbeParams names the variables the parent wants looked up.
type envProbeParams struct {
	Names []string `json:"names,omitempty"`
}

// envProbeCollector answers PER-NAME lookups and never serializes its whole
// environment.
//
// THE RESTRAINT IS THE POINT AND NOT A DETAIL. A probe that dumped os.Environ
// into a result the collect then admitted would write whatever the process held
// into a graph that is searched, summarized and synchronized — reproducing,
// inside its own test, exactly the disclosure the declared-environment contract
// exists to prevent.
type envProbeCollector struct{}

func (envProbeCollector) Tool() framework.ToolSpec {
	return framework.ToolSpec{Name: "collect_env_probe", Description: "answers per-name environment lookups"}
}

func (envProbeCollector) Walk(_ context.Context, id string, p envProbeParams, _ framework.ForeignContext) (framework.Result, error) {
	nodes := make([]framework.Node, 0, len(p.Names))
	for _, name := range p.Names {
		value, present := os.LookupEnv(name)
		nodes = append(nodes, framework.Node{
			ID:   id + "/" + name,
			Type: "env-probe",
			// PRESENCE AND VALUE ARE SEPARATE FIELDS, because an absent variable
			// and one set to the empty string are different states and the whole
			// contract turns on the difference.
			Summary:  name,
			Metadata: map[string]string{"present": boolText(present), "value": value},
		})
	}
	return framework.Result{Nodes: nodes, Complete: framework.Complete()}, nil
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Describe is the minimum a probe collector owes the contract: the framework
// refuses to build a server for a declaration that never said, so a fixture
// serving no declaration would fail at its own TestMain rather than in the row
// it exists for.
func (envProbeCollector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable: new(false), Embeddable: new(false), Syncable: new(true),
		},
		NodeTypes:   []string{"env-probe"},
		EdgeTypes:   []string{},
		Environment: []framework.EnvDeclaration{},
	}
}

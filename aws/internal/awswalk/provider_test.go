// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// provider_test.go — THE ENVIRONMENT ARM, run against a REAL CHILD PROCESS.
//
// WHAT IT PROVES AND WHAT IT DOES NOT. It proves that a collector binary spawned
// with a constructed environment reads exactly what it was given: a name in the
// environment arrives with that value, a name absent from it is ABSENT in the
// child rather than empty, and a variable planted in the PARENT's environment
// never arrives. It does NOT prove what the daemon puts in a child's environment
// — that is the config-file contract's own property, proven where the loader
// lives. The two together are the whole claim; neither alone is.
//
// THE FOUR ROW SHAPES BELOW ARE THE CONTRACT'S, in its own terms: the entry's
// `env` block is the child's WHOLE environment, the daemon copies nothing from
// its own and adds nothing.
//
// NOTHING HERE TOUCHES THE OPERATOR'S ENVIRONMENT, a credential, a file or a
// network. The child is this test binary re-execed with an environment built from
// scratch, and its whole job is to answer lookups.

// probeTimeout bounds the child's handshake and one call. A child that never
// speaks is a failure rather than a hang.
const probeTimeout = 30 * time.Second

// spawnEnvProbe starts this test binary as a stdio collector under the supplied
// environment and returns a session against it.
//
// THE ENVIRONMENT IS BUILT FROM SCRATCH, never appended to os.Environ(): the
// whole subject is what the child does with the environment it is GIVEN, and
// inheriting the test process's would make every absence assertion below a claim
// about this machine.
func spawnEnvProbe(t *testing.T, env map[string]string) *mcp.ClientSession {
	t.Helper()
	cmd := exec.Command(os.Args[0]) //nolint:gosec // this test binary, re-execed
	cmd.Env = []string{envProbeModeVar + "=1"}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stderr = os.Stderr

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "env-probe-parent", Version: "v1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("the spawned collector did not complete an MCP handshake: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// lookupInChild asks the spawned collector what it sees for one name.
func lookupInChild(t *testing.T, session *mcp.ClientSession, name string) (value string, present bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	args, err := json.Marshal(map[string]any{"id": "probe", "params": map[string]any{"names": []string{name}}})
	if err != nil {
		t.Fatalf("marshal the probe arguments: %v", err)
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "collect_env_probe",
		Arguments: json.RawMessage(args),
	})
	if err != nil {
		t.Fatalf("call the probe tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("the probe tool returned an error result: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal the probe result: %v", err)
	}
	var out struct {
		Nodes []struct {
			Summary  string            `json:"summary"`
			Metadata map[string]string `json:"metadata"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode the probe result: %v", err)
	}
	for _, n := range out.Nodes {
		if n.Summary == name {
			return n.Metadata["value"], n.Metadata["present"] == "true"
		}
	}
	t.Fatalf("the probe returned no answer for %q", name)
	return "", false
}

// TestProvider_ANameInTheEnvironmentArrivesWithItsValue is row (1), and it is the
// SAME-RUN KNOWN POSITIVE the three absence rows below rest on: without it they
// prove only that the probe read nothing.
func TestProvider_ANameInTheEnvironmentArrivesWithItsValue(t *testing.T) {
	const sentinel = "kn-aws-probe-sentinel-value"
	session := spawnEnvProbe(t, map[string]string{"AWS_PROFILE": sentinel})
	value, present := lookupInChild(t, session, "AWS_PROFILE")
	if !present {
		t.Fatal("a name given to the child must be present in it")
	}
	if value != sentinel {
		t.Errorf("the child read %q for AWS_PROFILE, want the value it was given (%q)", value, sentinel)
	}
}

// TestProvider_ANameAbsentFromTheEnvironmentIsAbsentInTheChild is row (2).
//
// ABSENT, NOT EMPTY. The distinction is the whole contract: the SDK treats an
// unset AWS_PROFILE as "use the default profile" and an empty one as a profile
// whose name is the empty string, which resolves nothing. A test asserting only
// that the value is empty would pass on both.
func TestProvider_ANameAbsentFromTheEnvironmentIsAbsentInTheChild(t *testing.T) {
	session := spawnEnvProbe(t, map[string]string{"AWS_PROFILE": "given"})
	value, present := lookupInChild(t, session, "AWS_SECRET_ACCESS_KEY")
	if present {
		t.Errorf("AWS_SECRET_ACCESS_KEY is present in the child with value %q; it was not given to it", value)
	}
}

// TestProvider_AnEmptyEnvironmentIsAnEmptyChild is row (3): an entry with no env
// block yields a child with nothing, never inheritance.
func TestProvider_AnEmptyEnvironmentIsAnEmptyChild(t *testing.T) {
	session := spawnEnvProbe(t, nil)
	for _, name := range []string{"AWS_PROFILE", "AWS_REGION", "HOME", "PATH"} {
		if value, present := lookupInChild(t, session, name); present {
			t.Errorf("%s is present in a child given no environment at all, with value %q", name, value)
		}
	}
}

// TestProvider_AVariablePlantedInTheParentNeverArrives is row (4), the NEGATIVE
// CONTROL the other three rest on.
//
// It is the one with no counterpart in the landed code and it is the sharpest:
// the parent sets a variable in its OWN environment and the child, spawned with a
// constructed environment that omits it, must not see it. Without this row every
// absence above is consistent with a child that inherits an environment which
// happens not to carry those names on this machine.
func TestProvider_AVariablePlantedInTheParentNeverArrives(t *testing.T) {
	const planted = "KN_AWS_PROBE_PLANTED_IN_PARENT"
	t.Setenv(planted, "the-parent-set-this")
	if os.Getenv(planted) != "the-parent-set-this" {
		t.Fatal("the control did not plant the variable in this process, so the assertion below proves nothing")
	}

	session := spawnEnvProbe(t, map[string]string{"AWS_PROFILE": "given"})

	// The known positive first, in the same child: a name that WAS given arrives.
	if _, present := lookupInChild(t, session, "AWS_PROFILE"); !present {
		t.Fatal("the child did not receive the name it was given, so the absence below proves nothing")
	}
	if value, present := lookupInChild(t, session, planted); present {
		t.Errorf("the child saw %s=%q, which only this test process set: the child's environment is the one it "+
			"was constructed with, and nothing is copied from the parent", planted, value)
	}
}

// TestProvider_HomeIsAbsentRatherThanSynthesized is HOME's own row, in both
// directions.
//
// THE HAZARD IS A PLACEHOLDER, not an absence. Several runtimes synthesize a HOME
// when the variable is missing, and a synthesized one BREAKS the credential path
// where an absent one merely fails it: the SDK resolves a shared config file
// under it, finds none, and reports no profile rather than reporting no home. So
// the assertion is on the child's own raw lookup and not on any library's view.
func TestProvider_HomeIsAbsentRatherThanSynthesized(t *testing.T) {
	session := spawnEnvProbe(t, map[string]string{"AWS_REGION": "us-east-1"})
	if value, present := lookupInChild(t, session, "HOME"); present {
		t.Errorf("HOME is present in a child that was not given it, with value %q; an invented home sends the "+
			"SDK looking for a shared config file that is not there", value)
	}

	withHome := spawnEnvProbe(t, map[string]string{"HOME": "/tmp/kn-aws-probe-home"})
	value, present := lookupInChild(t, withHome, "HOME")
	if !present || value != "/tmp/kn-aws-probe-home" {
		t.Errorf("HOME given in the environment must arrive with its value; got %q present=%t", value, present)
	}
}

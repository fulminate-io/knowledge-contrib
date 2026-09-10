// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// transport_test.go — THE ARM THE ENVIRONMENT EXCLUSIONS MAKE MANDATORY.
//
// This collector's configuration entry deliberately declares neither of the two
// trust-store names nor any of the six proxy spellings. That is a recorded
// decision with a real cost: on a host behind a proxy, or one whose certificate
// bundle is not at a compiled-in default path, an operator who has not listed
// those names cannot reach the endpoint at all. The failure is a TRANSPORT
// error, which is NOT a credential error, so the credential arm does not cover
// it.
//
// WHAT THIS COLLECTOR OWES is that such a failure is LOUD and names the
// condition — never an empty graph asserted as a complete walk, which would
// tell the deletion phase that everything the failed collect did not carry is
// gone.
//
// BOTH HALVES RUN IN A SPAWNED CHILD, which is forced rather than stylistic: Go
// resolves the proxy environment ONCE PER PROCESS and memoizes it, so a test
// setting the variable in its own process after any request had been made would
// observe nothing. A child's environment is fixed before its first request.
//
// THE DESTINATION IS A DOCUMENTATION-RESERVED ADDRESS, NOT A LOOPBACK ONE, and
// that too is forced: Go's proxy resolution bypasses proxies for loopback
// destinations UNCONDITIONALLY, so a proxy variable can have no effect on a
// request to a local test server and the arm would be inert. The address below
// is reserved for documentation and is not routable, so the un-proxied half
// fails at the dial and the proxied half is answered by the test's own proxy.

// unroutableEndpoint is a documentation-reserved address (RFC 5737 TEST-NET-1).
// Nothing routes to it, which is what makes the un-proxied half fail.
const unroutableEndpoint = "http://192.0.2.1:80"

// probeDialTimeout bounds the failing half. It is the PROBE's own client
// setting, not the collector's: without it a dial to an unroutable address
// waits for the operating system's own timeout.
const probeDialTimeout = 2 * time.Second

// transportProbeParams names the endpoint the probe walks.
type transportProbeParams struct {
	Endpoint string `json:"endpoint"`
	LogGroup string `json:"log_group"`
}

// transportProbeCollector walks a caller-supplied endpoint through a REAL SDK
// client on the collector's own fetch path.
//
// It supplies a static credential of its own so the walk reaches the transport
// rather than stopping at the credential chain: the subject here is what
// happens to a request that cannot be delivered, not how one is signed. The
// value is a fixed test string naming no real principal.
//
// ITS HTTP CLIENT KEEPS Go's ENVIRONMENT-DRIVEN PROXY RESOLUTION, which is the
// behavior under test, and adds only a dial timeout and no retries so the
// failing half fails in seconds rather than minutes.
type transportProbeCollector struct{}

func (t *transportProbeCollector) Tool() framework.ToolSpec {
	return framework.ToolSpec{Name: toolName, Description: "transport probe"}
}

func (t *transportProbeCollector) Walk(
	ctx context.Context, _ string, params transportProbeParams, _ framework.ForeignContext,
) (framework.Result, error) {
	client := cloudwatchlogs.New(cloudwatchlogs.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(params.Endpoint),
		Retryer:      aws.NopRetryer{},
		HTTPClient: awshttp.NewBuildableClient().WithTransportOptions(func(tr *http.Transport) {
			tr.Proxy = http.ProxyFromEnvironment
			tr.DialContext = (&net.Dialer{Timeout: probeDialTimeout}).DialContext
		}),
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "probe", SecretAccessKey: "probe", Source: "transport-probe"}, nil
		}),
	})
	fetched, err := fetchAll(ctx, client, Params{LogGroups: []string{params.LogGroup}}, window{})
	if err != nil {
		return framework.Result{}, err
	}
	nodes, edges, err := buildGraph(fetched.Entries, CloudContext{})
	if err != nil {
		return framework.Result{}, err
	}
	complete := framework.Complete()
	if fetched.Truncated {
		complete = framework.Incomplete(fetched.Reason)
	}
	return framework.Result{Nodes: nodes, Edges: edges, Complete: complete}, nil
}

// TestATransportFailureIsLoudAndWritesNothing is the arm, with its same-run
// control.
//
// The two halves differ in EXACTLY ONE THING: whether the child's environment
// carries a proxy name. With it the request reaches the test's own proxy and
// the walk succeeds; without it the request cannot be delivered and the walk
// fails loudly. That is the cost of excluding the proxy names, demonstrated
// rather than asserted.
func TestATransportFailureIsLoudAndWritesNothing(t *testing.T) {
	// A stand-in proxy that answers every forwarded request with an empty page,
	// which is enough: the question is whether the request reached it.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"events":[]}`))
	}))
	defer proxy.Close()

	args := map[string]any{
		"id":     "transport",
		"params": map[string]any{"endpoint": unroutableEndpoint, "log_group": "/g"},
	}

	t.Run("with no proxy name declared, the failure is loud and writes nothing", func(t *testing.T) {
		child := dialStdioProviderWithEnv(t, modeTransport, nil)
		res, err := child.call(t, args)
		if err == nil && (res == nil || !res.IsError) {
			t.Fatalf("a request that could not be delivered produced a successful result: %v", res)
		}
		text := refusalText(res, err)
		if !strings.Contains(text, "/g") {
			t.Errorf("the failure does not name the log group it was reading: %s", text)
		}
		lowered := strings.ToLower(text)
		if !strings.Contains(lowered, "dial") && !strings.Contains(lowered, "timeout") &&
			!strings.Contains(lowered, "connect") {
			t.Errorf("the failure does not name the transport condition: %s", text)
		}
		// THE REQUIREMENT'S REAL CONTENT: not an empty graph asserted complete.
		if strings.Contains(text, "walk_complete") {
			t.Errorf("the failed collect returned a walk envelope: %s", text)
		}
	})

	t.Run("with the proxy name declared, the same request reaches the endpoint", func(t *testing.T) {
		child := dialStdioProviderWithEnv(t, modeTransport, map[string]string{"HTTP_PROXY": proxy.URL})
		res, err := child.call(t, args)
		if err != nil {
			t.Fatalf("the control call failed at the transport: %v", err)
		}
		if res.IsError {
			t.Fatalf("the control call was refused, so the failing half above is not attributable to the "+
				"absent proxy name: %s", refusalText(res, nil))
		}
		envelope, ok := res.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("the control call returned no envelope: %v", res.StructuredContent)
		}
		if envelope["walk_complete"] != true {
			t.Errorf("the control walk asserted %v, want a complete walk of an empty log group",
				envelope["walk_complete"])
		}
	})
}

// Describe is the minimum a probe collector owes the contract: the framework
// refuses to build a server for a declaration that never said.
func (t *transportProbeCollector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable: new(false), Embeddable: new(false), Syncable: new(true),
		},
		NodeTypes:   []string{"transport-probe"},
		EdgeTypes:   []string{},
		Environment: []framework.EnvDeclaration{},
	}
}

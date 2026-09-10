// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// params_test.go — THE SELECTORS, observed to be HONORED rather than merely
// accepted.
//
// A defaulting bug here ships green and stays invisible: every node still carries
// a plausible region, every edge still resolves, and only a live account against
// a second region would show that the collect walked the wrong one. So each arm
// asserts on the REGION THE CLIENTS WERE BUILT FOR and on the region stamped into
// the emitted nodes, never on the absence of an error.

// regionRecordingCollector returns a collector that records which region its
// config was resolved for.
//
// THE RECORDED VALUE IS WHAT THE SERVICE CLIENTS WOULD BE BUILT FOR, which is the
// property that decides whether a collect walked the region it named — the
// stamped region is asserted against it rather than against the params, so the
// two cannot pass by agreeing with each other while both being wrong.
func regionRecordingCollector(clients *Clients, defaultRegion string) (*Collector, *string) {
	seen := new(string)
	c := &Collector{
		loadConfig: func(_ context.Context, region string) (aws.Config, error) {
			if region == "" {
				region = defaultRegion
			}
			*seen = region
			return aws.Config{Region: region}, nil
		},
		newClients: func(aws.Config) *Clients { return clients },
		identity:   func(context.Context, aws.Config) (string, error) { return fixtureAccount, nil },
	}
	return c, seen
}

func TestParams_RegionSelectorIsHonored(t *testing.T) {
	c, seen := regionRecordingCollector(fixtureClients(), fixtureRegion)
	res, err := c.Walk(context.Background(), "acct", Params{Region: "eu-west-1"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if *seen != "eu-west-1" {
		t.Errorf("the clients were built for region %q; a collect naming a region must walk THAT region's "+
			"endpoints, not the default", *seen)
	}
	// AND THE EMITTED NODES SAY SO. The region rides in metadata and in every
	// composed EC2-family ARN, so a walk that built the right clients and stamped
	// the wrong region would still produce a graph describing another region.
	vpcs := nodesOfType(res, ResourceTypeVPC)
	if len(vpcs) == 0 {
		t.Fatal("no vpc node was emitted, so the region assertion below would pass on nothing")
	}
	for _, n := range vpcs {
		// ASSERTED AGAINST THE REGION THE CLIENTS WERE BUILT FOR, not against the
		// literal: a walk that stamped params.Region while building clients for
		// another would produce a graph describing a region it never visited, and
		// comparing the stamp to the params would not see it.
		if n.Metadata[MetaRegion] != *seen {
			t.Errorf("node %s is stamped with region %q while the clients were built for %q; the stamp and the "+
				"walk must be one value", n.ID, n.Metadata[MetaRegion], *seen)
		}
		if !strings.Contains(n.ID, ":"+*seen+":") {
			t.Errorf("node id %s does not carry the walked region %q", n.ID, *seen)
		}
	}
}

// TestParams_TheStampedRegionIsTheOneTheClientsWereBuiltFor is the arm the
// mutation battery found unobserved.
//
// A collector that took its stamp from params.Region and its CLIENTS from the
// resolved config would agree with itself on every ordinary path and disagree
// exactly when a config source resolved a different region than it was asked for
// — producing nodes describing a region the walk never visited. This drives that
// case directly: the config seam answers with a region OTHER than the requested
// one, and the stamp must follow the config, because the config is what the
// service clients are built from.
func TestParams_TheStampedRegionIsTheOneTheClientsWereBuiltFor(t *testing.T) {
	const resolved = "sa-east-1"
	c := &Collector{
		loadConfig: func(context.Context, string) (aws.Config, error) {
			// A config source that did not honor the requested region.
			return aws.Config{Region: resolved}, nil
		},
		newClients: func(aws.Config) *Clients { return fixtureClients() },
		identity:   func(context.Context, aws.Config) (string, error) { return fixtureAccount, nil },
	}
	res, err := c.Walk(context.Background(), "acct", Params{Region: "eu-west-1"}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	vpcs := nodesOfType(res, ResourceTypeVPC)
	if len(vpcs) == 0 {
		t.Fatal("no vpc node was emitted")
	}
	for _, n := range vpcs {
		if n.Metadata[MetaRegion] != resolved {
			t.Errorf("node %s is stamped %q; the service clients are built from the resolved config, so the "+
				"stamp must be %q or the graph describes a region the walk never visited",
				n.ID, n.Metadata[MetaRegion], resolved)
		}
	}
}

// TestParams_NoRegionSelectorWalksTheConfigDefault is the SAME-RUN CONTROL for
// the arm above: without it, a collector that ignored the selector entirely and
// always walked eu-west-1 would pass.
func TestParams_NoRegionSelectorWalksTheConfigDefault(t *testing.T) {
	c, seen := regionRecordingCollector(fixtureClients(), "ap-south-1")
	res, err := c.Walk(context.Background(), "acct", Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if *seen != "ap-south-1" {
		t.Errorf("with no region selector the walk must use the credential chain's own region; got %q", *seen)
	}
	for _, n := range nodesOfType(res, ResourceTypeVPC) {
		if n.Metadata[MetaRegion] != "ap-south-1" {
			t.Errorf("node %s carries region %q, want the config default ap-south-1", n.ID, n.Metadata[MetaRegion])
		}
	}
}

func TestParams_AccountSelectorRefusesAMismatch(t *testing.T) {
	c := newTestCollector(fixtureClients())
	_, err := c.Walk(context.Background(), "acct", Params{Account: "999999999999"}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("a collect naming an account the credential does not resolve to must be REFUSED; this collector " +
			"assumes no role, so walking the other account would produce a graph that looks entirely correct " +
			"and describes the wrong account")
	}
	if !strings.Contains(err.Error(), "999999999999") || !strings.Contains(err.Error(), fixtureAccount) {
		t.Errorf("the refusal must name BOTH the requested and the resolved account; got %v", err)
	}
}

// TestParams_MatchingAccountSelectorIsAccepted is the control: without it the
// arm above would pass on a collector that refused every account selector.
func TestParams_MatchingAccountSelectorIsAccepted(t *testing.T) {
	res := runWalk(t, fixtureClients(), Params{Account: fixtureAccount})
	if len(res.Nodes) == 0 {
		t.Fatal("a collect naming the account the credential DOES resolve to must proceed")
	}
}

// TestParams_MalformedSelectorsAreRefusedBeforeAnyAPICall covers the bad-input
// rule: a selector this collector cannot honor errors, naming the field, and
// costs no round trip.
func TestParams_MalformedSelectorsAreRefusedBeforeAnyAPICall(t *testing.T) {
	cases := []struct {
		name   string
		params Params
		want   string
	}{
		{"a region that is not a region name", Params{Region: "US-East-1"}, "params.region"},
		{"a region with no separators", Params{Region: "useast1"}, "params.region"},
		{"a region that is punctuation", Params{Region: "-----"}, "params.region"},
		{"an account that is not twelve digits", Params{Account: "12345"}, "params.account"},
		{"an account with a letter in it", Params{Account: "12345678901x"}, "params.account"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// THE CONFIG SEAM PANICS, which is how this asserts "before any API
			// call": a collector that validated after resolving credentials would
			// reach it.
			c := &Collector{
				loadConfig: func(context.Context, string) (aws.Config, error) {
					t.Fatal("params were validated AFTER the credential chain was resolved; a caller error " +
						"must cost no round trip")
					return aws.Config{}, nil
				},
			}
			_, err := c.Walk(context.Background(), "acct", tc.params, framework.ForeignContext{})
			if err == nil {
				t.Fatalf("%s must be refused", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal must name the field; want %q in %v", tc.want, err)
			}
		})
	}
}

// TestParams_WellFormedSelectorsAreAdmitted is the known positive for the
// refusals above: without it they are satisfied by a validator that refuses
// everything.
func TestParams_WellFormedSelectorsAreAdmitted(t *testing.T) {
	for _, region := range []string{"us-east-1", "eu-west-3", "ap-southeast-2", "us-gov-west-1", "cn-north-1"} {
		if err := (Params{Region: region}).validate(); err != nil {
			t.Errorf("region %q must be admitted: %v", region, err)
		}
	}
	if err := (Params{Account: "123456789012"}).validate(); err != nil {
		t.Errorf("a twelve-digit account must be admitted: %v", err)
	}
	if err := (Params{}).validate(); err != nil {
		t.Errorf("empty params must be admitted; the collect that carries none is the ordinary case: %v", err)
	}
}

// TestParams_TheRealConfigLoaderReceivesTheRegion closes the one line the seam
// hides.
//
// Every other region assertion in this file replaces the config loader, so none
// of them exercises the production closure that turns params.Region into a
// config.WithRegion option — and the mutation battery found exactly that: with
// the option dropped, every other test stayed green. This drives the REAL loader.
//
// IT NEEDS NO CREDENTIAL AND NO NETWORK. LoadDefaultConfig resolves credentials
// LAZILY, so a config resolves fine with none; the instance metadata service is
// disabled explicitly so the call cannot dial anything, and the environment is
// scrubbed of the region variables so the only region in play is the one passed.
func TestParams_TheRealConfigLoaderReceivesTheRegion(t *testing.T) {
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "no-such-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "no-such-credentials"))

	// NO loadConfig OVERRIDE: this is the production closure.
	c := &Collector{}

	cfg, err := c.resolveConfig(context.Background(), "eu-north-1")
	if err != nil {
		t.Fatalf("resolving a config for a named region must succeed with no credential: %v", err)
	}
	if cfg.Region != "eu-north-1" {
		t.Errorf("the resolved config carries region %q; the collect's region selector must reach the config "+
			"loader, because the SERVICE CLIENTS are built from this value", cfg.Region)
	}

	// THE SAME-RUN CONTROL: with no region named, the loader resolves whatever the
	// scrubbed environment gives it, which is nothing. Without this the assertion
	// above is satisfied by a loader that hardcoded eu-north-1.
	empty, err := c.resolveConfig(context.Background(), "")
	if err != nil {
		t.Fatalf("resolving a config with no region must succeed: %v", err)
	}
	if empty.Region == "eu-north-1" {
		t.Error("a config resolved with NO region carries the previous call's region; the loader is not reading " +
			"the argument")
	}
}

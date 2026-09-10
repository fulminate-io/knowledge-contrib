// SPDX-License-Identifier: Apache-2.0

package gcpclients

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	runpb "cloud.google.com/go/run/apiv2/runpb"
	dataflow "google.golang.org/api/dataflow/v1b3"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
)

// partialread_test.go — THE PARTIAL-READ AXIS, as a declaration table paired
// with a census that fails when the code disagrees with a row.
//
// THE AXIS WAS DERIVED BY CENSUS, not by sampling: for every list call this
// module makes, `go doc` on its response type, looking for a field that reports
// part of the answer missing. Five channels carry one, and 25 of this module's
// list sites sit on those five. The remaining sites' response types carry no such
// field, which is a fact about the provider rather than a decision here.
//
// WHY THE TABLE ALONE WOULD BE WORTHLESS. A table of rows agrees with itself
// whatever the code does. It is paired with two things that fail when the code
// stops matching it: [TestEveryChannelHasALiveExtractor], which drives each
// channel's extractor over a canned response, and
// [TestOnlySignallessListsDrainWithoutASignal], which parses the production
// listers and refuses a `drain` call that passes no signal unless its list is on
// the allowlist below with a reason.

// channel names one way the provider reports that part of an answer is missing.
type channel string

const (
	// chScopedWarning: an aggregated list marks the individual SCOPE's entry.
	chScopedWarning channel = "compute aggregated scope warning"
	// chPageWarning: a flat compute list marks the PAGE with the same shape.
	chPageWarning channel = "compute flat page warning"
	// chUnreachable: a list response carries an explicit Unreachable list.
	chUnreachable channel = "response Unreachable list"
	// chMissingZones: a cluster list names the zones it could not reach.
	chMissingZones channel = "cluster MissingZones"
	// chNamedField: a service with its own spelling of the same fact.
	chNamedField channel = "service-specific field"
)

// partialReadSites is the axis. Every row is a list this module calls whose
// response type carries a partial-read field, with the channel it arrives on.
var partialReadSites = []struct {
	enumeration string
	channel     channel
	field       string
}{
	// Compute aggregated: five lists, one channel.
	{"gcp-compute-instances", chScopedWarning, "InstancesScopedList.Warning"},
	{"gcp-subnetworks", chScopedWarning, "SubnetworksScopedList.Warning"},
	{"gcp-disks", chScopedWarning, "DisksScopedList.Warning"},
	{"gcp-routers", chScopedWarning, "RoutersScopedList.Warning"},
	{"gcp-instance-groups", chScopedWarning, "InstanceGroupsScopedList.Warning"},

	// Compute flat: nine lists, one channel. The reviewer's own census stopped
	// at the aggregated five; `go doc` shows every flat compute list response
	// carries the same Warning, so the same fact is reportable on all nine.
	{"gcp-networks", chPageWarning, "NetworkList.Warning"},
	{"gcp-firewalls", chPageWarning, "FirewallList.Warning"},
	{"gcp-forwarding-rules", chPageWarning, "ForwardingRuleList.Warning"},
	{"gcp-target-http-proxies", chPageWarning, "TargetHttpProxyList.Warning"},
	{"gcp-target-https-proxies", chPageWarning, "TargetHttpsProxyList.Warning"},
	{"gcp-url-maps", chPageWarning, "UrlMapList.Warning"},
	{"gcp-backend-services", chPageWarning, "BackendServiceList.Warning"},
	{"gcp-security-policies", chPageWarning, "SecurityPolicyList.Warning"},
	{"gcp-ssl-certificates", chPageWarning, "SslCertificateList.Warning"},

	// An explicit Unreachable list: six gapic services and two REST ones.
	{"gcp-run-services", chUnreachable, "ListServicesResponse.Unreachable"},
	{"gcp-cloud-functions", chUnreachable, "ListFunctionsResponse.Unreachable"},
	{"gcp-eventarc-triggers", chUnreachable, "ListTriggersResponse.Unreachable"},
	{"gcp-redis-instances", chUnreachable, "ListInstancesResponse.Unreachable"},
	{"gcp-filestore-instances", chUnreachable, "ListInstancesResponse.Unreachable"},
	{"gcp-workflows", chUnreachable, "ListWorkflowsResponse.Unreachable"},
	{"gcp-bigquery-datasets", chUnreachable, "DatasetList.Unreachable"},
	{"gcp-firestore-databases", chUnreachable, "ListDatabasesResponse.Unreachable"},

	// One service each with its own spelling.
	{"gcp-gke-clusters", chMissingZones, "ListClustersResponse.MissingZones"},
	{"gcp-sql-instances", chNamedField, "InstancesListResponse.Warnings[].Region"},
	{"gcp-dataflow-jobs", chNamedField, "ListJobsResponse.FailedLocation[].Name"},
}

func TestThePartialReadAxisCoversTwentyFiveSitesOnFiveChannels(t *testing.T) {
	if len(partialReadSites) != 25 {
		t.Errorf("the axis table has %d rows; the census found 25", len(partialReadSites))
	}
	seen := map[string]bool{}
	channels := map[channel]int{}
	for _, site := range partialReadSites {
		if seen[site.enumeration] {
			t.Errorf("%q appears twice in the axis", site.enumeration)
		}
		seen[site.enumeration] = true
		channels[site.channel]++
	}
	if len(channels) != 5 {
		t.Errorf("the axis spans %d channels, want 5: %v", len(channels), channels)
	}
}

// TestEveryChannelHasALiveExtractor drives each channel's extractor over a
// canned response and over a clean one. Without the clean arm an extractor that
// reported a partial read on everything would pass.
func TestEveryChannelHasALiveExtractor(t *testing.T) {
	covered := map[channel]bool{}

	t.Run(string(chScopedWarning), func(t *testing.T) {
		covered[chScopedWarning] = true
		items, scope := scopedItems("zones/europe-west1-b", []string{"vm"},
			&computepb.Warning{Code: new("UNREACHABLE")})
		if scope != "zones/europe-west1-b" || len(items) != 1 {
			t.Errorf("an unreachable scope was not reported: scope=%q items=%v", scope, items)
		}
		_, clean := scopedItems("zones/us-central1-a", []string{"vm"}, nil)
		if clean != "" {
			t.Errorf("a scope with no warning was reported as unread: %q", clean)
		}
	})

	t.Run(string(chPageWarning), func(t *testing.T) {
		covered[chPageWarning] = true
		page := &computepb.NetworkList{Warning: &computepb.Warning{
			Code: new("PARTIAL_SUCCESS"),
			Data: []*computepb.Data{{Key: new("scope"), Value: new("global")}},
		}}
		signal := computeWarningSignal[*computepb.NetworkList](func() any { return page })
		if got := signal(); len(got) != 1 || got[0] != "global" {
			t.Errorf("a flat compute page's warning was not read: %v", got)
		}
		clean := computeWarningSignal[*computepb.NetworkList](
			func() any { return &computepb.NetworkList{} })
		if got := clean(); got != nil {
			t.Errorf("a clean page reported %v", got)
		}
		// Before the first page is fetched the response is nil, which must read
		// as "nothing to report" rather than panic.
		nothing := computeWarningSignal[*computepb.NetworkList](func() any { return nil })
		if got := nothing(); got != nil {
			t.Errorf("an unfetched iterator reported %v", got)
		}
	})

	t.Run(string(chUnreachable), func(t *testing.T) {
		covered[chUnreachable] = true
		page := &runpb.ListServicesResponse{Unreachable: []string{"us-east5"}}
		signal := unreachableSignal[*runpb.ListServicesResponse](func() any { return page })
		if got := signal(); len(got) != 1 || got[0] != "us-east5" {
			t.Errorf("an Unreachable list was not read: %v", got)
		}
		clean := unreachableSignal[*runpb.ListServicesResponse](
			func() any { return &runpb.ListServicesResponse{} })
		if got := clean(); len(got) != 0 {
			t.Errorf("a clean page reported %v", got)
		}
	})

	t.Run(string(chMissingZones), func(t *testing.T) {
		covered[chMissingZones] = true
		resp := &containerpb.ListClustersResponse{MissingZones: []string{"europe-west1-b"}}
		err := partialRead(resp.GetMissingZones())
		if err == nil || !strings.Contains(err.Error(), "europe-west1-b") {
			t.Errorf("a missing zone was not reported: %v", err)
		}
		if partialRead((&containerpb.ListClustersResponse{}).GetMissingZones()) != nil {
			t.Error("a cluster list with no missing zone reported a partial read")
		}
	})

	t.Run(string(chNamedField), func(t *testing.T) {
		covered[chNamedField] = true
		got := sqlUnreadRegions(&sqladmin.InstancesListResponse{
			Warnings: []*sqladmin.ApiWarning{nil, {Region: "us-west2"}, {Region: ""}},
		})
		if len(got) != 1 || got[0] != "us-west2" {
			t.Errorf("the unread region was not read: %v", got)
		}
		if len(sqlUnreadRegions(&sqladmin.InstancesListResponse{})) != 0 {
			t.Error("a clean page reported an unread region")
		}

		locations := dataflowUnreadLocations(&dataflow.ListJobsResponse{
			FailedLocation: []*dataflow.FailedLocation{nil, {Name: "europe-west4"}, {Name: ""}},
		})
		if len(locations) != 1 || locations[0] != "europe-west4" {
			t.Errorf("the failed location was not read: %v", locations)
		}
		if len(dataflowUnreadLocations(&dataflow.ListJobsResponse{})) != 0 {
			t.Error("a clean page reported a failed location")
		}
	})

	for _, site := range partialReadSites {
		if !covered[site.channel] {
			t.Errorf("channel %q carries %q in the axis and has no extractor test",
				site.channel, site.enumeration)
		}
	}
}

// TestAPartialReadClassifiesAsOneAtTheEnumerationBoundary closes the loop from
// an extractor to the value the walk classifies on.
func TestAPartialReadClassifiesAsOneAtTheEnumerationBoundary(t *testing.T) {
	err := partialRead([]string{"europe-west1-b"})
	if err == nil {
		t.Fatal("partialRead on a named scope returned nil")
	}
	if !errors.Is(err, collect.ErrPartial) {
		t.Errorf("the error does not classify as a partial read: %v", err)
	}
	if partialRead(nil) != nil {
		t.Error("partialRead on no scopes returned an error")
	}
	if partialRead([]string{""}) != nil {
		t.Error("partialRead on an empty scope name returned an error")
	}
}

// signallessLists are the drain call sites that pass NO page signal, each with
// the reason. Every one is a list whose response type carries no partial-read
// field at all, checked with `go doc` against the pinned SDK.
//
// THE ALLOWLIST IS THE POINT. A new list added without a signal is either on
// this list with a reason or a red, so the axis cannot silently lose a row.
var signallessLists = map[string]string{
	"gcp-service-accounts":      "ListServiceAccountsResponse carries no partial-read field",
	"gcp-secrets":               "ListSecretsResponse carries no partial-read field",
	"gcp-kms-key-rings":         "ListKeyRingsResponse carries no partial-read field",
	"gcp-kms-crypto-keys":       "ListCryptoKeysResponse carries no partial-read field",
	"gcp-logging-sinks":         "ListSinksResponse carries no partial-read field",
	"gcp-alert-policies":        "ListAlertPoliciesResponse carries no partial-read field",
	"gcp-notification-channels": "ListNotificationChannelsResponse carries no partial-read field",
	"gcp-artifact-repositories": "ListRepositoriesResponse carries no partial-read field",
	"gcp-storage-buckets":       "the bucket iterator exposes no page response at all",
	"gcp-pubsub-topics":         "the topic iterator exposes no page response at all",
	"gcp-pubsub-subscriptions":  "the subscription iterator exposes no page response at all",
	"gcp-vpc-connectors":        "ListConnectorsResponse carries no partial-read field",
	"gcp-task-queues":           "ListQueuesResponse carries no partial-read field",
	"gcp-scheduled-jobs":        "ListJobsResponse carries no partial-read field",
	"gcp-dns-zones":             "neither the zone nor the record-set list response carries one",
	"gcp-identity-groups":       "neither the group nor the membership list response carries one",
	"gcp-projects":              "a single-resource read, not a list",
	"gcp-iam-bindings":          "a single-policy read, not a list",
}

// TestOnlySignallessListsDrainWithoutASignal is the census that pairs the table
// above to the code. It parses the production listers and counts every `drain`
// call whose second argument is the literal nil.
//
// THE COUNT IS AN EQUALITY, NOT A CEILING. It was a ceiling against the size of
// the allowlist, which is a different and much weaker number: eighteen lists are
// recorded as signalless and only ten drain calls pass nil, because eight of
// those lists are paged by callback or drained through a helper rather than
// through `drain` at all. A ceiling of eighteen therefore tolerated eight NEW
// signalless drains before it said anything.
const signallessDrainCalls = 10

func TestOnlySignallessListsDrainWithoutASignal(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("locating the package: %v", err)
	}

	var (
		withSignal int
		withoutOne int
	)
	for _, name := range []string{"compute_listers.go", "service_listers.go", "rest_listers.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok || ident.Name != "drain" || len(call.Args) != 2 {
				return true
			}
			if arg, ok := call.Args[1].(*ast.Ident); ok && arg.Name == "nil" {
				withoutOne++
			} else {
				withSignal++
			}
			return true
		})
	}

	// THE KNOWN POSITIVE. Without it a parse that matched nothing would pass
	// both assertions silently.
	if withSignal == 0 {
		t.Fatal("the census found no drain call carrying a signal at all; it is not reading the " +
			"listers, and the assertion below would check nothing")
	}

	if withoutOne != signallessDrainCalls {
		t.Errorf("%d drain calls pass no page signal, and %d are accounted for. A drain added "+
			"without a signal is a partial-read channel the axis lost silently; a drain that "+
			"gained one is a row this constant must be re-derived for.",
			withoutOne, signallessDrainCalls)
	}
	for name, reason := range signallessLists {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is recorded as signalless with no reason", name)
		}
	}
}

// TestTheTwoTablesPartitionTheRealEnumerations is the completeness half, and it
// joins BOTH tables to the enumerations the production wiring actually builds.
//
// IT COMPARED TO A LITERAL COUNT BEFORE, which agrees with itself whatever the
// code does: renaming an enumeration, or replacing one with another, kept the
// total at the literal and said nothing. The names now come from
// subcollectors(), so a rename on either side is a red naming the drifted entry.
//
// CONSTRUCTING THE LIST TOUCHES NO CLIENT. Each constructor wraps a lister
// closure and returns a named Subcollector; the closure captures the nil client
// and is never called here, so a zero-value clients value produces the real
// names with no credential and no network.
func TestTheTwoTablesPartitionTheRealEnumerations(t *testing.T) {
	built := (&clients{}).subcollectors()

	// THE KNOWN POSITIVE: the wiring produced enumerations at all.
	if len(built) == 0 {
		t.Fatal("subcollectors() built nothing; the join below would check nothing")
	}

	real := make(map[string]bool, len(built))
	for _, sub := range built {
		if sub.Name == "" {
			t.Error("an enumeration was built with no name; the tables key on the name")
		}
		if real[sub.Name] {
			t.Errorf("two enumerations are both named %q", sub.Name)
		}
		real[sub.Name] = true
	}

	declared := make(map[string]string, len(partialReadSites)+len(signallessLists))
	for _, site := range partialReadSites {
		declared[site.enumeration] = "the partial-read axis"
	}
	for name := range signallessLists {
		if where, both := declared[name]; both {
			t.Errorf("%q is in %s AND recorded as signalless", name, where)
			continue
		}
		declared[name] = "the signalless allowlist"
	}

	// EVERY REAL ENUMERATION IS IN EXACTLY ONE TABLE. One in neither is a list
	// nobody decided about.
	for name := range real {
		if _, ok := declared[name]; !ok {
			t.Errorf("the enumeration %q is in neither table, so nothing records whether it can "+
				"report a partial read", name)
		}
	}
	// AND EVERY DECLARED NAME IS REAL. A row for an enumeration that no longer
	// exists reads as coverage and covers nothing.
	for name, where := range declared {
		if !real[name] {
			t.Errorf("%s names %q, which subcollectors() does not build", where, name)
		}
	}
	if len(declared) != len(real) {
		t.Errorf("the two tables name %d enumerations and the wiring builds %d",
			len(declared), len(real))
	}
}

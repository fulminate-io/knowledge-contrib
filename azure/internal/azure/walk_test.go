// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// walk_test.go — the runner: what it merges, in what order, and what it does
// with a failure.

// TestRunSubCollectors_MergesInSubcollectorOrderNotCompletionOrder. The
// subcollectors run concurrently, so without a deterministic merge the sequence
// a collect produces would depend on which goroutine finished first, and a
// second collect of an unchanged subscription would look changed.
func TestRunSubCollectors_MergesInSubcollectorOrderNotCompletionOrder(t *testing.T) {
	// The FIRST subcollector is deliberately the SLOWEST, so completion order
	// is the reverse of declaration order and a runner merging by completion
	// would be caught rather than merely unproven.
	slow := delayedSub{name: "slow", delay: 40 * time.Millisecond, id: "a"}
	fast := delayedSub{name: "fast", delay: 0, id: "b"}

	out := runSubCollectors(context.Background(), []subCollector{slow, fast}, 4)
	if len(out.resources) != 2 {
		t.Fatalf("expected both results merged, got %d", len(out.resources))
	}
	if out.resources[0].id != "a" || out.resources[1].id != "b" {
		t.Errorf("merged in completion order: got %s then %s",
			out.resources[0].id, out.resources[1].id)
	}
}

// TestRunSubCollectors_BoundsConcurrency. Every call goes to one management
// endpoint under one subscription's throttling budget, so an unbounded fan-out
// turns a latency win into a throttling storm the SDK's retryer then
// serializes anyway.
func TestRunSubCollectors_BoundsConcurrency(t *testing.T) {
	const degree = 3
	var live, peak atomic.Int32
	subs := make([]subCollector, 0, 12)
	for range 12 {
		subs = append(subs, watchingSub{live: &live, peak: &peak})
	}
	runSubCollectors(context.Background(), subs, degree)
	if got := peak.Load(); got > degree {
		t.Errorf("%d subcollectors ran at once, above the bound of %d", got, degree)
	}
	if peak.Load() < 2 {
		t.Errorf("peak concurrency was %d, so the fan-out is serial and the bound proves nothing", peak.Load())
	}
}

// TestRunSubCollectors_KeepsWhatAFailedSubcollectorGathered. A subcollector
// that paged three pages and failed on the fourth found three pages of real
// resources; discarding them would lose data the run already paid for, and the
// incomplete assertion is what keeps that honest.
func TestRunSubCollectors_KeepsWhatAFailedSubcollectorGathered(t *testing.T) {
	partial := staticSub{
		name:   "azure-storage",
		result: subResult{resources: []resource{{id: storageID, resourceType: rtStorageAccount}}},
		err:    errors.New("listing storage accounts: the request was throttled"),
	}
	healthy := staticSub{
		name:   "azure-vms",
		result: subResult{resources: []resource{{id: vmID, resourceType: rtVM}}},
	}

	out := runSubCollectors(context.Background(), []subCollector{partial, healthy}, 4)
	if len(out.resources) != 2 {
		t.Errorf("expected the failed subcollector's partial result to survive, got %d resources", len(out.resources))
	}
	if len(out.failures) != 1 {
		t.Fatalf("expected one recorded failure, got %d", len(out.failures))
	}
	if out.failures[0].name != "azure-storage" {
		t.Errorf("the failure names %q rather than the subcollector", out.failures[0].name)
	}

	reason := out.incompleteReason()
	for _, want := range []string{"azure-storage", "throttled", "1 of"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the incomplete reason does not carry %q: %s", want, reason)
		}
	}
	// The healthy sibling is NOT named: a reason listing every subcollector
	// would tell an operator nothing about which one to look at.
	if strings.Contains(reason, "azure-vms") {
		t.Errorf("the incomplete reason names a subcollector that succeeded: %s", reason)
	}
}

// TestIncompleteReason_IsEmptyWhenNothingFailed is the control that makes the
// row above readable: the reason is what turns a walk incomplete, so it must be
// empty exactly when the walk was whole.
func TestIncompleteReason_IsEmptyWhenNothingFailed(t *testing.T) {
	out := runSubCollectors(context.Background(),
		[]subCollector{staticSub{name: "azure-vms"}}, 2)
	if got := out.incompleteReason(); got != "" {
		t.Errorf("a walk with no failure produced the reason %q", got)
	}
}

// TestRunSubCollectors_StopsStartingWorkOnACancelledContext. A cancellation
// that lands while a goroutine waits on the semaphore must not start a fresh
// round of calls against the management API.
func TestRunSubCollectors_StopsStartingWorkOnACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	subs := []subCollector{
		staticSub{name: "azure-vms", result: subResult{resources: []resource{{id: vmID, resourceType: rtVM}}}},
	}
	out := runSubCollectors(ctx, subs, 1)
	if len(out.failures) != 1 {
		t.Fatalf("a cancelled walk recorded %d failures", len(out.failures))
	}
	if !errors.Is(out.failures[0].err, context.Canceled) {
		t.Errorf("the recorded failure is %v, not the cancellation", out.failures[0].err)
	}
	if len(out.resources) != 0 {
		t.Errorf("a cancelled walk still gathered %d resources", len(out.resources))
	}
}

// TestDedupeResources_FirstEmissionWins. Two subcollectors legitimately reach
// the same proxy — one key vault named by a function app's settings and by a
// web app's — and first-emission-wins keeps the output stable under the
// deterministic merge order.
func TestDedupeResources_FirstEmissionWins(t *testing.T) {
	first := resource{id: "azure:keyvault:vault1", name: "vault1", resourceType: rtVaultProxy,
		metadata: map[string]string{metaDiscoveredVia: "app service key vault reference"}}
	second := resource{id: "azure:keyvault:vault1", name: "vault1", resourceType: rtVaultProxy,
		metadata: map[string]string{metaDiscoveredVia: "function app key vault reference"}}

	got := dedupeResources([]resource{first, second})
	if len(got) != 1 {
		t.Fatalf("one proxy discovered twice produced %d nodes", len(got))
	}
	if got[0].metadata[metaDiscoveredVia] != "app service key vault reference" {
		t.Errorf("the later emission won: %v", got[0].metadata)
	}

	// Two DIFFERENT ids are two resources, which is the control on that one.
	both := dedupeResources([]resource{first, {id: "azure:keyvault:vault2", resourceType: rtVaultProxy}})
	if len(both) != 2 {
		t.Errorf("two distinct proxies collapsed to %d", len(both))
	}
}

// TestDedupeEdges_EvidenceIsPartOfTheIdentity. Two assignment edges between one
// pair, one carrying a principal type and one carrying a role source, are
// DIFFERENT facts and both must survive; the same fact twice is noise.
func TestDedupeEdges_EvidenceIsPartOfTheIdentity(t *testing.T) {
	fromAttachment := edge{from: vmID, to: identityA, relation: edgeAssumesRole,
		metadata: map[string]string{mdRoleSource: roleSourceManagedIdentity}}
	fromAssignment := edge{from: vmID, to: identityA, relation: edgeAssumesRole,
		metadata: map[string]string{mdPrincipalType: "ServicePrincipal"}}

	got := dedupeEdges([]edge{fromAttachment, fromAssignment, fromAttachment})
	if len(got) != 2 {
		t.Fatalf("expected the two distinct facts and one dedupe, got %d: %v", len(got), got)
	}

	// The same relationship reached from both ends — a disk's attachment, drawn
	// by the disk walk and by its machine's storage profile — is one fact.
	attached := edge{from: diskID, to: vmID, relation: edgeBoundTo}
	if got := dedupeEdges([]edge{attached, attached}); len(got) != 1 {
		t.Errorf("one relationship reached twice produced %d edges", len(got))
	}
}

// delayedSub returns after a delay, so a test can make completion order differ
// from declaration order deliberately.
type delayedSub struct {
	name  string
	delay time.Duration
	id    string
}

func (s delayedSub) Name() string { return s.name }

func (s delayedSub) Collect(ctx context.Context) (subResult, error) {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return subResult{}, ctx.Err()
	}
	return subResult{resources: []resource{{id: s.id, resourceType: rtVM}}}, nil
}

// watchingSub records how many of its kind run at once.
type watchingSub struct {
	live *atomic.Int32
	peak *atomic.Int32
}

func (s watchingSub) Name() string { return "watching" }

func (s watchingSub) Collect(context.Context) (subResult, error) {
	now := s.live.Add(1)
	for {
		peak := s.peak.Load()
		if now <= peak || s.peak.CompareAndSwap(peak, now) {
			break
		}
	}
	time.Sleep(5 * time.Millisecond)
	s.live.Add(-1)
	return subResult{}, nil
}

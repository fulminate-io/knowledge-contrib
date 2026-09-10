// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// drain_test.go — THE PAGER LOOP, which every one of this module's 62 list
// calls routes through.
//
// WHAT IS AT STAKE IS COMPLETENESS TRUTHFULNESS, not coverage. A loop that
// reads only the first page collects a fraction of a subscription and reports a
// COMPLETE walk; a loop that swallows a page error does the same over a
// subscription it could not finish reading. That assertion is what arms the
// server's deletion phase, so either defect turns a partial read into deletions
// of everything the run did not reach. Neither is visible from any converter
// test, and the pager interface exists precisely so this one can drive it.

// pagePair is a fake pager over a fixed list of pages, optionally failing at a
// chosen page.
type pagePair struct {
	pages   []string
	failAt  int // 1-based page number that returns an error; 0 never fails
	served  int
	failure error
}

func (p *pagePair) More() bool { return p.served < len(p.pages) }

func (p *pagePair) NextPage(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	p.served++
	if p.failAt == p.served {
		return "", p.failure
	}
	return p.pages[p.served-1], nil
}

// TestDrain_ReadsEveryPage. The no-caps row: nothing in this collector limits a
// list to its first page, and this is where that is observable.
func TestDrain_ReadsEveryPage(t *testing.T) {
	pager := &pagePair{pages: []string{"one", "two", "three"}}

	var seen []string
	err := drain(context.Background(), pager, func(page string) error {
		seen = append(seen, page)
		return nil
	})
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(seen) != 3 {
		t.Errorf("drain visited %d of 3 pages: %v", len(seen), seen)
	}
	if pager.served != 3 {
		t.Errorf("the pager was asked for %d of its 3 pages", pager.served)
	}
}

// TestDrain_APageErrorIsReturnedAndWhatWasReadSurvives. This is the mid-page
// transport error the walk's error-arm row names. The caller keeps page one —
// the run already paid for it — and the error is what makes the walk assert
// itself incomplete rather than complete over an unfinished read.
func TestDrain_APageErrorIsReturnedAndWhatWasReadSurvives(t *testing.T) {
	throttled := errors.New("429 Too Many Requests")
	pager := &pagePair{pages: []string{"one", "two"}, failAt: 2, failure: throttled}

	var seen []string
	err := drain(context.Background(), pager, func(page string) error {
		seen = append(seen, page)
		return nil
	})
	if err == nil {
		t.Fatal("a page that failed mid-list was swallowed, so a partial read would assert a complete walk")
	}
	if !errors.Is(err, throttled) {
		t.Errorf("the returned error is %v, not the transport's own", err)
	}
	if len(seen) != 1 || seen[0] != "one" {
		t.Errorf("the pages read before the failure were discarded: %v", seen)
	}
}

// TestDrain_AVisitErrorStopsTheWalkAndIsReturned. The other direction: a
// conversion that cannot project its response is a defect in this collector,
// and continuing past it would emit a graph missing whatever that page held
// while reporting success.
func TestDrain_AVisitErrorStopsTheWalkAndIsReturned(t *testing.T) {
	refused := errors.New("projecting a resource failed")
	pager := &pagePair{pages: []string{"one", "two", "three"}}

	visits := 0
	err := drain(context.Background(), pager, func(string) error {
		visits++
		return refused
	})
	if err == nil {
		t.Fatal("a conversion failure was swallowed")
	}
	if !errors.Is(err, refused) {
		t.Errorf("the returned error is %v, not the conversion's own", err)
	}
	if visits != 1 {
		t.Errorf("drain kept visiting after a conversion failure: %d visits", visits)
	}
	if pager.served != 1 {
		t.Errorf("drain kept paging after a conversion failure: %d pages served", pager.served)
	}
}

// TestDrain_ACancelledContextStopsAtThePageBoundary. The cancellation path a
// long walk over a large subscription depends on: the context reaches every
// page, so a cancelled collect stops at the next boundary rather than paging to
// the end.
func TestDrain_ACancelledContextStopsAtThePageBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pager := &pagePair{pages: []string{"one", "two"}}
	err := drain(ctx, pager, func(string) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled drain returned %v", err)
	}

	// The same-run known positive: an uncancelled drain over the same pager
	// reads both pages, so the stop above is the cancellation rather than a
	// pager that was empty.
	fresh := &pagePair{pages: []string{"one", "two"}}
	if err := drain(context.Background(), fresh, func(string) error { return nil }); err != nil {
		t.Fatalf("the control drain failed: %v", err)
	}
	if fresh.served != 2 {
		t.Errorf("the control drain read %d of 2 pages", fresh.served)
	}
}

// TestDrain_AnEmptyPagerIsNotAnError. A subscription with none of a resource
// type is the ordinary case and the first real run of any new collector; it
// walks to completion and finds nothing.
func TestDrain_AnEmptyPagerIsNotAnError(t *testing.T) {
	visits := 0
	err := drain(context.Background(), &pagePair{}, func(string) error {
		visits++
		return nil
	})
	if err != nil {
		t.Errorf("an empty list was an error: %v", err)
	}
	if visits != 0 {
		t.Errorf("an empty list visited %d pages", visits)
	}
}

// TestPaging_ASecondPageIsReadThroughARealClient. The rows above drive drain
// directly; this one drives it where it actually runs — under a real ARM client
// and its real pager, following the continuation link the service returned.
//
// IT IS THE END-TO-END HALF of the no-caps row: a collector that stopped at the
// first page would report one disk here and the census would still pass, since
// the census populates one page.
func TestPaging_ASecondPageIsReadThroughARealClient(t *testing.T) {
	const secondPage = "/census/disks/page2"

	first := armList(t, managedDisk())
	// A continuation link, exactly as ARM returns one: an absolute URL the
	// client follows without composing anything itself.
	withLink := strings.TrimSuffix(string(first), "}") +
		`,"nextLink":"https://management.azure.com` + secondPage + `?api-version=2024-03-02"}`

	second := managedDisk()
	second.ID = new(diskID + "-2")
	second.Name = new("disk2")

	fake := newARMFake([]armRoute{
		{"disks page two", secondPage, armList(t, second)},
		{"disks page one", "/providers/Microsoft.Compute/disks", []byte(withLink)},
	})

	sub := &diskSub{subBase: subBase{name: nameDisks, cred: fakeCredential{},
		subscriptionID: censusSubscription, transport: fake}}
	out, err := sub.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	fake.assertEveryRouteFired(t)

	ids := map[string]bool{}
	for _, r := range out.resources {
		ids[r.id] = true
	}
	if !ids[diskID] {
		t.Error("the first page's disk is missing")
	}
	if !ids[diskID+"-2"] {
		t.Error("the second page's disk is missing, so the walk stopped at the first page")
	}
}

// TestPaging_AMidPageFailureReachesTheWalkAsAnIncompleteAssertion is the whole
// chain the two middle rows above exist for, from the transport to the
// completeness assertion the server reads.
func TestPaging_AMidPageFailureReachesTheWalkAsAnIncompleteAssertion(t *testing.T) {
	const secondPage = "/census/disks/page2"

	first := armList(t, managedDisk())
	withLink := strings.TrimSuffix(string(first), "}") +
		`,"nextLink":"https://management.azure.com` + secondPage + `?api-version=2024-03-02"}`

	fake := newARMFake([]armRoute{
		// The second page is refused by the management plane, which is what a
		// throttled or newly-forbidden subscription does mid-list.
		{"disks page two", secondPage, nil},
		{"disks page one", "/providers/Microsoft.Compute/disks", []byte(withLink)},
	})
	// A permission the walk lost between pages, which is NOT retried by the
	// SDK — a throttle would be, and this row is about what the walk does with
	// the failure rather than about the retryer.
	fake.status = map[string]int{"disks page two": 403}

	sub := &diskSub{subBase: subBase{name: nameDisks, cred: fakeCredential{},
		subscriptionID: censusSubscription, transport: fake}}

	c := &Collector{
		newCredential: func(context.Context) (azureCredential, error) { return fakeCredential{}, nil },
		buildSubs:     func(azureCredential, string) []subCollector { return []subCollector{sub} },
		lookupEnv:     func(string) (string, bool) { return "", false },
	}
	result, err := c.Walk(context.Background(), censusSubscription, Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if result.Complete.IsComplete() {
		t.Fatal("a walk whose list failed on its second page asserted itself COMPLETE, " +
			"which would arm the deletion of everything it did not reach")
	}
	reason := result.Complete.Reason()
	if !strings.Contains(reason, nameDisks) {
		t.Errorf("the incomplete reason does not name the subcollector that failed: %s", reason)
	}
	if !strings.Contains(reason, "403") {
		t.Errorf("the incomplete reason does not carry what the management plane said: %s", reason)
	}
	// The first page survives, which is the half that makes the assertion a
	// partial result rather than a discarded one.
	if len(result.Nodes) != 1 {
		t.Errorf("the page read before the failure was discarded: %d nodes", len(result.Nodes))
	}
}

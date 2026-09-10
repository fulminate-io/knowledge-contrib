// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// readoutcome_test.go — ONE TEST PER OUTCOME A CONTAINER READ CAN HAVE, each
// carried through to the verdict the walk reports.
//
// WHY THE WHOLE CLASS IS HERE RATHER THAN AN ARM AT A TIME. The server treats a
// COMPLETE collect's absent rows as gone, so the failure this class exists to
// prevent is a partial walk reported complete — and that failure does not live
// in any one outcome, it lives in the set of outcomes nothing drives. Two
// mutations of the completeness path used to leave the whole suite green:
// deleting the incomplete-append for a container that could not be read, and
// turning a mid-body stream error into a silent break. Neither was reachable
// because the recorded-response harness could not refuse a log read or break a
// response. It can now, and every row below drives a real one.
//
// THE OUTCOMES, and what each must produce:
//
//	success                  entries, and COMPLETE
//	empty container          no entries, and COMPLETE — an empty log is an answer
//	denied (RBAC)            INCOMPLETE naming the container, neighbors intact
//	pod gone mid-read        INCOMPLETE naming the container
//	container restarted      INCOMPLETE naming the container
//	stream error mid-read    INCOMPLETE, and the partial entries are NOT passed off as whole
//	bound reached            INCOMPLETE, in TestWalkCompleteArms
//
// EVERY FAILING ROW CARRIES A SUCCEEDING NEIGHBOR in the same collect. That is
// the known positive: it is what separates "this outcome made the walk
// incomplete" from "the walk failed", and it is also the property that matters
// operationally, since a collect must carry what it COULD read.

// twoPods builds the shared fixture: two pods in one namespace, each with one
// container that has output.
func twoPods(t *testing.T) *fakeAPI {
	t.Helper()
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addPod("dev", "api-2", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "the refused pod's neighbor is fine"))
	api.addLog("dev", "api-2", "api", stamped(fixtureBase, "one", "two"))
	return api
}

// assertNeighborSurvived is the known positive every failing row carries: the
// pod that COULD be read is in the result.
func assertNeighborSurvived(t *testing.T, api *fakeAPI, reason string) {
	t.Helper()
	res, err := collectorOver(api, t).Walk(context.Background(), "id", devParams(), framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a single failed container read FAILED the whole collect: %v. The result must carry what "+
			"was read, with the failure reported as incompleteness", err)
	}
	if res.Complete.IsComplete() {
		t.Fatalf("%s left the walk asserting COMPLETE. The server treats a complete collect's absent rows "+
			"as gone, so a partial walk reported complete arms deletion over the rows it could not read", reason)
	}
	if !strings.Contains(res.Complete.Reason(), "api-1") {
		t.Errorf("the incompleteness reason does not name the container that failed: %q", res.Complete.Reason())
	}
	if len(res.Nodes) == 0 {
		t.Fatal("the walk carried no nodes at all; the neighboring pod read cleanly and must be present")
	}
	found := false
	for _, n := range res.Nodes {
		if n.Metadata["label:"+LabelPod] == "api-2" {
			found = true
		}
	}
	if !found {
		t.Fatal("the neighboring pod's stream is absent; one container's failure dropped a container that " +
			"read cleanly")
	}
}

// TestReadOutcomeSuccess is the control the five failing rows rest on.
func TestReadOutcomeSuccess(t *testing.T) {
	api := twoPods(t)
	res, err := collectorOver(api, t).Walk(context.Background(), "id", devParams(), framework.ForeignContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Complete.IsComplete() {
		t.Fatalf("two clean reads reported an incomplete walk: %s", res.Complete.Reason())
	}
	if len(res.Nodes) == 0 {
		t.Fatal("two clean reads produced no nodes")
	}
}

// TestReadOutcomeEmptyContainer — an empty log is an ANSWER, not a failure. A
// container that has written nothing yet must not make the walk incomplete, or
// a freshly-started workload would disable deletion for the whole graph.
func TestReadOutcomeEmptyContainer(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", "")

	res, err := collectorOver(api, t).Walk(context.Background(), "id", devParams(), framework.ForeignContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Complete.IsComplete() {
		t.Fatalf("a container that has written nothing made the walk incomplete: %s", res.Complete.Reason())
	}
	if len(res.Nodes) != 0 {
		t.Fatalf("an empty container produced %d nodes", len(res.Nodes))
	}
}

// TestReadOutcomeDenied — the RBAC case: one pod's log subresource is refused
// while its neighbor succeeds.
func TestReadOutcomeDenied(t *testing.T) {
	api := twoPods(t)
	api.refuseLog("dev", "api-1", "api", http.StatusForbidden)
	assertNeighborSurvived(t, api, "a container whose log the apiserver refused")
}

// TestReadOutcomePodGoneMidRead — the pod was listed and then deleted, so its
// log subresource is gone by the time the read reaches it. A collect racing a
// rollout hits this on every run.
func TestReadOutcomePodGoneMidRead(t *testing.T) {
	api := twoPods(t)
	api.refuseLog("dev", "api-1", "api", http.StatusNotFound)
	assertNeighborSurvived(t, api, "a pod that disappeared between the list and the read")
}

// TestReadOutcomeContainerRestarted — the container the log belongs to is no
// longer the current one, which the apiserver reports as a bad request rather
// than serving the previous container's log.
func TestReadOutcomeContainerRestarted(t *testing.T) {
	api := twoPods(t)
	api.refuseLog("dev", "api-1", "api", http.StatusBadRequest)
	assertNeighborSurvived(t, api, "a container that restarted out from under the read")
}

// TestReadOutcomeStreamErrorMidRead is the one the other four cannot reach: the
// read STARTS, delivers part of a body, and then the connection breaks.
//
// It is the arm where a silent `break` in place of the error return would pass
// off a partial container as a whole one, and it is also the only path on which
// the deferred Close is load-bearing, because a body read to its end is drained
// and recycled by the transport itself.
func TestReadOutcomeStreamErrorMidRead(t *testing.T) {
	api := twoPods(t)
	api.addLog("dev", "api-1", "api", stamped(fixtureBase,
		"the first line of a body that stops", "the second", "the third", "the fourth"))
	api.abortLog("dev", "api-1", "api")
	assertNeighborSurvived(t, api, "a container whose log stream broke part way through")
}

// TestAPartialStreamIsNotPassedOffAsWhole is the same arm read at the container
// level rather than the walk level, so the failure is observed where it
// happens as well as where it lands.
func TestAPartialStreamIsNotPassedOffAsWhole(t *testing.T) {
	api := twoPods(t)
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one", "two", "three", "four", "five", "six"))
	api.abortLog("dev", "api-1", "api")

	_, err := ReadContainerLog(context.Background(), api.client(t),
		Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil, ReadOptions{})
	if err == nil {
		t.Fatal("a stream that broke part way through returned no error. Its partial entries would then be " +
			"reported as the container's whole log, and the walk would assert COMPLETE over them")
	}
	if !strings.Contains(err.Error(), "api-1") {
		t.Errorf("the stream failure does not name the pod it happened on: %v", err)
	}
}

// TestACancelledCollectStopsRatherThanReportingAPartialWalk — the two
// cancellation checks inside the walk, each reached on its own.
//
// A cancelled collect must FAIL rather than return what it managed to read.
// Reporting a partial walk is a judgement about the source; a cancellation is a
// judgement about the caller, and the two must not be confused — a cancelled
// collect that returned an incomplete result would be indistinguishable from a
// cluster that half-refused it.
func TestACancelledCollectStopsRatherThanReportingAPartialWalk(t *testing.T) {
	t.Run("cancelled before the first namespace", func(t *testing.T) {
		api := twoPods(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := collectorOver(api, t).Walk(ctx, "id", devParams(), framework.ForeignContext{}); err == nil {
			t.Fatal("a collect cancelled before it began returned no error")
		}
	})

	// CANCELLED DURING THE CONTAINER READS, which is the cell the two cases
	// around it cannot reach: canceling before the walk is caught when the first
	// list fails locally, and canceling between namespaces is caught when the
	// container reads of the previous namespace fail. Only a cancellation that
	// lands after a pod list has succeeded surfaces as a failed READ, and that
	// must fail the collect rather than be filed as one more container the
	// cluster would not give up.
	t.Run("cancelled during the container reads", func(t *testing.T) {
		api := newFakeAPI(t)
		api.addPod("dev", "api-1", nil, nil, []string{"api", "sidecar"})
		api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one"))
		api.addLog("dev", "api-1", "sidecar", stamped(fixtureBase, "two"))

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		api.mu.Lock()
		api.afterLog = cancel
		api.mu.Unlock()

		res, err := collectorOver(api, t).Walk(ctx, "id", devParams(), framework.ForeignContext{})
		if err == nil {
			t.Fatalf("a collect cancelled during its container reads returned a result rather than an error "+
				"(complete=%v, reason=%q). A container that could not be read because the CALLER went away "+
				"is not a container the cluster refused, and filing it as incompleteness would report a "+
				"cancelled collect as a partial view of the source",
				res.Complete.IsComplete(), res.Complete.Reason())
		}
	})

	// CANCELLED BETWEEN NAMESPACES. The outcome an error is not enough to
	// observe here, because a cancellation that lands mid-request produces the
	// same error from a different check. What this asserts is that a canceled
	// collect issues no further LIST: the container reads of the first namespace
	// fail on the canceled context and the walk returns before the second
	// namespace is reached.
	t.Run("cancelled between namespaces issues no further request", func(t *testing.T) {
		api := newFakeAPI(t)
		api.addPod("dev", "api-1", nil, nil, []string{"api"})
		api.addPod("data", "db-1", nil, nil, []string{"db"})
		api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one"))
		api.addLog("data", "db-1", "db", stamped(fixtureBase, "two"))

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		api.mu.Lock()
		api.afterList = cancel
		api.mu.Unlock()

		params := devParams()
		params.Namespaces = []string{"dev", "data"}
		_, err := collectorOver(api, t).Walk(ctx, "id", params, framework.ForeignContext{})
		if err == nil {
			t.Fatal("a collect cancelled after its first namespace returned a result rather than an error. " +
				"A cancellation is a fact about the caller, not about the source, so it must not be reported " +
				"as an incomplete walk")
		}
		api.mu.Lock()
		lists := len(api.listQueries)
		api.mu.Unlock()
		if lists != 1 {
			t.Fatalf("%d namespaces were listed after the collect was cancelled, want 1. A walk that keeps "+
				"issuing requests it will discard spends the apiserver's time on a result nobody receives",
				lists)
		}
	})

	// CANCELLED WITH THE LIST IN FLIGHT, which the cell above cannot reach: there
	// the cancellation lands on the container reads, here it lands on the list
	// itself and surfaces as a FAILED LIST. That must fail the collect rather
	// than be filed as a namespace the cluster refused, which would report an
	// abandoned collect as a partial view of the cluster.
	t.Run("cancelled with the list in flight", func(t *testing.T) {
		api := newFakeAPI(t)
		api.addPod("dev", "api-1", nil, nil, []string{"api"})
		api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one"))

		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		api.mu.Lock()
		api.duringList = cancel
		api.mu.Unlock()

		res, err := collectorOver(api, t).Walk(ctx, "id", devParams(), framework.ForeignContext{})
		if err == nil {
			t.Fatalf("a collect whose list was cancelled in flight returned a result rather than an error "+
				"(complete=%v, reason=%q)", res.Complete.IsComplete(), res.Complete.Reason())
		}
		if !strings.Contains(err.Error(), "cancelled") {
			t.Fatalf("the failure does not say the collect was cancelled: %v", err)
		}
	})
}

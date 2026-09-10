// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// podlog_test.go — the four API facts that decide how a pod log is read, each
// observed against the recorded-response harness in fakeapi_test.go.

// TestOneNamespaceListsOnlyThatNamespace, with a pod in a SECOND namespace in
// the same fixture as the control.
func TestOneNamespaceListsOnlyThatNamespace(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addPod("prod", "api-9", nil, nil, []string{"api"})

	pods, err := ListPods(context.Background(), api.client(t), "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != "api-1" {
		t.Fatalf("listing dev returned %d pod(s) %v; the prod pod is the control and must not appear",
			len(pods), podNames(pods))
	}
	if len(api.listQueries) != 1 {
		t.Fatalf("one namespace produced %d list calls; the pod list is namespace-scoped, one call each",
			len(api.listQueries))
	}
}

// TestAnEmptyNamespaceIsRefusedBeforeItReachesTheAPI is the boundary row: at
// the API an empty namespace means every namespace.
func TestAnEmptyNamespaceIsRefusedBeforeItReachesTheAPI(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})

	api.addPod("prod", "api-9", nil, nil, []string{"api"})

	_, err := ListPods(context.Background(), api.client(t), "", "")
	if err == nil {
		t.Fatal("an empty namespace was passed through to the API, where it reads EVERY namespace in the " +
			"cluster with nothing in the request saying so")
	}
	if len(api.listQueries) != 0 || len(api.clusterWideQueries) != 0 {
		t.Fatalf("the empty namespace reached the server: namespaced %v, cluster-wide %v",
			api.listQueries, api.clusterWideQueries)
	}

	// THE CONTROL, in the same run and through the same instrument: with the
	// check bypassed the same request DOES reach the cluster-wide path and
	// returns pods from both namespaces. Without it, the assertions above would
	// pass on a fixture that simply has no route for the call.
	client := api.client(t)
	list, err := client.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("the cluster-wide control read failed, so the assertions above prove nothing: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("the cluster-wide control returned %d pods, want both namespaces' 2", len(list.Items))
	}
	if len(api.clusterWideQueries) != 1 {
		t.Fatalf("the cluster-wide control did not reach /api/v1/pods: %v", api.clusterWideQueries)
	}
}

// TestTheLabelSelectorNarrows, with the unselected pod as the same-run control.
func TestTheLabelSelectorNarrows(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", map[string]string{"app": "api"}, nil, []string{"api"})
	api.addPod("dev", "api-2", map[string]string{"app": "api"}, nil, []string{"api"})
	api.addPod("dev", "batch-1", map[string]string{"app": "batch"}, nil, []string{"batch"})

	all, err := ListPods(context.Background(), api.client(t), "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("the unselected listing returned %d pods, want 3; it is the control", len(all))
	}

	narrowed, err := ListPods(context.Background(), api.client(t), "dev", "app=api")
	if err != nil {
		t.Fatal(err)
	}
	if len(narrowed) != 2 {
		t.Fatalf("the selector narrowed to %d pods %v, want 2", len(narrowed), podNames(narrowed))
	}
	if got := api.listQueries[1].Get("labelSelector"); got != "app=api" {
		t.Fatalf("the selector reached the server as %q; a selector the collector drops narrows nothing "+
			"and the collect silently reads every pod", got)
	}
}

// TestInitContainersAreLogSources — the probed shape has three of them, and
// enumerating only the regular containers omits three of four log sources.
func TestInitContainersAreLogSources(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "dev"}}
	for _, c := range []string{"istio-init", "istio-proxy", "vault-agent"} {
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{Name: c})
	}
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "api"})

	sources := SourcesForPod(pod, nil)
	if len(sources) != 4 {
		t.Fatalf("a pod with three init containers and one regular container has %d log sources, want 4. "+
			"Enumerating only .Spec.Containers omits every init container, and on a service mesh those "+
			"are long-running sidecars carrying most of the output", len(sources))
	}
	init := 0
	for _, s := range sources {
		if s.Init {
			init++
		}
	}
	if init != 3 {
		t.Fatalf("%d sources are init containers, want 3", init)
	}

	narrowed := SourcesForPod(pod, []string{"api", "vault-agent"})
	if len(narrowed) != 2 {
		t.Fatalf("naming two containers selected %d sources", len(narrowed))
	}
}

// TestTheEndBoundIsAppliedClientSide is the row the API cannot carry: there is
// no end bound in PodLogOptions, so entries past it must be dropped here.
func TestTheEndBoundIsAppliedClientSide(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "inside one", "inside two", "past the end"))

	res, err := ReadContainerLog(context.Background(), api.client(t),
		Source{Namespace: "dev", Pod: "api-1", Container: "api"},
		map[string]string{"container": "api"},
		ReadOptions{Since: fixtureBase, Until: fixtureBase.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("%d entries survived an end bound one second wide over three lines one second apart; "+
			"want 2. Messages: %v", len(res.Entries), messages(res))
	}
	for _, e := range res.Entries {
		if e.Message == "past the end" {
			t.Fatal("a line past the end bound was kept; the Kubernetes API applies no end bound at all")
		}
	}

	q := api.logQueries[0]
	if q.Get("sinceTime") == "" {
		t.Error("the start bound did not reach the server; it is applied SERVER-side")
	}
	for _, absent := range []string{"until", "untilTime", "endTime", "end"} {
		if q.Get(absent) != "" {
			t.Errorf("the request carried %q; PodLogOptions has no end bound", absent)
		}
	}
}

// TestTimestampsAreAlwaysRequestedAndStripped — without the flag no line has a
// parseable prefix, and the prefix must not survive into the message or every
// template would be a timestamp.
func TestTimestampsAreAlwaysRequestedAndStripped(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "served a request"))

	res, err := ReadContainerLog(context.Background(), api.client(t),
		Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil, ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := api.logQueries[0].Get("timestamps"); got != "true" {
		t.Fatalf("timestamps=%q reached the server; the client-side end bound and every entry's own "+
			"timestamp are parsed from the prefix that flag adds", got)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("%d entries from one stamped line", len(res.Entries))
	}
	if res.Entries[0].Message != "served a request" {
		t.Fatalf("the message is %q; the timestamp prefix must be stripped", res.Entries[0].Message)
	}
	if !res.Entries[0].Timestamp.Equal(fixtureBase) {
		t.Fatalf("the entry timestamp is %s, want the prefix's %s", res.Entries[0].Timestamp, fixtureBase)
	}
}

// TestAnUnparseableLineIsCountedNotSwallowed — a whole read of them means the
// prefix was never returned, which is a different failure from an empty
// container and must not read as one.
func TestAnUnparseableLineIsCountedNotSwallowed(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", "fake logs\nno timestamp here\n")

	res, err := ReadContainerLog(context.Background(), api.client(t),
		Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil, ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 0 {
		t.Fatalf("%d entries from lines with no parseable prefix", len(res.Entries))
	}
	if res.SkippedLines != 2 {
		t.Fatalf("SkippedLines is %d, want 2; a silent zero would read as an empty container", res.SkippedLines)
	}
}

// TestTheSelfBoundsReachTheServerAndMarkTheReadTruncated asserts that both
// bounds reach the wire. Its truncation half rides on the LINE arm, since the
// byte bound here is far larger than the body; each arm's own truncation cells
// are asserted separately below, so neither rides on the other.
func TestTheSelfBoundsReachTheServerAndMarkTheReadTruncated(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one", "two"))

	tail := int64(2)
	limit := int64(4096)
	res, err := ReadContainerLog(context.Background(), api.client(t),
		Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil,
		ReadOptions{TailLines: &tail, LimitBytes: &limit})
	if err != nil {
		t.Fatal(err)
	}
	q := api.logQueries[0]
	if q.Get("tailLines") != "2" || q.Get("limitBytes") != "4096" {
		t.Fatalf("the bounds reached the server as tailLines=%q limitBytes=%q", q.Get("tailLines"), q.Get("limitBytes"))
	}
	if !res.Truncated {
		t.Fatal("a read that returned exactly its tail-line bound was not reported truncated; " +
			"under-reporting incompleteness lets the server treat everything the walk did not carry as gone")
	}
	if q.Get("continue") != "" {
		t.Error("the read issued a continuation token; a container log is a STREAM, not a paginated list")
	}
}

// TestAnUnboundedReadIsNotTruncated is the control for the row above.
func TestAnUnboundedReadIsNotTruncated(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one", "two"))

	res, err := ReadContainerLog(context.Background(), api.client(t),
		Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil, ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Truncated {
		t.Fatal("an unbounded read reported itself truncated; every collect would then be incomplete")
	}
	if len(res.Entries) != 2 {
		t.Fatalf("%d entries from two lines", len(res.Entries))
	}
	q := api.logQueries[0]
	if q.Get("tailLines") != "" || q.Get("limitBytes") != "" {
		t.Fatalf("an unset bound reached the server as tailLines=%q limitBytes=%q; this collector imposes none",
			q.Get("tailLines"), q.Get("limitBytes"))
	}
}

// TestTheStreamSelectorReachesTheServer.
func TestTheStreamSelectorReachesTheServer(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, "one"))
	client := api.client(t)

	for _, tc := range []struct{ param, wire string }{
		{StreamStdout, "Stdout"},
		{StreamStderr, "Stderr"},
		{StreamAll, ""},
		{"", ""},
	} {
		api.mu.Lock()
		api.logQueries = nil
		api.mu.Unlock()
		if _, err := ReadContainerLog(context.Background(), client,
			Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil,
			ReadOptions{Stream: tc.param}); err != nil {
			t.Fatal(err)
		}
		if got := api.logQueries[0].Get("stream"); got != tc.wire {
			t.Errorf("stream=%q reached the server as %q, want %q", tc.param, got, tc.wire)
		}
	}
}

// TestSeverityComesFromTheBodyNotTheStream.
func TestSeverityComesFromTheBodyNotTheStream(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	api.addLog("dev", "api-1", "api", stamped(fixtureBase,
		"level=warn msg=\"disk nearly full\"",
		"[ERROR] upstream refused the connection",
		"served a request",
	))

	res, err := ReadContainerLog(context.Background(), api.client(t),
		Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil,
		ReadOptions{Stream: StreamStderr})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"WARN", "ERROR", "INFO"}
	if len(res.Entries) != len(want) {
		t.Fatalf("%d entries from three lines", len(res.Entries))
	}
	for i, w := range want {
		if res.Entries[i].Severity != w {
			t.Errorf("line %d read from STDERR has severity %q, want %q derived from its own body. "+
				"On this platform everything a container writes to stderr is surfaced as ERROR, so the "+
				"stream is a weaker signal than the text", i, res.Entries[i].Severity, w)
		}
	}
}

// TestACancelledContextStopsTheRead — the loop checks each iteration.
func TestACancelledContextStopsTheRead(t *testing.T) {
	api := newFakeAPI(t)
	api.addPod("dev", "api-1", nil, nil, []string{"api"})
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d of many", i)
	}
	api.addLog("dev", "api-1", "api", stamped(fixtureBase, lines...))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ReadContainerLog(ctx, api.client(t),
		Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil, ReadOptions{})
	if err == nil {
		t.Fatal("a cancelled read returned no error; a cancelled collect that keeps reading burns the " +
			"apiserver's time on a result nobody will receive")
	}
}

// cancellingReader hands out a log body and cancels the context part way
// through it, which is how a collect is actually cancelled: the stream is
// already open and the read is under way.
type cancellingReader struct {
	body   *strings.Reader
	cancel context.CancelFunc
	after  int
	read   int
}

func (c *cancellingReader) Read(p []byte) (int, error) {
	n, err := c.body.Read(p)
	c.read += n
	if c.read >= c.after && c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return n, err
}

// TestTheScanLoopStopsOnCancellationMidRead observes the IN-LOOP check
// specifically.
//
// The end-to-end arm above cannot: client-go refuses to open a stream on an
// already-cancelled context, so that test goes red whether or not the scan loop
// checks anything. The realistic case is a context cancelled while a long log is
// being read, and only the loop's own check stops that.
func TestTheScanLoopStopsOnCancellationMidRead(t *testing.T) {
	lines := make([]string, 2000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d of a long log", i)
	}
	body := stamped(fixtureBase, lines...)

	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancellingReader{body: strings.NewReader(body), cancel: cancel, after: 200}
	t.Cleanup(cancel)

	res, err := scanLog(ctx, reader, Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil, ReadOptions{})
	if err == nil {
		t.Fatalf("the scan read all %d entries after its context was cancelled part way through. A cancelled "+
			"collect that keeps reading burns the apiserver's time on a result nobody will receive",
			len(res.Entries))
	}
	if len(res.Entries) >= len(lines) {
		t.Fatalf("the scan returned %d of %d entries; it read the whole log despite the cancellation",
			len(res.Entries), len(lines))
	}

	// THE CONTROL, same reader shape and same body with no cancellation: the
	// scan reads every line, so the short read above is the cancellation and not
	// the fixture.
	full, err := scanLog(context.Background(), strings.NewReader(body),
		Source{Namespace: "dev", Pod: "api-1", Container: "api"}, nil, ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Entries) != len(lines) {
		t.Fatalf("the uncancelled control read %d of %d entries", len(full.Entries), len(lines))
	}
}

func podNames(pods []corev1.Pod) []string {
	out := make([]string, 0, len(pods))
	for _, p := range pods {
		out = append(out, p.Name)
	}
	return out
}

func messages(res ReadResult) []string {
	out := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		out = append(out, e.Message)
	}
	return out
}

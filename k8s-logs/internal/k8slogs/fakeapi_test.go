// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// fakeapi_test.go — the RECORDED-RESPONSE HARNESS, and why it is an httptest
// server rather than client-go's own fake clientset.
//
// THE FAKE CLIENTSET CANNOT CARRY THESE ROWS. Its GetLogs records the request
// and then returns a fake REST client whose response body is the hardcoded
// string "fake logs", with no seam to configure it — so it can observe the
// options a read ISSUED and can never observe what a read RETURNS. Half of
// these rows are about the returned lines.
//
// The server below serves the two real endpoints against a real client-go
// client, so BOTH SIDES OF THE SEAM ARE REAL: the query string it records is
// the one client-go actually put on the wire, and the body it returns travels
// the same decoding path a live cluster's would.
//
// It is a separate file from the tests that drive it so each stays readable;
// podlog_test.go, collector_test.go and cloudcontext_test.go all build their
// fixtures here.

// fakeAPI is a recorded-response Kubernetes API.
type fakeAPI struct {
	srv *httptest.Server

	mu sync.Mutex
	// pods maps a namespace to the pods it holds.
	pods map[string][]corev1.Pod
	// logs maps "<namespace>/<pod>/<container>" to that container's log body,
	// already carrying the RFC3339Nano prefixes the API adds under Timestamps.
	logs map[string]string
	// listQueries and logQueries record what the client actually asked for.
	listQueries []url.Values
	logQueries  []url.Values
	logPaths    []string
	// clusterWideQueries records reads of /api/v1/pods, which is where an
	// unvalidated empty namespace actually lands.
	clusterWideQueries []url.Values

	// THE THREE FAILURE ARMS. A recorded-response harness that can only succeed
	// tests only the outcome that never needed testing: every completeness
	// assertion this collector makes is about a read that DID NOT succeed, so a
	// harness with no way to fail cannot reach any of them.
	//
	// forbid holds "<namespace>/<pod>/<container>" keys the log subresource
	// refuses with 403, which is what an RBAC denial on one pod looks like while
	// its neighbors succeed.
	forbid map[string]int
	// abortMidBody holds the same keys for logs that write part of a body and
	// then break the connection, which is what a stream dying mid-read looks
	// like. A short body that ENDS cleanly is a different outcome (an empty or
	// finished container) and is not this.
	abortMidBody map[string]struct{}

	// afterList runs after each pod list is served. It is how a test cancels a
	// collect PART WAY THROUGH — after one namespace has been listed and before
	// the next — which is the only way to reach the checks that sit between the
	// walk's steps rather than at its start.
	afterList func()
	// afterLog runs after each log body is served, which is how a test reaches
	// the checks inside the container-read loop rather than the ones between
	// namespaces.
	afterLog func()
	// duringList runs BEFORE a pod list body is written, so a test can cancel
	// while the list request is in flight rather than between iterations. The
	// two moments are caught by different checks in the walk.
	duringList func()
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	api := &fakeAPI{
		pods:         map[string][]corev1.Pod{},
		logs:         map[string]string{},
		forbid:       map[string]int{},
		abortMidBody: map[string]struct{}{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/namespaces/", api.handle)
	// THE CLUSTER-WIDE PATH, and it is registered on purpose. An empty
	// namespace does not reach /api/v1/namespaces//pods — client-go builds
	// /api/v1/pods, the read across EVERY namespace. Leaving that path
	// unhandled would make an empty namespace fail with a 404 and the refusal
	// test would pass for the wrong reason: it would be observing a fixture gap
	// rather than the collector's own check.
	mux.HandleFunc("/api/v1/pods", api.handleClusterWide)
	api.srv = httptest.NewServer(mux)
	// The abort arm panics with http.ErrAbortHandler by design, which the
	// server logs; silence it so a deliberate abort does not read as a crash in
	// the test output.
	api.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	t.Cleanup(api.srv.Close)
	return api
}

// handleClusterWide serves the read an empty namespace actually performs: every
// pod in every namespace.
func (a *fakeAPI) handleClusterWide(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clusterWideQueries = append(a.clusterWideQueries, r.URL.Query())
	list := &corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}}
	for _, namespace := range sortedNamespaces(a.pods) {
		list.Items = append(list.Items, a.pods[namespace]...)
	}
	writePodList(w, list)
}

func sortedNamespaces(pods map[string][]corev1.Pod) []string {
	out := make([]string, 0, len(pods))
	for ns := range pods {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}

func (a *fakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/"), "/")
	switch {
	case len(parts) == 2 && parts[1] == "pods":
		a.listQueries = append(a.listQueries, r.URL.Query())
		if a.duringList != nil {
			a.duringList()
		}
		a.servePodList(w, parts[0], r.URL.Query().Get("labelSelector"))
		if a.afterList != nil {
			a.afterList()
		}
	case len(parts) == 4 && parts[1] == "pods" && parts[3] == "log":
		a.logQueries = append(a.logQueries, r.URL.Query())
		a.logPaths = append(a.logPaths, r.URL.Path)
		a.serveLog(w, parts[0]+"/"+parts[2]+"/"+r.URL.Query().Get("container"))
		if a.afterLog != nil {
			a.afterLog()
		}
	default:
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
	}
}

// serveLog serves one container's log, or one of the two failures.
func (a *fakeAPI) serveLog(w http.ResponseWriter, key string) {
	if status, refused := a.forbid[key]; refused {
		http.Error(w, "logs are forbidden for "+key, status)
		return
	}
	body := a.logs[key]
	w.Header().Set("Content-Type", "text/plain")
	if _, aborting := a.abortMidBody[key]; !aborting {
		fmt.Fprint(w, body)
		return
	}
	// A body that STOPS rather than ends. The first half is flushed so the
	// client has really begun reading, and the handler then aborts, which
	// leaves the chunked stream unterminated and surfaces to the reader as an
	// unexpected end of input part way through. Writing a short body and
	// returning normally would be a clean end and a different outcome
	// altogether — that is an empty or finished container, not a broken stream.
	half := len(body) / 2
	fmt.Fprint(w, body[:half])
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	panic(http.ErrAbortHandler)
}

// refuseLog makes one container's log read return 403, the shape an RBAC denial
// on the pods/log subresource takes.
func (a *fakeAPI) refuseLog(namespace, pod, container string, status int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.forbid[namespace+"/"+pod+"/"+container] = status
}

// abortLog makes one container's log read break mid-body.
func (a *fakeAPI) abortLog(namespace, pod, container string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.abortMidBody[namespace+"/"+pod+"/"+container] = struct{}{}
}

func (a *fakeAPI) servePodList(w http.ResponseWriter, namespace, selector string) {
	list := &corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}}
	for _, pod := range a.pods[namespace] {
		if selector != "" && !matchesSelector(pod.Labels, selector) {
			continue
		}
		list.Items = append(list.Items, pod)
	}
	writePodList(w, list)
}

// writePodList encodes a pod list onto the response. The encode error is checked
// rather than discarded: a v1.Time field makes it a real error return, and a
// fixture that silently served a truncated body would show up as a decode
// failure in whatever test happened to run next.
func writePodList(w http.ResponseWriter, list *corev1.PodList) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(list); err != nil {
		http.Error(w, "encoding the pod list: "+err.Error(), http.StatusInternalServerError)
	}
}

// matchesSelector understands the one form these tests use, `key=value`, which
// is enough to observe that the selector reached the server at all.
func matchesSelector(labels map[string]string, selector string) bool {
	for term := range strings.SplitSeq(selector, ",") {
		k, v, ok := strings.Cut(term, "=")
		if !ok || labels[k] != v {
			return false
		}
	}
	return true
}

func (a *fakeAPI) client(t *testing.T) kubernetes.Interface {
	t.Helper()
	cs, err := kubernetes.NewForConfig(&rest.Config{Host: a.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func (a *fakeAPI) addPod(namespace, name string, labels map[string]string, initContainers, containers []string) {
	pod := corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
	}
	for _, c := range initContainers {
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{Name: c})
	}
	for _, c := range containers {
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: c})
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pods[namespace] = append(a.pods[namespace], pod)
}

func (a *fakeAPI) addLog(namespace, pod, container, body string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs[namespace+"/"+pod+"/"+container] = body
}

// stamped renders a log body with the RFC3339Nano prefixes the apiserver adds
// under Timestamps.
func stamped(base time.Time, lines ...string) string {
	var b strings.Builder
	for i, l := range lines {
		fmt.Fprintf(&b, "%s %s\n", base.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano), l)
	}
	return b.String()
}

var fixtureBase = time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)

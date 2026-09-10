// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
)

// armfake_test.go — A FAKE MANAGEMENT PLANE, so the listing layer is executed
// rather than assumed.
//
// WHAT IT IS FOR. Half of this collector is code a converter test cannot reach:
// the client construction, the pager, the page loop, the stop-at-first-error,
// and the wiring that hands each response to the conversion function that owns
// it. A test that calls the converters directly proves the converters and
// nothing above them — which is how a relationship emitted only inside a lister
// can be deleted with a green suite.
//
// So this replaces the TRANSPORT under the real SDK clients. Every request the
// real pager makes is answered from a body this test wrote, and everything
// between the client constructor and the emitted edge is the shipped code
// running for real.
//
// THE ROUTES ARE ASSERTED, NOT ASSUMED. A route that never matched a request is
// a wrong guess about a URL, and a guess that silently matched nothing would
// turn this whole instrument into an elaborate way of collecting empty results.
// So [armFake] records both halves — which routes fired and which request paths
// nothing claimed — and the census fails on either.

// armFake answers ARM requests from canned bodies, in route order.
type armFake struct {
	mu sync.Mutex
	// routes are tried in order, so a more specific path is listed before the
	// prefix that would also match it.
	routes []armRoute
	// hits counts the requests each route answered, by route name.
	hits map[string]int
	// unmatched is every request path no route claimed, deduplicated. It is
	// NOT an error by itself: most of the 36 subcollectors legitimately ask for
	// things this census does not populate, and they get an empty list.
	unmatched map[string]int
	// status overrides the response status for a named route, so a test can
	// make the management plane REFUSE one call — a throttle mid-list, a
	// permission that changed under the walk — and watch what the collector
	// does with it.
	status map[string]int
}

// armRoute is one canned answer: a name for the failure message, the path
// substring that selects it, and the body.
type armRoute struct {
	name  string
	match string
	body  []byte
}

func newARMFake(routes []armRoute) *armFake {
	return &armFake{routes: routes, hits: map[string]int{}, unmatched: map[string]int{}}
}

// Do answers one request. An unmatched path gets an EMPTY LIST rather than a
// failure: a subcollector asking for something this census does not populate
// should walk to completion and find nothing, which is a real subscription's
// ordinary case and the one an empty-page bug would hide in.
func (f *armFake) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := req.URL.Path
	for _, route := range f.routes {
		if !strings.Contains(path, route.match) {
			continue
		}
		f.hits[route.name]++
		if code, refused := f.status[route.name]; refused {
			return refusalResponse(req, code), nil
		}
		return jsonResponse(req, route.body), nil
	}
	f.unmatched[path]++
	return jsonResponse(req, []byte(`{"value":[]}`)), nil
}

// refusalResponse is what the management plane returns when it will not serve a
// call: a status the SDK surfaces as an error, with the body it carries.
//
// THE RETRY POLICY IS NOT DISABLED FOR IT, so the caller chooses a status that
// says what it means to test. A 403 is surfaced immediately, which is a
// permission the walk lost between pages; a 429 would be retried with backoff
// first, which is the retryer's behaviour rather than this collector's and
// costs seconds of wall clock to observe.
func refusalResponse(req *http.Request, code int) *http.Response {
	body := `{"error":{"code":"AuthorizationFailed","message":"the client does not have authorization to perform this action"}}`
	return &http.Response{
		StatusCode: code,
		Status:     http.StatusText(code),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func jsonResponse(req *http.Request, body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

// assertEveryRouteFired is the instrument's own control. A route that answered
// nothing means its path guess is wrong, and every resource behind it is
// missing from the census — which would otherwise read as a collector that
// emits less than it does.
func (f *armFake) assertEveryRouteFired(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()

	var silent []string
	for _, route := range f.routes {
		if f.hits[route.name] == 0 {
			silent = append(silent, route.name+" (path substring "+route.match+")")
		}
	}
	if len(silent) == 0 {
		return
	}
	sort.Strings(silent)
	t.Errorf("%d canned route(s) answered no request, so their path is wrong and everything behind them is "+
		"absent from this census:\n  %s\nrequest paths nothing claimed:\n  %s",
		len(silent), strings.Join(silent, "\n  "), strings.Join(f.unclaimedPaths(), "\n  "))
}

func (f *armFake) unclaimedPaths() []string {
	out := make([]string, 0, len(f.unmatched))
	for path := range f.unmatched {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// armList renders an ARM list response body from the values a real list call
// would return. The values are the SAME hand-built fixtures the converter tests
// use, so one fixture serves both.
func armList(t *testing.T, values ...any) []byte {
	t.Helper()
	items := make([]json.RawMessage, 0, len(values))
	for _, v := range values {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("encoding a canned ARM list item: %v", err)
		}
		items = append(items, raw)
	}
	body, err := json.Marshal(map[string]any{"value": items})
	if err != nil {
		t.Fatalf("encoding a canned ARM list body: %v", err)
	}
	return body
}

// armObject renders a single-object ARM response, for the handful of reads that
// are a GET rather than a list.
func armObject(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding a canned ARM object: %v", err)
	}
	return body
}

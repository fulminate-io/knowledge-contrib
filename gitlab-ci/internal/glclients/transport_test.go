// SPDX-License-Identifier: Apache-2.0

package glclients_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glclients"
)

// transport_test.go — THE ONE TEST THAT DRIVES A REAL ROUND TRIP, against a
// scratch server this test starts.
//
// WHY IT EXISTS WHEN THE WALK IS TESTED OFFLINE. Every enumeration runs against
// recorded responses through injected interfaces, which is what makes the whole
// collector testable with no credential — and it means NOTHING in this module
// otherwise exercises the wiring between those interfaces and the provider SDK.
// Three things live only there and are invisible to every other test: that the
// instance selector actually decides which host is dialed; that the token
// actually reaches the request; and that the context threaded into each adapter
// actually cancels a call. A compile-time interface assertion proves the shapes
// match and proves none of that.
//
// IT NEVER HITS A REAL ENDPOINT. The scratch server is an httptest.Server this
// test starts and closes, which is also what makes it the SELF-HOSTED arm of the
// instance selector: pointing the collector at a scratch URL is the same code
// path an operator running their own GitLab takes, with no credential involved.
//
// THE TOKEN IS ASSERTED AS PRESENT AND NON-EMPTY, never by value. A test that
// compared the header against a literal would put a credential-shaped string in
// its own failure output for nothing.

// recorder is the scratch instance: it records what it was asked for and answers
// with an empty list.
type recorder struct {
	mu          sync.Mutex
	paths       []string
	authHeaders []string
	// block, when set, holds the handler until the caller's context is done,
	// which is what makes a cancellation observable from outside.
	block bool
	// release is closed by the test's own cleanup so a blocked handler always has
	// a way out.
	//
	// WITHOUT IT THE INSTRUMENT DEADLOCKS ON ITS OWN RED. A handler waiting only
	// on the request's context never returns when the collector failed to pass
	// one, and httptest's Close waits for outstanding requests — so the arm that
	// PROVES the context is threaded would hang the whole package instead of
	// failing it. Measured: the mutation that drops the context from an adapter
	// took the run past three minutes before this channel existed.
	release chan struct{}
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.paths = append(r.paths, req.URL.Path)
	r.authHeaders = append(r.authHeaders, req.Header.Get(gl.AccessTokenHeaderName))
	blocking := r.block
	r.mu.Unlock()

	if blocking {
		select {
		case <-req.Context().Done():
		case <-r.release:
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("[]"))
}

func (r *recorder) seen() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...), append([]string(nil), r.authHeaders...)
}

// dialScratchInstance builds the provider through the REAL path — the same
// environment reads, the same client constructor, the same adapters — pointed at
// a scratch server.
func dialScratchInstance(t *testing.T, handler *recorder) context.Context {
	t.Helper()
	if handler.release == nil {
		handler.release = make(chan struct{})
	}
	server := httptest.NewServer(handler)
	// LIFO: the release closes BEFORE the server does, so a handler still holding
	// a request lets go and Close returns.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(handler.release) })

	t.Setenv(enventry.PrimaryTokenVariable, "a-token-for-this-test-only")
	t.Setenv(enventry.BaseURLVariable, server.URL+"/")
	return t.Context()
}

// TestTheSelectedInstanceIsTheOneDialedAndTheTokenReachesTheRequest.
//
// This is the assertion the offline suite cannot make: that the selector chose a
// host and the client went there, carrying the credential, on a call made through
// one of the seven adapters rather than through the SDK directly.
func TestTheSelectedInstanceIsTheOneDialedAndTheTokenReachesTheRequest(t *testing.T) {
	handler := &recorder{}
	ctx := dialScratchInstance(t, handler)

	api, release, err := glclients.API(ctx)
	if err != nil {
		t.Fatalf("building the provider against a scratch instance: %v", err)
	}
	if release != nil {
		defer release()
	}

	// ONE CALL THROUGH ONE ADAPTER. Which one hardly matters — they are the same
	// wiring — but the group listing is the read every other enumeration starts
	// from, so it is the one whose failure would be widest.
	if _, _, err := api.Groups.ListGroupProjects(ctx, "acme",
		&gl.ListGroupProjectsOptions{}); err != nil {
		t.Fatalf("listing a group against the scratch instance: %v", err)
	}

	paths, headers := handler.seen()
	if len(paths) == 0 {
		t.Fatal("the scratch instance was never dialed. The selector chose it and the client went " +
			"somewhere else, which is the one thing no offline test can see")
	}
	if !strings.Contains(paths[0], "/api/v4/groups/acme/projects") {
		t.Errorf("the request went to %q, want the group's projects path", paths[0])
	}
	// PRESENT AND NON-EMPTY, never the value.
	if headers[0] == "" {
		t.Errorf("the request carried no %s header, so the token the collector read reached the "+
			"client and not the wire", gl.AccessTokenHeaderName)
	}
}

// TestACancelledContextStopsTheCall is what makes the positional context in every
// adapter more than a convention.
//
// THE SDK TAKES ITS CONTEXT AS ONE OF A VARIADIC LIST OF REQUEST OPTIONS, so an
// adapter that simply forgot to pass it compiles, runs, and cannot be cancelled —
// and a collect that an operator interrupted would run every remaining round trip
// to completion. The scratch server holds the handler open until the request's own
// context is done, so a call that carried no context would block here rather than
// return.
func TestACancelledContextStopsTheCall(t *testing.T) {
	handler := &recorder{block: true}
	ctx := dialScratchInstance(t, handler)

	api, release, err := glclients.API(ctx)
	if err != nil {
		t.Fatalf("building the provider: %v", err)
	}
	if release != nil {
		defer release()
	}

	cancellable, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		_, _, callErr := api.Groups.ListGroupProjects(cancellable, "acme",
			&gl.ListGroupProjectsOptions{})
		done <- callErr
	}()

	// Wait until the server has the request in hand, then cancel.
	for range 200 {
		if paths, _ := handler.seen(); len(paths) > 0 {
			break
		}
		<-time.After(10 * time.Millisecond)
	}
	cancel()

	select {
	case callErr := <-done:
		if callErr == nil {
			t.Fatal("a cancelled call returned cleanly")
		}
		if !errors.Is(callErr, context.Canceled) {
			t.Errorf("the call failed with %v, want a cancellation; an adapter that dropped the "+
				"context would fail with something else or not at all", callErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the call did not return after its context was cancelled. The adapter did not " +
			"thread the context into the request, so nothing an operator does stops a walk")
	}
}

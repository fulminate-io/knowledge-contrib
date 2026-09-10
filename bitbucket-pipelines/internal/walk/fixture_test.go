// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/walk"
)

// fixture_test.go — a recorded workspace for the walk-level rows, and the reason
// every collector built in this package carries a Client builder.
//
// NO TEST IN THIS PACKAGE MAY REACH THE NETWORK. The shipped client is pointed
// at the real provider, so a walk that got past its validation with a default
// collector would issue a request to Bitbucket from a test suite. Every
// collector below is built with a builder pointed at a local server, which makes
// that structurally impossible rather than a thing each test remembers.

// recorded is a small workspace: one repository with one pipeline, one runner
// with one label, one environment with a lock, and one variable at each of two
// scopes. It carries every declared type the walk-level rows need and no more —
// the whole-vocabulary corpus lives with the enumerations.
var recorded = map[string]string{
	"repositories/acme": `{"values":[{"uuid":"{r}","slug":"api","full_name":"acme/api",
	  "is_private":true,"scm":"git","language":"go","mainbranch":{"name":"main"}}],"next":""}`,

	"repositories/acme/api/src/main/bitbucket-pipelines.yml": "pipelines:\n" +
		"  default:\n" +
		"    - step:\n" +
		"        name: build\n" +
		"        deployment: production\n" +
		"        runs-on:\n" +
		"          - self-hosted\n" +
		"        script:\n" +
		"          - 'echo $API_KEY'\n",

	"repositories/acme/api/pipelines": `{"values":[{"uuid":"{run}","build_number":1,
	  "created_on":"2026-09-01T10:00:00Z","completed_on":"2026-09-01T10:01:00Z",
	  "duration_in_seconds":60,"state":{"name":"COMPLETED","stage":{"name":""},
	  "result":{"name":"SUCCESSFUL"}},"target":{"ref_name":"main","ref_type":"branch"},
	  "trigger":{"type":"push"}}],"next":""}`,

	"workspaces/acme/pipelines-config/runners": `{"values":[{"uuid":"{runner}",
	  "name":"runner","state":{"status":"ONLINE"},"labels":[{"name":"self-hosted"}]}],"next":""}`,
	"repositories/acme/api/pipelines-config/runners": `{"values":[],"next":""}`,

	// THE RESTRICTIONS OBJECT IS THE LIVE ONE: `admin_only` is a JSON boolean and
	// the object carries a `type`. Recorded as an empty ARRAY here, it decoded
	// against a module that declared a slice and told this package nothing about
	// a provider that sends neither.
	"repositories/acme/api/environments": `{"values":[{"uuid":"{env}","name":"production",
	  "slug":"production","rank":1,"environment_type":{"name":"Production"},
	  "lock":{"type":"lock"},
	  "restrictions":{"type":"deployment_restrictions_configuration","admin_only":true}}],
	  "next":""}`,

	// THE VARIABLES LISTING IS `pipelines_config` AT REPOSITORY SCOPE AND
	// `pipelines-config` AT WORKSPACE SCOPE, and the runners listing above is the
	// hyphen at BOTH. That asymmetry is Bitbucket's own, measured against the live
	// provider with one credential in one run, and this fixture is keyed on the
	// measurement rather than on the module's format strings — the enumeration
	// package's own rows pin both spellings.
	"workspaces/acme/pipelines-config/variables": `{"values":[],"next":""}`,
	"repositories/acme/api/pipelines_config/variables": `{"values":[{"uuid":"{v}",
	  "key":"API_KEY","secured":true,"system":false}],"next":""}`,
	"repositories/acme/api/deployments_config/environments/{env}/variables": `{"values":[],
	  "next":""}`,
}

// fixture is a recorded provider with per-path status injection.
type fixture struct {
	status map[string]int
	mu     sync.Mutex
	counts map[string]int
	server *httptest.Server
	// onRequest, when set, runs inside the handler before the response is
	// written, with the request's path. It is how a test cancels a walk WHILE a
	// request is in flight rather than racing a goroutine against it.
	onRequest func(path string)
}

// newFixture starts the recorded provider.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{status: map[string]int{}, counts: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		f.mu.Lock()
		f.counts[path]++
		hook := f.onRequest
		f.mu.Unlock()
		if hook != nil {
			hook(path)
		}

		if status, injected := f.status[path]; injected {
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":{"message":"injected %d"}}`, status)
			return
		}
		body, ok := recorded[path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"message":"Resource not found"}}`)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// collector is a walk pointed at this fixture.
func (f *fixture) collector() walk.Collector {
	return walk.Collector{Client: func(username, appPassword string) *bbclient.Client {
		return bbclient.NewAt(f.server.URL, f.server.Client(), username, appPassword)
	}}
}

// requests is how many times a path was fetched.
func (f *fixture) requests(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[path]
}

// offline is a collector whose client is pointed at a server that refuses
// everything.
//
// IT IS WHAT THE VALIDATION ROWS USE. Those rows assert that a bad input is
// refused BEFORE anything is read, and the way they show it is by the message;
// pointing the client at a local refusal means a row whose input was wrongly
// ACCEPTED fails here rather than by issuing a request to the real provider.
func offline(t *testing.T) walk.Collector {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":{"message":"this test must not read anything"}}`)
	}))
	t.Cleanup(server.Close)
	return walk.Collector{Client: func(username, appPassword string) *bbclient.Client {
		return bbclient.NewAt(server.URL, server.Client(), username, appPassword)
	}}
}

// withCredentials sets the two halves of the credential for one test.
func withCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("BITBUCKET_USERNAME", "fixture-user")
	t.Setenv("BITBUCKET_APP_PASSWORD", "fixture-app-password")
}

// withEmptyResponse makes one recorded endpoint answer an empty page for the
// duration of a test, and restores it afterwards. The map is package-level, so a
// test that mutated it without restoring would change every later test's
// fixture.
//
// THE BODY IS NOT A PARAMETER because every override this package needs is the
// same one: an endpoint that answers, and answers with nothing. A parameter that
// only ever takes one value reads as generality the caller can rely on and
// cannot.
func withEmptyResponse(t *testing.T, path string) {
	t.Helper()
	const body = `{"values":[],"next":""}`
	previous, existed := recorded[path]
	recorded[path] = body
	t.Cleanup(func() {
		if existed {
			recorded[path] = previous
			return
		}
		delete(recorded, path)
	})
}

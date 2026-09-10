// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
)

// fixture_test.go — THE RECORDED WORKSPACE, served by a real HTTP server.
//
// WHY A SERVER AND NOT A STUBBED CLIENT. This collector has no provider SDK: its
// seam is a base URL and an *http.Client, so a double on the far side of it
// would be a double of net/http rather than of the provider. A real server keeps
// both sides of the seam real, and it is what makes the pagination, the retry
// budget, the Retry-After parse, the per-status classification and the client's
// own timeout testable offline with no credential.
//
// ONE WORKSPACE, ASSEMBLED SO A SINGLE WALK REACHES THE WHOLE DECLARED
// VOCABULARY. A per-test fixture set can pass every test while leaving a
// resource type nothing emits, so the corpus is one workspace with one recorded
// response per enumeration, and the coverage assertion runs over that one walk.
//
// EVERY FIXTURE IS SHAPED TO REACH ITS CONVERTER'S EDGES, not merely to produce
// a node: a runner at workspace level AND one at repository level, an
// environment with a lock and one without, variables at all three scopes with
// one name declared at all three, and a pipeline step carrying `deployment`,
// `runs-on` and a script referencing four variables of which one resolves
// nowhere.
//
// BOTH REPOSITORIES DECLARE A MAIN BRANCH, so the one file-404 this corpus
// produces — the second repository's absent pipeline definition — is a file
// missing on a branch the PROVIDER named. It used to be a file missing on a
// branch this module GUESSED, which is a different case entirely: that URL may
// be absent because the BRANCH is, and a corpus that walks clean over it pins
// the guessed-branch silent empty as a true one. The fallback itself keeps a
// row of its own, on a fixture of its own.

const (
	fixtureWorkspace = "acme"
	fixtureAPIRepo   = "api"
	fixtureWebRepo   = "web"
)

// THE PATH SEGMENTS ARE WRITTEN OUT HERE, FROM A MEASUREMENT, RATHER THAN TAKEN
// FROM THE CODE UNDER TEST. A fixture keyed on the module's own format strings
// agrees with the module however wrong both are: that is exactly how the
// repository-scope variables path stayed wrong through this module's whole
// suite, with the parity floor counting Variable as a covered resource type
// because the fixture served it at the spelling the walk asked for.
//
// THE TWO SEGMENTS ARE DIFFERENT, AND THAT IS BITBUCKET'S OWN INCONSISTENCY
// rather than a typo class to normalize. Measured against the live provider
// with one credential in one run, on a repository that has both:
//
//	GET repositories/<ws>/<repo>/pipelines_config/variables -> 200
//	GET repositories/<ws>/<repo>/pipelines-config/variables -> 404
//	GET repositories/<ws>/<repo>/pipelines-config/runners   -> 200
//	GET repositories/<ws>/<repo>/pipelines_config/runners   -> 404
const (
	// variablesSegment is the path segment the repository-scope VARIABLES
	// listing lives under: an underscore.
	variablesSegment = "pipelines_config"
	// runnersSegment is the path segment the repository-scope RUNNERS listing
	// lives under: a hyphen.
	runnersSegment = "pipelines-config"
	// wrongVariablesSegment and wrongRunnersSegment are each other's segments,
	// named so the rows that assert the walk never requests them read as the
	// pins they are.
	wrongVariablesSegment = runnersSegment
	wrongRunnersSegment   = variablesSegment
)

// repoVariablesPath and repoRunnersPath are the two repository-scope listing
// paths, in the measured spellings above.
func repoVariablesPath(repo string) string {
	return fmt.Sprintf("repositories/%s/%s/%s/variables", fixtureWorkspace, repo, variablesSegment)
}

func repoRunnersPath(repo string) string {
	return fmt.Sprintf("repositories/%s/%s/%s/runners", fixtureWorkspace, repo, runnersSegment)
}

// fixtureServer is a recorded Bitbucket API. Responses are keyed by the request
// path; a path with no entry answers 404, which is what the provider answers for
// a feature a repository has not configured.
type fixtureServer struct {
	// pages maps a path to its ordered pages. A path with more than one page is
	// served with a `next` URL pointing at the following one.
	pages map[string][]string
	// raw maps a path to a non-paginated body, used for the pipeline definition.
	raw map[string]string
	// status maps a path to a status the server answers INSTEAD of its body, for
	// the per-outcome rows.
	status map[string]int
	// failAfter maps a path to the number of pages that succeed before the next
	// one fails with 500, for the partial-pagination row.
	failAfter map[string]int
	// slow maps a path to a delay the handler sleeps before answering, for the
	// client-timeout row.
	slow map[string]time.Duration

	mu     sync.Mutex
	counts map[string]int
	server *httptest.Server
}

// newFixture builds the recorded workspace and starts serving it.
func newFixture(t *testing.T) *fixtureServer {
	t.Helper()
	fixture := &fixtureServer{
		pages:     recordedPages(),
		raw:       recordedRaw(),
		status:    map[string]int{},
		failAfter: map[string]int{},
		slow:      map[string]time.Duration{},
		counts:    map[string]int{},
	}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serve))
	t.Cleanup(fixture.server.Close)
	return fixture
}

// client is a client pointed at this fixture, with the credentials the handler
// asserts on.
func (f *fixtureServer) client() *bbclient.Client {
	return bbclient.NewAt(f.server.URL, f.server.Client(), "fixture-user", "fixture-app-password")
}

// requests is how many times a path was fetched.
func (f *fixtureServer) requests(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[path]
}

// serve answers one request. It asserts the credential arrived as HTTP Basic on
// every call, which is the same-run proof that no request in the corpus reaches
// the provider unauthenticated.
func (f *fixtureServer) serve(w http.ResponseWriter, r *http.Request) {
	if user, password, ok := r.BasicAuth(); !ok || user != "fixture-user" || password != "fixture-app-password" {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")

	f.mu.Lock()
	f.counts[path]++
	seen := f.counts[path]
	f.mu.Unlock()

	if delay, injected := f.slow[path]; injected {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
	}
	if status, injected := f.status[path]; injected {
		if status == http.StatusTooManyRequests {
			// A Retry-After of zero, which the client FLOORS at one second rather
			// than taking literally. It keeps the four-attempt row honest — every
			// attempt is really made and really waited on — without spending the
			// header's two-second default on it four times.
			w.Header().Set("Retry-After", "0")
		}
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"type":"error","error":{"message":"injected %d"}}`, status)
		return
	}
	if body, ok := f.raw[path]; ok {
		fmt.Fprint(w, body)
		return
	}
	f.servePage(w, r, path, seen)
}

// servePage answers one page of a paginated endpoint.
func (f *fixtureServer) servePage(w http.ResponseWriter, r *http.Request, path string, seen int) {
	pages, ok := f.pages[path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"type":"error","error":{"message":"Resource not found"}}`)
		return
	}
	index := 0
	if page := r.URL.Query().Get("page"); page != "" {
		fmt.Sscanf(page, "%d", &index) //nolint:errcheck // a malformed page reads as the first
	}
	if limit, injected := f.failAfter[path]; injected && index >= limit {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"type":"error","error":{"message":"Server Error"}}`)
		return
	}
	_ = seen
	if index >= len(pages) {
		index = len(pages) - 1
	}
	next := ""
	if index+1 < len(pages) {
		next = fmt.Sprintf("%s/%s?page=%d", f.server.URL, path, index+1)
	}
	// THE ENVELOPE IS MARSHALED RATHER THAN FORMATTED. The `next` URL is built
	// from the REQUEST's own path, so formatting it into a template writes a
	// request-derived string into a response body unescaped — which is the shape
	// the security linter names, and it is a real one even in a fixture: a path
	// carrying a quote would produce a body this module's own decoder could not
	// read, and the test would fail on the fixture rather than on the subject.
	w.Header().Set("Content-Type", "application/json")
	envelope, err := json.Marshal(struct {
		Values json.RawMessage `json:"values"`
		Next   string          `json:"next"`
	}{json.RawMessage(pages[index]), next})
	if err != nil {
		http.Error(w, "the fixture could not encode its own page", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(envelope)
}

// The recorded responses. They are RAW JSON handed to this module's own
// decoders, rather than Go literals of those decoders' structs, so the fixture
// exercises the decode as well as the conversion — including the fields the
// decoders deliberately declare no home for.
func recordedPages() map[string][]string {
	workspace := fixtureWorkspace
	api := fixtureAPIRepo
	web := fixtureWebRepo
	return map[string][]string{
		"repositories/" + workspace: {`[
			{"uuid":"{repo-api}","slug":"api","full_name":"acme/api","is_private":true,
			 "scm":"git","language":"go","mainbranch":{"name":"trunk"}},
			{"uuid":"{repo-web}","slug":"web","full_name":"acme/web","is_private":false,
			 "scm":"git","language":"","mainbranch":{"name":"main"}}
		]`},

		fmt.Sprintf("repositories/%s/%s/pipelines", workspace, api): {`[
			{"uuid":"{run-1}","build_number":41,"created_on":"2026-09-01T10:00:00Z",
			 "completed_on":"2026-09-01T10:04:00Z","duration_in_seconds":240,
			 "state":{"name":"COMPLETED","stage":{"name":""},"result":{"name":"SUCCESSFUL"}},
			 "target":{"ref_name":"trunk","ref_type":"branch"},"trigger":{"type":"push"}},
			{"uuid":"{run-2}","build_number":42,"created_on":"2026-09-02T10:00:00Z",
			 "completed_on":"","duration_in_seconds":0,
			 "state":{"name":"IN_PROGRESS","stage":{"name":"RUNNING"},"result":{"name":""}},
			 "target":{"ref_name":"","ref_type":""},"trigger":{"type":""}}
		]`},

		// TWO PAGES, so the corpus walk itself follows a `next` URL rather than
		// leaving that to the client's own suite.
		fmt.Sprintf("workspaces/%s/pipelines-config/runners", workspace): {
			`[{"uuid":"{runner-ws}","name":"workspace runner","state":{"status":"ONLINE"},
			   "labels":[{"name":"self-hosted"},{"name":"linux"}]}]`,
			`[{"uuid":"{runner-ws-2}","name":"second workspace runner","state":{"status":"OFFLINE"},
			   "labels":[{"name":"self-hosted"},{"name":""}]}]`,
		},
		repoRunnersPath(api): {
			`[{"uuid":"{runner-api}","name":"api runner","state":{"status":"ONLINE"},
			   "labels":[{"name":"self-hosted"}]}]`,
		},
		repoRunnersPath(web): {`[]`},

		// THE SECOND REPOSITORY HAS NEVER RUN A PIPELINE, and the provider says so
		// with an empty page rather than a 404 — the same shape it uses for that
		// repository's runners, variables and environments below. The 404 an absent
		// entry produces means the URL was not there, which is a different answer
		// and now reaches the walk as one.
		fmt.Sprintf("repositories/%s/%s/pipelines", workspace, web): {`[]`},

		// THE RESTRICTIONS OBJECT IS THE LIVE ONE, read off the provider with a
		// read-only credential: `admin_only` is a JSON BOOLEAN and the object
		// carries a `type`. The module declared a slice for it, so every
		// environments page failed to decode against the real API while this
		// fixture — which carried an empty array — decoded fine and said nothing.
		// A fixture that models a field's TYPE differently from the provider is
		// the same defect class as one that models a path differently.
		fmt.Sprintf("repositories/%s/%s/environments", workspace, api): {`[
			{"uuid":"{env-prod}","name":"production","slug":"production","rank":1,
			 "environment_type":{"name":"Production"},"lock":{"type":"lock"},
			 "restrictions":{"type":"deployment_restrictions_configuration","admin_only":false}},
			{"uuid":"{env-stage}","name":"staging","slug":"staging","rank":0,
			 "environment_type":{"name":""},
			 "lock":{"name":"OPEN","type":"deployment_environment_lock_open"},
			 "restrictions":{"type":"deployment_restrictions_configuration","admin_only":false}},
			{"uuid":"{env-review}","name":"review","slug":"review","rank":2,
			 "environment_type":{"name":"Test","rank":0,"type":"deployment_environment_type"},
			 "lock":{"name":"OPEN","type":"deployment_environment_lock_open"},
			 "restrictions":{"type":"deployment_restrictions_configuration","admin_only":true}}
		]`},
		fmt.Sprintf("repositories/%s/%s/environments", workspace, web): {`[]`},

		fmt.Sprintf("workspaces/%s/pipelines-config/variables", workspace): {
			`[{"uuid":"{var-ws-1}","key":"WORKSPACE_TOKEN","secured":true,"system":false,
			   "value":"workspace-secret-value-should-never-be-stored"}]`,
			`[{"uuid":"{var-ws-2}","key":"DEPLOY_KEY","secured":true,"system":false,
			   "value":"workspace-scoped-deploy-key"}]`,
		},
		repoVariablesPath(api): {
			`[{"uuid":"{var-api-1}","key":"API_KEY","secured":true,"system":false,
			   "value":"repository-secret-value-should-never-be-stored"},
			  {"uuid":"{var-api-2}","key":"DEPLOY_KEY","secured":false,"system":false,
			   "value":"repository-scoped-deploy-key"}]`,
		},
		repoVariablesPath(web): {`[]`},
		fmt.Sprintf(
			"repositories/%s/%s/deployments_config/environments/%s/variables",
			workspace, api, "{env-prod}"): {
			`[{"uuid":"{var-env-1}","key":"DEPLOY_KEY","secured":true,"system":false,
			   "value":"environment-scoped-deploy-key"}]`,
		},
		fmt.Sprintf(
			"repositories/%s/%s/deployments_config/environments/%s/variables",
			workspace, api, "{env-stage}"): {`[]`},
		fmt.Sprintf(
			"repositories/%s/%s/deployments_config/environments/%s/variables",
			workspace, api, "{env-review}"): {`[]`},
	}
}

// recordedRaw is the non-paginated content: one repository's pipeline
// definition. The other repository has none, which the absent entry answers as a
// 404 exactly as the provider does.
func recordedRaw() map[string]string {
	return map[string]string{
		fmt.Sprintf("repositories/%s/%s/src/trunk/bitbucket-pipelines.yml",
			fixtureWorkspace, fixtureAPIRepo): fixturePipelineYAML,
	}
}

// fixturePipelineYAML is the recorded pipeline definition. It carries all five
// trigger sections, a `parallel` entry with no `step` key, and one step of each
// shape the converters branch on.
//
// THE CUSTOM PIPELINE NAMES AN ENVIRONMENT AND A LABEL NOTHING PROVIDES, and
// that is the ordinary case rather than a contrived one: a step's `deployment`
// and its `runs-on` are read from this file, while the environments and the
// runners are read from the API by two other enumerations. A workspace holds
// both mismatches every day — an environment nobody created in the deployments
// config, a label no online runner advertises — and neither enumeration can
// supply the node.
const fixturePipelineYAML = `
pipelines:
  default:
    - step:
        name: build
        script:
          - echo "building with $API_KEY and $HOME"
          - 'curl -H "token: ${WORKSPACE_TOKEN}" https://example.invalid'
          - echo "$MISSING_VAR"
        runs-on:
          - self-hosted
          - linux
        services:
          - docker
        caches:
          - go
    - parallel:
        - step:
            name: ignored by this collector
            script:
              - echo "$IGNORED_BY_PARALLEL"
  branches:
    trunk:
      - step:
          name: deploy
          deployment: production
          script:
            - deploy --key "$DEPLOY_KEY" --also "$API_KEY"
  pull-requests:
    '**':
      - step:
          name: verify
          script:
            - make verify
  custom:
    nightly:
      - step:
          name: nightly
          deployment: qa
          runs-on:
            - gpu-only
          script:
            - make nightly
  tags:
    'v*':
      - step:
          name: release
          script:
            - make release
`

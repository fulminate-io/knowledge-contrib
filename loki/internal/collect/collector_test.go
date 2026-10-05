// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/loki/internal/logpipe"
	"github.com/fulminate-io/knowledge-contrib/loki/internal/lokiapi"
)

// collector_test.go — the params refusals and the walk end to end against a
// fake Loki. The advertised schema is schema_test.go's.

// TestParamsRefusals covers every arm of the validation, each naming what is
// wrong. The knowledge client's own pre-call check enforces that the address is
// a required string and nothing about what the string says, so every refusal
// below is this collector's own.
func TestParamsRefusals(t *testing.T) {
	valid := Params{Address: "http://localhost:3100", Start: "2026-09-07T12:00:00Z", End: "2026-09-07T13:00:00Z"}
	cases := []struct {
		name   string
		mutate func(p *Params)
		want   string
	}{
		{name: "an empty address", mutate: func(p *Params) { p.Address = "" }, want: "empty"},
		{name: "an address with no scheme", mutate: func(p *Params) { p.Address = "localhost:3100" }, want: "scheme"},
		{name: "a file:// address", mutate: func(p *Params) { p.Address = "file:///etc/passwd" }, want: "scheme"},
		{name: "an address with no host", mutate: func(p *Params) { p.Address = "https://" }, want: "no host"},
		{name: "an empty start", mutate: func(p *Params) { p.Start = "" }, want: `"start" bound is empty`},
		{name: "an empty end", mutate: func(p *Params) { p.End = "" }, want: `"end" bound is empty`},
		{name: "a start that is not RFC 3339", mutate: func(p *Params) { p.Start = "yesterday" }, want: "not an RFC 3339"},
		{name: "an end that is not RFC 3339", mutate: func(p *Params) { p.End = "2026-09-07" }, want: "not an RFC 3339"},
		{name: "an inverted window", mutate: func(p *Params) { p.Start, p.End = p.End, p.Start }, want: "start must be before end"},
		{name: "an instantaneous window", mutate: func(p *Params) { p.End = p.Start }, want: "start must be before end"},
		{name: "a severity outside the vocabulary", mutate: func(p *Params) { p.SeverityMin = "ERR0R" }, want: "is not a severity"},
		{name: "a lowercase severity, which is not the canonical spelling", mutate: func(p *Params) { p.SeverityMin = "error" }, want: "is not a severity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := valid
			tc.mutate(&p)
			_, err := p.validate()
			if err == nil {
				t.Fatalf("the params were accepted: %+v", p)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error does not say %q: %v", tc.want, err)
			}
		})
	}

	// THE CONTROL: the unmutated params validate. Without it every case above
	// would also pass on a validator that refused everything.
	if _, err := valid.validate(); err != nil {
		t.Fatalf("the valid params were refused: %v", err)
	}
}

// TestSeverityMinAcceptsEachCanonicalLevel is the severity control, one case
// per level, so "is not a severity" above is a statement about the vocabulary
// rather than about the check being unreachable.
func TestSeverityMinAcceptsEachCanonicalLevel(t *testing.T) {
	base := Params{Address: "http://localhost:3100", Start: "2026-09-07T12:00:00Z", End: "2026-09-07T13:00:00Z"}
	for _, level := range []string{
		logpipe.SeverityTrace, logpipe.SeverityDebug, logpipe.SeverityInfo,
		logpipe.SeverityWarn, logpipe.SeverityError, logpipe.SeverityCritical,
	} {
		p := base
		p.SeverityMin = level
		r, err := p.validate()
		if err != nil {
			t.Fatalf("severity_min %q was refused: %v", level, err)
		}
		if r.query.SeverityMin != level {
			t.Fatalf("severity_min %q reached the query as %q", level, r.query.SeverityMin)
		}
	}
	// An empty severity is not a filter at all and must pass through as one.
	r, err := base.validate()
	if err != nil {
		t.Fatalf("no severity_min was refused: %v", err)
	}
	if r.query.SeverityMin != "" {
		t.Fatalf("an absent severity_min became %q", r.query.SeverityMin)
	}
}

// TestValidatedParamsReachTheQueryVerbatim covers the plumbing between the
// params and the reader, which nothing else asserts.
func TestValidatedParamsReachTheQueryVerbatim(t *testing.T) {
	p := Params{
		Address:      "http://localhost:3100/",
		Start:        "2026-09-07T12:00:00Z",
		End:          "2026-09-07T13:00:00.5Z",
		Selector:     `{app="checkout"}`,
		Source:       "prod",
		FieldFilters: map[string]string{"pod": "p1"},
		TextFilter:   "timeout",
		SeverityMin:  logpipe.SeverityError,
		RawQuery:     "| json",
	}
	r, err := p.validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if r.address != "http://localhost:3100" {
		t.Fatalf("address = %q, want the normalized form", r.address)
	}
	if !r.start.Equal(time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("start = %s", r.start)
	}
	if !r.end.Equal(time.Date(2026, 9, 7, 13, 0, 0, 500000000, time.UTC)) {
		t.Fatalf("end = %s; a fractional second must survive", r.end)
	}
	if r.query.Selector != p.Selector || r.query.Source != p.Source ||
		r.query.TextFilter != p.TextFilter || r.query.RawQuery != p.RawQuery ||
		r.query.FieldFilters["pod"] != "p1" {
		t.Fatalf("the query does not carry the params verbatim: %+v", r.query)
	}
}

// TestWalkProducesTheLogGraph is the collector end to end against a fake Loki,
// over the real params, the real reader and the real pipeline.
func TestWalkProducesTheLogGraph(t *testing.T) {
	end := time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{"result": []map[string]any{{
				"stream": map[string]string{"app": "checkout", "instance": "host-3"},
				"values": [][]string{
					{strconv.FormatInt(end.UnixNano(), 10), "disk pressure detected"},
					{strconv.FormatInt(end.Add(-time.Second).UnixNano(), 10), "disk pressure detected"},
				},
			}}},
		})
	}))
	defer srv.Close()

	c := &Collector{Lookup: func(string) (string, bool) { return "", false }}
	got, err := c.Walk(context.Background(), "my-collect-id", Params{
		Address: srv.URL,
		Start:   "2026-09-07T12:00:00Z",
		End:     "2026-09-07T13:00:01Z",
	}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !got.Complete.IsAsserted() {
		t.Fatal("the walk returned an unasserted completeness")
	}
	if !got.Complete.IsComplete() {
		t.Fatalf("the walk asserted incomplete: %s", got.Complete.Reason())
	}

	types := map[string]int{}
	for _, n := range got.Nodes {
		types[n.Type]++
	}
	for _, want := range []string{"log-template", "log-stream", "log-chunk", "log-label"} {
		if types[want] == 0 {
			t.Fatalf("no %s node in the walk's result; got %v", want, types)
		}
	}
	edges := map[string]int{}
	for _, e := range got.Edges {
		edges[e.Type]++
	}
	for _, want := range []string{"HAS_LABEL", "BELONGS_TO", "CONTAINS"} {
		if edges[want] == 0 {
			t.Fatalf("no %s edge in the walk's result; got %v", want, edges)
		}
	}
	// The stream carries the alias the Loki arm derives.
	for _, n := range got.Nodes {
		if n.Type == "log-stream" && n.SymbolName != "checkout@host-3" {
			t.Fatalf("the stream is named %q, want %q", n.SymbolName, "checkout@host-3")
		}
	}
}

// TestTheCollectIDDoesNotReachTheEmittedNodes is what makes a second collect
// under one id a DIFF rather than a new graph: the same window produces the
// same node ids whatever id the operator supplies.
func TestTheCollectIDDoesNotReachTheEmittedNodes(t *testing.T) {
	end := time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{"result": []map[string]any{{
				"stream": map[string]string{"app": "checkout"},
				"values": [][]string{{strconv.FormatInt(end.UnixNano(), 10), "disk pressure detected"}},
			}}},
		})
	}))
	defer srv.Close()

	c := &Collector{Lookup: func(string) (string, bool) { return "", false }}
	params := Params{Address: srv.URL, Start: "2026-09-07T12:00:00Z", End: "2026-09-07T13:00:01Z"}

	first, err := c.Walk(context.Background(), "collect-a", params, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	second, err := c.Walk(context.Background(), "collect-b", params, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(first.Nodes) != len(second.Nodes) {
		t.Fatalf("the two collects produced %d and %d nodes", len(first.Nodes), len(second.Nodes))
	}
	for i := range first.Nodes {
		if first.Nodes[i].ID != second.Nodes[i].ID {
			t.Fatalf("node %d differs between two collect ids: %q and %q", i, first.Nodes[i].ID, second.Nodes[i].ID)
		}
	}
}

// TestWalkFailsRatherThanReturningAnEmptyResult covers the refusal arms at the
// collector level. NONE of them may return a partial graph: the knowledge
// client writes nothing on a refused collect, and a provider that returned half
// a walk asserting completeness would have lied.
func TestWalkFailsRatherThanReturningAnEmptyResult(t *testing.T) {
	c := &Collector{Lookup: func(string) (string, bool) { return "", false }}
	cases := []struct {
		name   string
		params Params
	}{
		{name: "a bad address", params: Params{Address: "not-a-url", Start: "2026-09-07T12:00:00Z", End: "2026-09-07T13:00:00Z"}},
		{name: "a bad window", params: Params{Address: "http://localhost:1", Start: "nope", End: "2026-09-07T13:00:00Z"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Walk(context.Background(), "id", tc.params, framework.ForeignContext{})
			if err == nil {
				t.Fatal("the walk returned no error")
			}
			if len(got.Nodes) != 0 || len(got.Edges) != 0 {
				t.Fatalf("a refused walk returned %d nodes and %d edges", len(got.Nodes), len(got.Edges))
			}
			if got.Complete.IsAsserted() {
				t.Fatal("a refused walk asserted a completeness")
			}
		})
	}
}

// TestAMalformedEnvironmentFailsTheCollectRatherThanBeingIgnored. The
// environment is read inside the walk, so this is the arm that carries a bad
// entry to the operator.
func TestAMalformedEnvironmentFailsTheCollectRatherThanBeingIgnored(t *testing.T) {
	c := &Collector{Lookup: func(name string) (string, bool) {
		if name == lokiapi.EnvTLSSkipVerify {
			return "yes", true
		}
		return "", false
	}}
	_, err := c.Walk(context.Background(), "id", Params{
		Address: "http://localhost:3100", Start: "2026-09-07T12:00:00Z", End: "2026-09-07T13:00:00Z",
	}, framework.ForeignContext{})
	if err == nil {
		t.Fatal("a malformed environment value was ignored")
	}
	if !strings.Contains(err.Error(), lokiapi.EnvTLSSkipVerify) {
		t.Fatalf("the error does not name the variable: %v", err)
	}
}

// TestTheToolIsNamedAndDescribed covers the config entry's `tool` field, which
// an operator copies into their entry by hand.
func TestTheToolIsNamedAndDescribed(t *testing.T) {
	spec := (&Collector{}).Tool()
	// THE LITERAL, not the constant: comparing the served name against the
	// constant it is built from is a subject supplying its own answer key, and
	// it passes just as loudly when the constant is the empty string — which
	// would serve the framework's default name and make an operator's config
	// entry name a tool this collector does not advertise.
	if spec.Name != "collect_loki_logs" {
		t.Fatalf("the tool is named %q, want %q", spec.Name, "collect_loki_logs")
	}
	if ToolName != spec.Name {
		t.Fatalf("the exported ToolName is %q and the served name is %q", ToolName, spec.Name)
	}
	if spec.Description == "" {
		t.Fatal("the tool carries no description; an operator installing it reads that line")
	}
	if !strings.Contains(spec.Description, "Loki") {
		t.Fatalf("the description does not say what it collects: %q", spec.Description)
	}
}

// TestAnEmptyCollectIDIsRefusedByTheFramework records where that refusal lives.
// The advertised schema can require the id to be PRESENT and a string; it
// cannot require it to be non-empty, so the check is the framework's handler
// and this collector inherits it rather than repeating it.
func TestAnEmptyCollectIDIsRefusedByTheFramework(t *testing.T) {
	// The walk itself does not read the id, which is the property that makes
	// the framework's check the only one needed.
	c := &Collector{Lookup: func(string) (string, bool) { return "", false }}
	if _, err := c.Walk(context.Background(), "", Params{Address: "bad"}, framework.ForeignContext{}); err == nil {
		t.Fatal("the walk accepted bad params")
	} else if strings.Contains(err.Error(), "collect id") {
		t.Fatalf("the walk refused the empty id itself; that check is the framework's: %v", err)
	}
}

// TestAnIncompleteReadReachesTheResultsCompletenessAssertion is the collector's
// own plumbing of the reader's completeness onto the envelope, which nothing
// else covers: the reader's arms are tested where the reader lives, and this is
// the row that says the walk carries the answer rather than asserting complete
// unconditionally.
//
// The fake serves one full page whose entries all carry ONE timestamp, which is
// the condition the reader cannot narrow past.
func TestAnIncompleteReadReachesTheResultsCompletenessAssertion(t *testing.T) {
	const pageLimit = 5000
	instant := time.Date(2026, 9, 7, 12, 30, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		values := make([][]string, 0, pageLimit)
		for range pageLimit {
			values = append(values, []string{strconv.FormatInt(instant.UnixNano(), 10), "disk pressure detected"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{"result": []map[string]any{
				{"stream": map[string]string{"app": "checkout"}, "values": values},
			}},
		})
	}))
	defer srv.Close()

	c := &Collector{Lookup: func(string) (string, bool) { return "", false }}
	got, err := c.Walk(context.Background(), "id", Params{
		Address: srv.URL, Start: "2026-09-07T12:00:00Z", End: "2026-09-07T13:00:00Z",
	}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !got.Complete.IsAsserted() {
		t.Fatal("the walk returned an unasserted completeness")
	}
	if got.Complete.IsComplete() {
		t.Fatal("the walk asserted COMPLETE over a read that reported itself incomplete")
	}
	if got.Complete.Reason() == "" {
		t.Fatal("the incomplete assertion carries no reason; the framework refuses one and the operator learns nothing")
	}
	if !strings.Contains(got.Complete.Reason(), "all carry the timestamp") {
		t.Fatalf("the reason does not say why the read stopped: %q", got.Complete.Reason())
	}
	// IT STILL RETURNS WHAT IT READ. An incomplete walk is not an empty one:
	// the nodes are written and only the deletion phase is disabled.
	if len(got.Nodes) == 0 {
		t.Fatal("an incomplete walk returned no nodes; it read a full page")
	}

	// THE CONTROL, in the same file: a read that completes asserts complete.
	// TestWalkProducesTheLogGraph is that control, and it runs in this package.
}

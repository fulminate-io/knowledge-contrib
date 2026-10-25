// SPDX-License-Identifier: Apache-2.0

// Package walk is the Notion collector's implementation of the framework's
// collector interface. A walk validates the collect's params, reads the
// token, and reads every named page (with every block, recursively, and every
// unresolved comment), every entry of every named database and data source,
// and every title-search hit, then returns the graph with its completeness
// assertion.
package walk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/notionapi"
)

// Collector is the walk the framework serves.
type Collector struct {
	// Client builds the API client a walk reads through from the token. Nil
	// means [notionapi.New].
	Client ClientBuilder
	// ToolName is the served tool's name. Empty means the framework default.
	ToolName string
}

// ClientBuilder builds the API client for one walk.
type ClientBuilder func(token string) *notionapi.Client

// Tool names the MCP tool this collector serves.
func (c Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: c.ToolName,
		Description: "Read Notion pages (optionally with their descendants), databases, data sources (with an optional " +
			"filter) and title searches with your own Notion token: every block's text, every entry and every unresolved comment.",
		Summary: "Notion pages, databases and comments",
	}
}

// Walk reads the collect's targets in the order pages, databases, data
// sources, title searches.
func (c Collector) Walk(ctx context.Context, _ string, params Params, _ framework.ForeignContext) (framework.Result, error) {
	if err := params.validate(); err != nil {
		return framework.Result{}, err
	}
	tok, err := token()
	if err != nil {
		return framework.Result{}, err
	}
	build := c.Client
	if build == nil {
		build = notionapi.New
	}
	wk := &walker{
		client: build(tok), graph: newGraph(), descendants: params.Descendants,
		pages: map[string]bool{}, databases: map[string]bool{}, sources: map[string]bool{},
	}
	if err := wk.run(ctx, params); err != nil {
		return framework.Result{}, err
	}
	nodes, edges := wk.graph.result()
	return framework.Result{Nodes: nodes, Edges: edges, Complete: wk.completeness()}, nil
}

// walker carries one walk's state.
type walker struct {
	client      *notionapi.Client
	graph       *graph
	descendants bool
	pages       map[string]bool
	databases   map[string]bool
	// sources is the data source queries made so far, keyed by sourceReadKey.
	sources    map[string]bool
	incomplete []string
	cancelled  error
}

func (wk *walker) run(ctx context.Context, params Params) error {
	for _, id := range params.Pages {
		if err := wk.readPage(ctx, id, true); err != nil {
			return err
		}
	}
	for _, id := range params.Databases {
		if err := wk.readDatabase(ctx, id, true); err != nil {
			return err
		}
	}
	for i, ds := range params.DataSources {
		if err := wk.readNamedDataSource(ctx, ds, fmt.Sprintf("params.data_sources[%d].filter", i)); err != nil {
			return err
		}
	}
	for _, q := range params.TitleSearch {
		if err := wk.readSearch(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// stopped reports whether the walk's context ended; the first time it does,
// the cause is recorded.
func (wk *walker) stopped(ctx context.Context) bool {
	if wk.cancelled != nil {
		return true
	}
	if err := ctx.Err(); err != nil {
		wk.cancelled = err
		return true
	}
	return false
}

func (wk *walker) completeness() framework.Completeness {
	marks := slices.Clone(wk.incomplete)
	if wk.cancelled != nil {
		marks = append(marks, fmt.Sprintf("the walk was cancelled before it finished: %v", wk.cancelled))
	}
	if len(marks) == 0 {
		return framework.Complete()
	}
	return framework.Incomplete(fmt.Sprintf("%d read(s) did not return their whole scope: %s",
		len(marks), strings.Join(marks, "; ")))
}

// failure is the context fail classifies a read's error in.
type failure struct {
	// scope names what the read was for, such as "page <id>".
	scope string
	// named is true when the read's id came from the collect's params.
	named bool
	// filterParam is the param path of the user's filter the read sent, or "".
	filterParam string
}

// fail classifies a read's error. It returns the error that ends the collect,
// or nil after recording the read as incomplete.
func (wk *walker) fail(f failure, err error) error {
	var apiErr *notionapi.APIError
	if !errors.As(err, &apiErr) {
		wk.incomplete = append(wk.incomplete, fmt.Sprintf("%s: %v", f.scope, err))
		return nil
	}
	switch {
	case apiErr.Status == http.StatusUnauthorized:
		return fmt.Errorf("notion: %s %s answered 401 %s: the token in %s is not valid; a personal access token "+
			"expires and has no refresh, so create a new one and set %s", apiErr.Method, apiErr.Path, apiErr.Code,
			TokenVariable, TokenVariable)
	case apiErr.Status == http.StatusNotFound && f.named:
		return fmt.Errorf("notion: %s answered 404 %s for %s: it does not exist, or it is not shared with or visible to "+
			"the owner of the token in %s", apiErr.Path, apiErr.Code, f.scope, TokenVariable)
	case apiErr.Status == http.StatusNotFound:
		wk.incomplete = append(wk.incomplete, fmt.Sprintf("%s: %v", f.scope, apiErr))
		return nil
	case apiErr.Status == http.StatusBadRequest && f.filterParam != "":
		return fmt.Errorf("notion: %s answered 400 %s on %s for %s: %s", apiErr.Path, apiErr.Code, f.filterParam, f.scope, apiErr.Message)
	case apiErr.Status >= http.StatusInternalServerError:
		wk.incomplete = append(wk.incomplete, fmt.Sprintf("%s: %v", f.scope, apiErr))
		return nil
	default:
		return fmt.Errorf("notion: %v (reading %s)", apiErr, f.scope)
	}
}

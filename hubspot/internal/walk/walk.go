// SPDX-License-Identifier: Apache-2.0

// Package walk is the HubSpot collector's implementation of the framework's
// collector interface. A walk validates the collect's params, reads the
// token, reads every named record, every member of every named list and every
// search hit, then reads the records associated with each of them, one hop,
// and returns the graph with its completeness assertion.
package walk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/hubapi"
)

// Collector is the walk the framework serves.
type Collector struct {
	// Client builds the API client a walk reads through from the token. Nil
	// means [hubapi.New].
	Client ClientBuilder
	// ToolName is the served tool's name. Empty means the framework default.
	ToolName string
}

// ClientBuilder builds the API client for one walk.
type ClientBuilder func(token string) *hubapi.Client

// Tool names the MCP tool this collector serves.
func (c Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: c.ToolName,
		Description: "Read HubSpot companies, contacts, deals, tickets, list members and CRM search hits with your own " +
			"HubSpot token, each with the contacts, companies, deals, tickets, calls, emails, meetings, notes and tasks " +
			"associated with it.",
		Summary: "HubSpot CRM records and their associations",
	}
}

// Walk reads the named records, lists and searches, then their associations.
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
		build = hubapi.New
	}
	wk := &walker{
		client: build(tok), graph: newGraph(), properties: params.Properties,
		read: map[string]map[string]bool{}, targets: map[string][]string{}, isTarget: map[string]bool{},
	}
	if err := wk.run(ctx, params); err != nil {
		return framework.Result{}, err
	}
	nodes, edges := wk.graph.result()
	return framework.Result{Nodes: nodes, Edges: edges, Complete: wk.completeness()}, nil
}

// walker carries one walk's state.
type walker struct {
	client     *hubapi.Client
	graph      *graph
	properties map[string][]string
	// read is the record ids read so far, by object type.
	read map[string]map[string]bool
	// targets are the records whose associations the walk reads, by object
	// type, in the order they were met.
	targets    map[string][]string
	isTarget   map[string]bool
	incomplete []string
	cancelled  error
}

func (wk *walker) run(ctx context.Context, params Params) error {
	for _, named := range []struct {
		objectType string
		ids        []string
	}{{"companies", params.Companies}, {"contacts", params.Contacts}, {"deals", params.Deals}, {"tickets", params.Tickets}} {
		if err := wk.readRecords(ctx, named.objectType, named.ids, true, true); err != nil {
			return err
		}
	}
	for _, id := range params.Lists {
		if err := wk.readList(ctx, id); err != nil {
			return err
		}
	}
	for i, s := range params.Searches {
		if err := wk.readSearch(ctx, s, fmt.Sprintf("params.searches[%d].filter_groups", i)); err != nil {
			return err
		}
	}
	return wk.readAssociations(ctx)
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

// markIncomplete records a read that did not return its whole scope.
func (wk *walker) markIncomplete(format string, args ...any) {
	wk.incomplete = append(wk.incomplete, fmt.Sprintf(format, args...))
}

// failure is the context fail classifies a read's error in.
type failure struct {
	// scope names what the read was for, such as "contacts 101, 102".
	scope string
	// objectType is the object type the read reads, for naming its scope.
	objectType string
	// named is true when the read's ids came from the collect's params.
	named bool
	// filterParam is the param path of the user's filter_groups, or "".
	filterParam string
}

// readScopes are the token scopes HubSpot names for reading each object type.
var readScopes = map[string]string{
	"contacts":  "crm.objects.contacts.read",
	"companies": "crm.objects.companies.read",
	"deals":     "crm.objects.deals.read",
	"tickets":   "tickets",
	"lists":     "crm.lists.read",
}

// fail classifies a read's error. It returns the error that ends the collect,
// or nil after recording the read as incomplete.
func (wk *walker) fail(f failure, err error) error {
	var apiErr *hubapi.APIError
	if !errors.As(err, &apiErr) {
		wk.markIncomplete("%s: %v", f.scope, err)
		return nil
	}
	switch {
	case apiErr.Status == http.StatusUnauthorized:
		return fmt.Errorf("hubspot: %s %s answered 401: the token in %s is not valid; set it to a Service Key or a "+
			"private-app access token", apiErr.Method, apiErr.Path, TokenVariable)
	case apiErr.Status == http.StatusForbidden:
		scope := readScopes[f.objectType]
		if scope == "" {
			scope = "the scope that reads " + f.objectType
		}
		return fmt.Errorf("hubspot: %s %s answered 403 for %s: the token in %s lacks %s (%s)",
			apiErr.Method, apiErr.Path, f.scope, TokenVariable, scope, apiErr.Message)
	case apiErr.Status == http.StatusNotFound && f.named:
		return fmt.Errorf("hubspot: %s answered 404 for %s: it does not exist or the token cannot see it", apiErr.Path, f.scope)
	case apiErr.Status == http.StatusNotFound:
		wk.markIncomplete("%s: %v", f.scope, apiErr)
		return nil
	case apiErr.Status == http.StatusBadRequest && f.filterParam != "":
		return fmt.Errorf("hubspot: %s answered 400 on %s for %s: %s", apiErr.Path, f.filterParam, f.scope, apiErr.Message)
	case apiErr.Status >= http.StatusInternalServerError:
		wk.markIncomplete("%s: %v", f.scope, apiErr)
		return nil
	default:
		return fmt.Errorf("hubspot: %v (reading %s)", apiErr, f.scope)
	}
}

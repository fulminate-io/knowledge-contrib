// SPDX-License-Identifier: Apache-2.0

// Package walk is the Slack collector's implementation of the framework's
// collector interface. A walk validates the collect's params, reads the token,
// reads every named channel, thread and search query to its last page, reads
// each distinct poster once, and returns the graph with its completeness
// assertion.
package walk

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/slack/internal/slackapi"
)

// Collector is the walk the framework serves.
type Collector struct {
	// Client builds the API client a walk reads through from the token. Nil
	// means [slackapi.New].
	Client ClientBuilder
	// ToolName is the served tool's name. Empty means the framework default.
	ToolName string
}

// ClientBuilder builds the API client for one walk.
type ClientBuilder func(token string) *slackapi.Client

// Tool names the MCP tool this collector serves.
func (c Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: c.ToolName,
		Description: "Read Slack channels, single threads and search.messages queries with your own Slack token: " +
			"the channels, every message and thread reply in the optional time window, and the users who posted them.",
		Summary: "Slack channels, threads and message searches",
	}
}

// tokenRejections are the `error` codes with which Slack refuses the token
// itself.
var tokenRejections = []string{"invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive"}

// methodScopes are the token scopes each method's reference page names.
var methodScopes = map[string]string{
	"conversations.history": "channels:history, groups:history, im:history, mpim:history",
	"conversations.replies": "channels:history, groups:history, im:history, mpim:history",
	"conversations.info":    "channels:read, groups:read, im:read, mpim:read",
	"users.info":            "users:read",
	"search.messages":       "search:read",
}

// Walk reads the collect's targets in the order channels, threads, searches,
// then the posters' profiles.
func (c Collector) Walk(ctx context.Context, _ string, params Params, _ framework.ForeignContext) (framework.Result, error) {
	w, err := params.validate()
	if err != nil {
		return framework.Result{}, err
	}
	tok, err := token()
	if err != nil {
		return framework.Result{}, err
	}
	build := c.Client
	if build == nil {
		build = slackapi.New
	}
	wk := &walker{client: build(tok), window: w, graph: newGraph(), infoRead: map[string]bool{}}
	if err := wk.run(ctx, params); err != nil {
		return framework.Result{}, err
	}
	nodes, edges := wk.graph.result()
	return framework.Result{Nodes: nodes, Edges: edges, Complete: wk.completeness()}, nil
}

// walker carries one walk's state.
type walker struct {
	client     *slackapi.Client
	window     window
	graph      *graph
	infoRead   map[string]bool
	incomplete []string
	cancelled  error
}

func (wk *walker) run(ctx context.Context, params Params) error {
	for _, channel := range params.Channels {
		if err := wk.readChannel(ctx, channel); err != nil {
			return err
		}
	}
	for _, t := range params.Threads {
		if err := wk.readInfo(ctx, t.Channel); err != nil {
			return err
		}
		if err := wk.readThread(ctx, t.Channel, t.TS); err != nil {
			return err
		}
	}
	for _, query := range params.Search {
		if err := wk.readSearch(ctx, query); err != nil {
			return err
		}
	}
	return wk.readPosters(ctx)
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

// fail classifies a read's error. It returns the error that ends the collect,
// or nil after recording the read as incomplete. scope names what the read was
// for, such as "channel C123".
func (wk *walker) fail(scope string, err error) error {
	var apiErr *slackapi.APIError
	switch {
	case errors.As(err, &apiErr):
		return wk.failAPI(scope, apiErr)
	default:
		wk.incomplete = append(wk.incomplete, fmt.Sprintf("%s: %v", scope, err))
		return nil
	}
}

func (wk *walker) failAPI(scope string, e *slackapi.APIError) error {
	switch {
	case slices.Contains(tokenRejections, e.Code):
		return fmt.Errorf("slack: %s refused the token in %s with %s; set %s to a valid bot (xoxb) or user (xoxp) token",
			e.Method, TokenVariable, e.Code, TokenVariable)
	case e.Code == "missing_scope":
		return fmt.Errorf("slack: %s answered missing_scope for %s: the token in %s lacks a scope this read needs "+
			"(the method takes %s; Slack reported needed %q, provided %q)",
			e.Method, scope, TokenVariable, methodScopes[e.Method], e.Needed, e.Provided)
	case e.Code == "not_allowed_token_type":
		return fmt.Errorf("slack: %s answered not_allowed_token_type for %s: params.search needs a user (xoxp) token "+
			"with the search:read scope in %s", e.Method, scope, TokenVariable)
	case e.Code == "invalid_cursor" || e.Code == "ratelimited" || e.Code == "user_not_found":
		wk.incomplete = append(wk.incomplete, fmt.Sprintf("%s: %s answered %s", scope, e.Method, e.Code))
		return nil
	default:
		return fmt.Errorf("slack: %s answered %s for %s", e.Method, e.Code, scope)
	}
}

// readInfo reads conversations.info for channel once per walk.
func (wk *walker) readInfo(ctx context.Context, channel string) error {
	if wk.infoRead[channel] || wk.stopped(ctx) {
		return nil
	}
	wk.infoRead[channel] = true
	info, err := wk.client.ConversationInfo(ctx, channel)
	if err != nil {
		if ferr := wk.fail("channel "+channel, err); ferr != nil {
			return ferr
		}
		wk.graph.addChannelStub(slackapi.MatchChannel{ID: channel})
		return nil
	}
	wk.graph.addChannel(info)
	return nil
}

// readChannel reads one channel's info, every history page in the window, and
// the replies of every thread parent the history carried.
func (wk *walker) readChannel(ctx context.Context, channel string) error {
	if err := wk.readInfo(ctx, channel); err != nil {
		return err
	}
	scope := "channel " + channel
	var parents []string
	cursor := ""
	for !wk.stopped(ctx) {
		page, err := wk.client.History(ctx, channel, cursor, wk.window.slack())
		if err != nil {
			if ferr := wk.fail(scope, err); ferr != nil {
				return ferr
			}
			break
		}
		for _, m := range page.Messages {
			wk.graph.addMessage(channel, m)
			if isThreadParent(m) {
				parents = append(parents, m.TS)
			}
		}
		if page.IsLimited {
			wk.incomplete = append(wk.incomplete, scope+": conversations.history answered is_limited, so older "+
				"messages exist beyond the workspace's free-plan message limit and no call returns them")
		}
		cursor = page.ResponseMetadata.NextCursor
		if cursor == "" {
			break
		}
	}
	for _, ts := range parents {
		if err := wk.readThread(ctx, channel, ts); err != nil {
			return err
		}
	}
	return nil
}

// readThread reads every conversations.replies page of the thread whose
// parent is ts.
func (wk *walker) readThread(ctx context.Context, channel, ts string) error {
	scope := "thread " + channel + "/" + ts
	cursor := ""
	for !wk.stopped(ctx) {
		page, err := wk.client.Replies(ctx, channel, ts, cursor, wk.window.slack())
		if err != nil {
			return wk.fail(scope, err)
		}
		for _, m := range page.Messages {
			wk.graph.addMessage(channel, m)
		}
		cursor = page.ResponseMetadata.NextCursor
		if cursor == "" {
			return nil
		}
	}
	return nil
}

// readSearch reads every search.messages page of query and keeps the matches
// inside the window. The walk is incomplete when paging.total exceeds the
// matches read.
func (wk *walker) readSearch(ctx context.Context, query string) error {
	scope := fmt.Sprintf("search %q", query)
	cursor := slackapi.SearchFirstCursor
	read, total := 0, 0
	for !wk.stopped(ctx) {
		page, err := wk.client.Search(ctx, query, cursor)
		if err != nil {
			return wk.fail(scope, err)
		}
		total = page.Messages.Paging.Total
		for _, match := range page.Messages.Matches {
			read++
			if !wk.window.isOpen() && !wk.window.contains(match.TS) {
				continue
			}
			wk.graph.addChannelStub(match.Channel)
			wk.graph.addMessage(match.Channel.ID, match.Message)
		}
		cursor = page.ResponseMetadata.NextCursor
		if cursor == "" {
			break
		}
	}
	if total > read {
		wk.incomplete = append(wk.incomplete, fmt.Sprintf("%s: search.messages reported %d matches and %d were read", scope, total, read))
	}
	return nil
}

// readPosters reads users.info once for every user a message named.
func (wk *walker) readPosters(ctx context.Context) error {
	for _, user := range wk.graph.sortedPosters() {
		if wk.stopped(ctx) {
			return nil
		}
		u, err := wk.client.UserInfo(ctx, user)
		if err != nil {
			if ferr := wk.fail("user "+user, err); ferr != nil {
				return ferr
			}
			wk.graph.addUserStub(user)
			continue
		}
		wk.graph.addUser(u)
	}
	return nil
}

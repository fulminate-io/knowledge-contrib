// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/notionapi"
)

// queryCapped is the request_status type Notion sets on a data source query
// it stopped at its result limit.
const queryCapped = "incomplete"

// readDatabase retrieves database id and reads every entry of each of its
// data sources.
func (wk *walker) readDatabase(ctx context.Context, id string, named bool) error {
	if wk.databases[id] || wk.stopped(ctx) {
		return nil
	}
	wk.databases[id] = true
	db, err := wk.client.Database(ctx, id)
	if err != nil {
		return wk.fail(failure{scope: "database " + id, named: named}, err)
	}
	wk.graph.addDatabase(db)
	for _, ds := range db.DataSources {
		wk.graph.addDataSource(ds.ID, ds.Name, db.ID)
		wk.graph.addEdge(DataSourceID(ds.ID), DatabaseID(db.ID), EdgeBelongsTo)
		if err := wk.queryDataSource(ctx, ds.ID, nil, ""); err != nil {
			return err
		}
	}
	return nil
}

// readNamedDataSource retrieves a data source the collect names and queries it
// with the collect's filter.
func (wk *walker) readNamedDataSource(ctx context.Context, q DataSourceQuery, filterParam string) error {
	if wk.stopped(ctx) {
		return nil
	}
	ds, err := wk.client.DataSource(ctx, q.ID)
	if err != nil {
		return wk.fail(failure{scope: "data source " + q.ID, named: true}, err)
	}
	wk.graph.addDataSource(ds.ID, ds.DisplayName(), firstNonEmpty(ds.DatabaseParent.DatabaseID, ds.Parent.DatabaseID))
	param := ""
	if q.Filter != nil {
		param = filterParam
	}
	return wk.queryDataSource(ctx, ds.ID, q.Filter, param)
}

// queryDataSource reads every entry of data source id that filter selects.
// It makes no request when the walk already queried id unfiltered, or with a
// filter whose JSON encoding is the same. Every query sorts by created_time
// ascending. When Notion caps a query, the next query selects created_time on
// or after the last entry read, and the walk is incomplete only when such a
// window cannot move past the cap.
func (wk *walker) queryDataSource(ctx context.Context, id string, filter map[string]any, filterParam string) error {
	read, err := sourceReadKey(id, filter)
	if err != nil {
		return fmt.Errorf("notion: %s: %w", filterParam, err)
	}
	unfiltered, _ := sourceReadKey(id, nil)
	if wk.sources[unfiltered] || wk.sources[read] {
		return nil
	}
	wk.sources[read] = true
	scope := "data source " + id
	windowStart := ""
	for !wk.stopped(ctx) {
		last, capped, err := wk.queryWindow(ctx, id, windowFilter(filter, windowStart), failure{scope: scope, filterParam: filterParam})
		if err != nil || !capped {
			return err
		}
		if last == "" || last == windowStart {
			wk.incomplete = append(wk.incomplete, fmt.Sprintf("%s: the query reached Notion's result limit "+
				"(query_result_limit_reached) and every remaining entry was created at %s, so no created_time window "+
				"gets past it", scope, windowStart))
			return nil
		}
		windowStart = last
	}
	return nil
}

// sourceReadKey is the key a query of data source id with filter is recorded
// under: the id and the filter's JSON encoding, "null" for no filter. It
// returns an error when the filter does not encode as JSON.
func sourceReadKey(id string, filter map[string]any) (string, error) {
	encoded, err := json.Marshal(filter)
	if err != nil {
		return "", fmt.Errorf("the filter does not encode as JSON: %w", err)
	}
	return id + " " + string(encoded), nil
}

// queryWindow reads every page of one query and returns the created_time of
// the last entry read and whether any page carried Notion's cap signal.
func (wk *walker) queryWindow(ctx context.Context, id string, filter map[string]any, f failure) (string, bool, error) {
	last, capped, cursor := "", false, ""
	for !wk.stopped(ctx) {
		body := map[string]any{"sorts": []any{map[string]any{"timestamp": "created_time", "direction": "ascending"}}}
		if filter != nil {
			body["filter"] = filter
		}
		if cursor != "" {
			body["start_cursor"] = cursor
		}
		page, err := wk.client.QueryDataSource(ctx, id, body)
		if err != nil {
			return "", false, wk.fail(f, err)
		}
		if page.RequestStatus.Type == queryCapped {
			capped = true
		}
		for _, row := range page.Results {
			last = row.CreatedTime
			wk.graph.addEdge(PageID(row.ID), DataSourceID(id), EdgeBelongsTo)
			if err := wk.readPageObject(ctx, row); err != nil {
				return "", false, err
			}
		}
		if cursor = page.Next; cursor == "" {
			break
		}
	}
	return last, capped, nil
}

// windowFilter combines the collect's filter with created_time on or after
// windowStart; an empty windowStart leaves the filter as it is.
func windowFilter(filter map[string]any, windowStart string) map[string]any {
	if windowStart == "" {
		return filter
	}
	window := map[string]any{"timestamp": "created_time", "created_time": map[string]any{"on_or_after": windowStart}}
	if filter == nil {
		return window
	}
	return map[string]any{"and": []any{filter, window}}
}

// readSearch reads every page of a title search: a page hit is read as a
// page, a data source hit is queried.
func (wk *walker) readSearch(ctx context.Context, query string) error {
	scope := fmt.Sprintf("title search %q", query)
	cursor := ""
	for !wk.stopped(ctx) {
		page, err := wk.client.Search(ctx, query, cursor)
		if err != nil {
			return wk.fail(failure{scope: scope}, err)
		}
		for _, hit := range page.Results {
			if err := wk.readHit(ctx, hit); err != nil {
				return err
			}
		}
		if cursor = page.Next; cursor == "" {
			return nil
		}
	}
	return nil
}

func (wk *walker) readHit(ctx context.Context, hit notionapi.SearchResult) error {
	switch hit.Object {
	case "page":
		return wk.readPageObject(ctx, hit.Page)
	case "data_source":
		ds := hit.Source
		wk.graph.addDataSource(ds.ID, ds.DisplayName(), firstNonEmpty(ds.DatabaseParent.DatabaseID, ds.Parent.DatabaseID))
		return wk.queryDataSource(ctx, ds.ID, nil, "")
	default:
		return nil
	}
}

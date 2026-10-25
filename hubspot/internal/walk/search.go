// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"context"
	"fmt"
	"maps"
)

// SearchResultCap is the most results HubSpot returns for one search query.
const SearchResultCap = 10000

// idSort is the sort every search request sends: hs_object_id ascending.
func idSort() []any {
	return []any{map[string]any{"propertyName": "hs_object_id", "direction": "ASCENDING"}}
}

// readSearch reads every hit of one search as a target. Every request sorts
// by hs_object_id ascending; when a query reaches SearchResultCap, the next
// query adds hs_object_id GT the last id read to every filter group.
func (wk *walker) readSearch(ctx context.Context, s Search, filterParam string) error {
	f := failure{scope: "search of " + s.ObjectType, objectType: s.ObjectType, filterParam: filterParam}
	groups := s.FilterGroups
	for !wk.stopped(ctx) {
		read, total, last, err := wk.searchQuery(ctx, s.ObjectType, groups, f)
		if err != nil {
			return err
		}
		if read < SearchResultCap || total <= read {
			if total > read {
				wk.markIncomplete("%s: HubSpot reported %d results and %d were read", f.scope, total, read)
			}
			return nil
		}
		next, reason := windowGroups(s.FilterGroups, last)
		if reason != "" {
			wk.markIncomplete("%s: %d results exceed HubSpot's %d per query and the hs_object_id window cannot be added: %s",
				f.scope, total, SearchResultCap, reason)
			return nil
		}
		groups = next
	}
	return nil
}

// searchQuery reads every page of one query, up to SearchResultCap results,
// and returns how many were read, the total HubSpot reported and the last id.
func (wk *walker) searchQuery(ctx context.Context, objectType string, groups []map[string]any, f failure) (int, int, string, error) {
	read, total, last, after := 0, 0, "", ""
	for read < SearchResultCap && !wk.stopped(ctx) {
		body := map[string]any{"filterGroups": groups, "sorts": idSort()}
		if props := wk.properties[objectType]; len(props) > 0 {
			body["properties"] = props
		}
		if after != "" {
			body["after"] = after
		}
		page, err := wk.client.Search(ctx, objectType, body)
		if err != nil {
			return read, total, last, wk.fail(f, err)
		}
		total = page.Total
		for _, r := range page.Results {
			read++
			last = string(r.ID)
			if wk.read[objectType] == nil {
				wk.read[objectType] = map[string]bool{}
			}
			wk.read[objectType][last] = true
			wk.graph.addRecord(objectType, r)
			wk.addTarget(objectType, last)
		}
		if after = page.Next; after == "" {
			break
		}
	}
	return read, total, last, nil
}

// windowGroups returns the filter groups with hs_object_id GT last added to
// each, or the reason HubSpot's filter caps do not admit it.
func windowGroups(groups []map[string]any, last string) ([]map[string]any, string) {
	total := 0
	for _, g := range groups {
		total += len(filtersOf(g)) + 1
	}
	if total > maxFiltersTotal {
		return nil, fmt.Sprintf("the windowed query would carry %d filters, over HubSpot's %d", total, maxFiltersTotal)
	}
	out := make([]map[string]any, 0, len(groups))
	for i, g := range groups {
		filters := filtersOf(g)
		if len(filters) >= maxFiltersPerGroup {
			return nil, fmt.Sprintf("filter group %d already carries HubSpot's %d filters", i, maxFiltersPerGroup)
		}
		copied := map[string]any{}
		maps.Copy(copied, g)
		copied["filters"] = append(append([]any{}, filters...),
			map[string]any{"propertyName": "hs_object_id", "operator": "GT", "value": last})
		out = append(out, copied)
	}
	return out, ""
}

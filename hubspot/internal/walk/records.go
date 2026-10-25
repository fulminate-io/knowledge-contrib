// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/hubapi"
)

// listObjectTypes maps a list's objectTypeId to the object type its members
// are.
var listObjectTypes = map[string]string{
	"0-1": "contacts", "0-2": "companies", "0-3": "deals", "0-5": "tickets",
	"0-48": "calls", "0-49": "emails", "0-47": "meetings", "0-46": "notes", "0-27": "tasks",
}

// addTarget makes a record one whose associations the walk reads.
func (wk *walker) addTarget(objectType, id string) {
	key := objectType + ":" + id
	if wk.isTarget[key] {
		return
	}
	wk.isTarget[key] = true
	wk.targets[objectType] = append(wk.targets[objectType], id)
}

// readRecords batch-reads the ids of objectType not attempted yet. named is true
// when the ids came from the collect's params, so an id HubSpot does not
// return fails the collect; asTarget makes every record read a target.
func (wk *walker) readRecords(ctx context.Context, objectType string, ids []string, named, asTarget bool) error {
	if wk.read[objectType] == nil {
		wk.read[objectType] = map[string]bool{}
	}
	var todo []string
	queued := map[string]bool{}
	for _, id := range ids {
		if !wk.read[objectType][id] && !queued[id] {
			queued[id] = true
			todo = append(todo, id)
		}
	}
	for chunk := range slices.Chunk(todo, hubapi.BatchReadLimit) {
		if wk.stopped(ctx) {
			return nil
		}
		f := failure{scope: objectType + " " + strings.Join(chunk, ", "), objectType: objectType, named: named}
		for _, id := range chunk {
			wk.read[objectType][id] = true
		}
		batch, err := wk.client.ReadRecords(ctx, objectType, chunk, wk.properties[objectType])
		if err != nil {
			if ferr := wk.fail(f, err); ferr != nil {
				return ferr
			}
			continue
		}
		returned := map[string]bool{}
		for _, r := range batch.Results {
			returned[string(r.ID)] = true
			wk.graph.addRecord(objectType, r)
			if asTarget {
				wk.addTarget(objectType, string(r.ID))
			}
		}
		var missing []string
		for _, id := range chunk {
			if !returned[id] {
				missing = append(missing, id)
			}
		}
		if len(missing) == 0 {
			continue
		}
		if named {
			return fmt.Errorf("hubspot: the batch read of %s did not return %s: they do not exist or the token cannot see them (%s)",
				objectType, strings.Join(missing, ", "), batchErrors(batch.Errors))
		}
		wk.markIncomplete("%s %s: the batch read did not return them (%s)", objectType, strings.Join(missing, ", "), batchErrors(batch.Errors))
	}
	return nil
}

// batchErrors renders a batch answer's errors for a reason.
func batchErrors(errs []hubapi.BatchError) string {
	if len(errs) == 0 {
		return "no error was named"
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, strings.TrimSpace(e.Category+" "+e.Message))
	}
	return strings.Join(parts, "; ")
}

// readList reads a named list's definition and every page of its
// memberships, then its members as targets.
func (wk *walker) readList(ctx context.Context, id string) error {
	if wk.stopped(ctx) {
		return nil
	}
	list, err := wk.client.ReadList(ctx, id)
	if err != nil {
		return wk.fail(failure{scope: "list " + id, objectType: "lists", named: true}, err)
	}
	wk.graph.addList(id, list)
	objectType, known := listObjectTypes[list.ObjectTypeID]
	if !known {
		wk.markIncomplete("list %s: its objectTypeId %q is not an object type this collector reads", id, list.ObjectTypeID)
		return nil
	}
	var members []string
	after := ""
	for !wk.stopped(ctx) {
		page, err := wk.client.ReadMemberships(ctx, id, after)
		if err != nil {
			if ferr := wk.fail(failure{scope: "memberships of list " + id, objectType: "lists"}, err); ferr != nil {
				return ferr
			}
			break
		}
		members = append(members, page.RecordIDs...)
		if after = page.Next; after == "" {
			break
		}
	}
	for _, m := range members {
		wk.graph.addEdge(hasMember(id, objectType, m))
	}
	return wk.readRecords(ctx, objectType, members, false, true)
}

// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/hubapi"
)

// readAssociations reads, for every target, its associations to every
// object type, then reads every associated record not read yet. Associated
// records are not targets, so their own associations are not read.
func (wk *walker) readAssociations(ctx context.Context) error {
	associated := map[string][]string{}
	for _, fromType := range objectTypes {
		targets := wk.targets[fromType]
		if len(targets) == 0 {
			continue
		}
		for _, toType := range objectTypes {
			for chunk := range slices.Chunk(targets, hubapi.AssociationBatchLimit) {
				ids, err := wk.associationsOf(ctx, fromType, toType, chunk)
				if err != nil {
					return err
				}
				associated[toType] = append(associated[toType], ids...)
			}
		}
	}
	for _, objectType := range objectTypes {
		if err := wk.readRecords(ctx, objectType, associated[objectType], false, false); err != nil {
			return err
		}
	}
	return nil
}

// associationsOf reads the toType associations of the fromType records in
// ids, following each input's paging until no result carries any, and
// returns the associated record ids.
func (wk *walker) associationsOf(ctx context.Context, fromType, toType string, ids []string) ([]string, error) {
	inputs := make([]hubapi.AssociationInput, 0, len(ids))
	for _, id := range ids {
		inputs = append(inputs, hubapi.AssociationInput{ID: id})
	}
	var found []string
	for len(inputs) > 0 && !wk.stopped(ctx) {
		f := failure{scope: fmt.Sprintf("%s associations of %s", toType, fromType), objectType: fromType}
		read, err := wk.client.ReadAssociations(ctx, fromType, toType, inputs)
		if err != nil {
			return found, wk.fail(f, err)
		}
		if len(read.Errors) > 0 {
			wk.markIncomplete("%s: HubSpot answered %d with errors for some inputs (%s)", f.scope, read.Status, batchInputs(read.Errors))
		}
		var next []hubapi.AssociationInput
		for _, row := range read.Results {
			from := string(row.From.ID)
			for _, to := range row.To {
				if err := wk.graph.addAssociation(fromType, from, toType, string(to.ToObjectID), to.AssociationTypes); err != nil {
					return found, err
				}
				found = append(found, string(to.ToObjectID))
			}
			if after := row.NextAfter(); after != "" {
				next = append(next, hubapi.AssociationInput{ID: from, After: after})
			}
		}
		inputs = next
	}
	return found, nil
}

// batchInputs renders a batch answer's errors with the inputs each names.
func batchInputs(errs []hubapi.BatchError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		var ids []string
		for _, values := range e.Context {
			ids = append(ids, values...)
		}
		slices.Sort(ids)
		parts = append(parts, strings.TrimSpace(fmt.Sprintf("%s %s %s", e.Category, strings.Join(ids, ","), e.Message)))
	}
	return strings.Join(parts, "; ")
}

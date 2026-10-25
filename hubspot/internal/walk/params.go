// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Params is what a collect names. Every field carries omitempty, so the
// inferred schema requires none of them; validate refuses a collect that names
// no record, list or search.
type Params struct {
	// Companies, Contacts, Deals and Tickets are record ids; each record is
	// read with the records associated with it.
	Companies []string `json:"companies,omitempty"`
	Contacts  []string `json:"contacts,omitempty"`
	Deals     []string `json:"deals,omitempty"`
	Tickets   []string `json:"tickets,omitempty"`
	// Lists are list ids; each member is read with its associated records.
	Lists []string `json:"lists,omitempty"`
	// Searches are CRM searches; each hit is read with its associated records.
	Searches []Search `json:"searches,omitempty"`
	// Properties names extra properties to read per object type, such as
	// {"contacts": ["jobtitle"]}. Absent means HubSpot's default properties.
	Properties map[string][]string `json:"properties,omitempty"`
}

// Search is one CRM search: an object type and HubSpot filterGroups, sent as
// written.
type Search struct {
	ObjectType   string           `json:"object_type"`
	FilterGroups []map[string]any `json:"filter_groups"`
}

// The search limits HubSpot documents.
const (
	maxFilterGroups     = 5
	maxFiltersPerGroup  = 6
	maxFiltersTotal     = 18
	maxSearchBodyLength = 3000
)

// searchableTypes are the object types a search may name.
var searchableTypes = []string{"companies", "contacts", "deals", "tickets"}

// recordIDPattern is a HubSpot record or list id: decimal digits.
var recordIDPattern = regexp.MustCompile(`^[0-9]+$`)

// validate refuses a collect that names no target, a malformed id, a search
// over HubSpot's documented caps or one with no filter, and a properties key
// that is not an object type, naming the param path.
func (p Params) validate() error {
	if len(p.Companies)+len(p.Contacts)+len(p.Deals)+len(p.Tickets)+len(p.Lists)+len(p.Searches) == 0 {
		return fmt.Errorf("hubspot: the collect names no target; set at least one of params.companies, params.contacts, " +
			"params.deals, params.tickets, params.lists or params.searches ({object_type, filter_groups})")
	}
	for _, named := range []struct {
		name string
		ids  []string
	}{{"companies", p.Companies}, {"contacts", p.Contacts}, {"deals", p.Deals}, {"tickets", p.Tickets}, {"lists", p.Lists}} {
		for i, id := range named.ids {
			if !recordIDPattern.MatchString(id) {
				return fmt.Errorf("hubspot: params.%s[%d] %q is not a HubSpot id (decimal digits)", named.name, i, id)
			}
		}
	}
	for i, s := range p.Searches {
		if err := s.validate(fmt.Sprintf("params.searches[%d]", i)); err != nil {
			return err
		}
	}
	for objectType := range p.Properties {
		if !slices.Contains(objectTypes, objectType) {
			return fmt.Errorf("hubspot: params.properties names %q, which is not one of %s", objectType, strings.Join(objectTypes, ", "))
		}
	}
	return nil
}

func (s Search) validate(path string) error {
	if !slices.Contains(searchableTypes, s.ObjectType) {
		return fmt.Errorf("hubspot: %s.object_type %q is not one of %s", path, s.ObjectType, strings.Join(searchableTypes, ", "))
	}
	if len(s.FilterGroups) == 0 {
		return fmt.Errorf("hubspot: %s.filter_groups is empty; a search with no filter selects every %s", path, s.ObjectType)
	}
	if len(s.FilterGroups) > maxFilterGroups {
		return fmt.Errorf("hubspot: %s.filter_groups has %d groups; HubSpot takes at most %d", path, len(s.FilterGroups), maxFilterGroups)
	}
	total := 0
	for i, g := range s.FilterGroups {
		n := len(filtersOf(g))
		if n > maxFiltersPerGroup {
			return fmt.Errorf("hubspot: %s.filter_groups[%d] has %d filters; HubSpot takes at most %d per group", path, i, n, maxFiltersPerGroup)
		}
		total += n
	}
	if total > maxFiltersTotal {
		return fmt.Errorf("hubspot: %s.filter_groups has %d filters in total; HubSpot takes at most %d", path, total, maxFiltersTotal)
	}
	raw, err := json.Marshal(map[string]any{"filterGroups": s.FilterGroups})
	if err != nil {
		return fmt.Errorf("hubspot: %s.filter_groups does not encode: %w", path, err)
	}
	if len(raw) > maxSearchBodyLength {
		return fmt.Errorf("hubspot: %s.filter_groups encodes to %d characters; HubSpot takes a query of at most %d",
			path, len(raw), maxSearchBodyLength)
	}
	return nil
}

// filtersOf is a filter group's filters.
func filtersOf(group map[string]any) []any {
	filters, _ := group["filters"].([]any)
	return filters
}

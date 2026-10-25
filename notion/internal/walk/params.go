// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"fmt"
	"regexp"
	"strings"
)

// Params is what a collect names. Every field carries omitempty, so the
// inferred schema requires none of them; validate refuses a collect that names
// no page, database, data source or title search.
type Params struct {
	// Pages are page ids; each page is read with every block and comment.
	Pages []string `json:"pages,omitempty"`
	// Descendants follows child pages and child databases of every page read.
	Descendants bool `json:"descendants,omitempty"`
	// Databases are database ids; every entry of each of their data sources
	// is read.
	Databases []string `json:"databases,omitempty"`
	// DataSources are data source ids, each with an optional Notion filter
	// object sent verbatim in its query.
	DataSources []DataSourceQuery `json:"data_sources,omitempty"`
	// TitleSearch are search queries; each matching page is read and each
	// matching data source queried.
	TitleSearch []string `json:"title_search,omitempty"`
}

// DataSourceQuery names one data source and the filter its query sends.
type DataSourceQuery struct {
	ID     string         `json:"id"`
	Filter map[string]any `json:"filter,omitempty"`
}

// notionIDPattern is a Notion id: 32 hexadecimal digits, with or without the
// UUID dashes.
var notionIDPattern = regexp.MustCompile(`^(?i:[0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

// validate refuses a collect that names no target, an empty or malformed id,
// or an empty search query, naming the param path.
func (p Params) validate() error {
	if len(p.Pages) == 0 && len(p.Databases) == 0 && len(p.DataSources) == 0 && len(p.TitleSearch) == 0 {
		return fmt.Errorf("notion: the collect names no target; set at least one of params.pages, " +
			"params.databases, params.data_sources ({id, filter}) or params.title_search")
	}
	for i, id := range p.Pages {
		if err := checkID(fmt.Sprintf("params.pages[%d]", i), id); err != nil {
			return err
		}
	}
	for i, id := range p.Databases {
		if err := checkID(fmt.Sprintf("params.databases[%d]", i), id); err != nil {
			return err
		}
	}
	for i, ds := range p.DataSources {
		if err := checkID(fmt.Sprintf("params.data_sources[%d].id", i), ds.ID); err != nil {
			return err
		}
	}
	for i, q := range p.TitleSearch {
		if strings.TrimSpace(q) == "" {
			return fmt.Errorf("notion: params.title_search[%d] is empty; an empty search selects everything the token can see", i)
		}
	}
	return nil
}

func checkID(path, id string) error {
	if !notionIDPattern.MatchString(id) {
		return fmt.Errorf("notion: %s %q is not a Notion id (32 hexadecimal digits, with or without dashes)", path, id)
	}
	return nil
}

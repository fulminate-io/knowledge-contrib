// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"fmt"
	"os"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// TokenVariable is the environment variable the Notion token is read from.
const TokenVariable = "NOTION_API_KEY"

// The node types this collector emits.
const (
	NodePage       = "notion-page"
	NodeDatabase   = "notion-database"
	NodeDataSource = "notion-data-source"
	NodeComment    = "notion-comment"
)

// The edge types this collector emits.
const (
	// EdgeChildOf joins a child page or child database to the page it sits in.
	EdgeChildOf = "CHILD_OF"
	// EdgeBelongsTo joins a data source to its database and a database entry
	// to its data source.
	EdgeBelongsTo = "BELONGS_TO"
	// EdgeCommentsOn joins a comment to the page it, or its block, sits in.
	EdgeCommentsOn = "COMMENTS_ON"
)

// NodeTypes is every node type the walk emits, sorted.
func NodeTypes() []string { return []string{NodeComment, NodeDataSource, NodeDatabase, NodePage} }

// EdgeTypes is every edge type the walk emits, sorted.
func EdgeTypes() []string { return []string{EdgeBelongsTo, EdgeChildOf, EdgeCommentsOn} }

// Describe declares the collector: its suggested behavior, its vocabulary and
// the one secret it reads.
func (c Collector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable:    new(true),
			Embeddable:      new(true),
			Syncable:        new(true),
			EmbedFields:     []string{"symbol_name", "content"},
			SummarizeFields: []string{"symbol_name", "content"},
			Bm25Fields:      []string{"symbol_name", "content"},
		},
		NodeTypes: NodeTypes(),
		EdgeTypes: EdgeTypes(),
		Environment: []framework.EnvDeclaration{{
			Name:        TokenVariable,
			Class:       framework.EnvClassSecret,
			Description: "a Notion personal access token or internal connection token",
		}},
	}
}

// token reads NOTION_API_KEY. Absent and present-but-empty are both refused,
// naming the variable and the reference form an entry carries.
func token() (string, error) {
	value := os.Getenv(TokenVariable)
	if value == "" {
		return "", fmt.Errorf(
			"notion: %s is not set. It carries the Notion token this collector reads with; the config entry's env "+
				"block names it as the reference %q and the collector's credential file holds the value",
			TokenVariable, "${"+TokenVariable+"}")
	}
	return value, nil
}

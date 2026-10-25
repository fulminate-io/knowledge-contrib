// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"slices"
	"strconv"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/notionapi"
)

// PageID is the node id of a Notion page.
func PageID(id string) string { return "notion:page:" + id }

// DatabaseID is the node id of a Notion database.
func DatabaseID(id string) string { return "notion:database:" + id }

// DataSourceID is the node id of a Notion data source.
func DataSourceID(id string) string { return "notion:data-source:" + id }

// CommentID is the node id of a Notion comment.
func CommentID(id string) string { return "notion:comment:" + id }

// graph accumulates the nodes and edges of one walk, keyed by id.
type graph struct {
	nodes map[string]framework.Node
	edges map[string]framework.Edge
}

func newGraph() *graph {
	return &graph{nodes: map[string]framework.Node{}, edges: map[string]framework.Edge{}}
}

func (g *graph) addEdge(from, to, kind string) {
	g.edges[from+"\x00"+kind+"\x00"+to] = framework.Edge{FromID: from, ToID: to, Type: kind}
}

// addPage emits a page node carrying its title, its block text as content,
// its properties and the block types no call returns content for.
func (g *graph) addPage(p notionapi.Page, content string, unsupported []string) {
	meta := map[string]string{"page_id": p.ID, "in_trash": strconv.FormatBool(p.InTrash)}
	setIf(meta, "url", p.URL)
	setIf(meta, "created_time", p.CreatedTime)
	setIf(meta, "last_edited_time", p.LastEditedTime)
	setIf(meta, "created_by", p.CreatedBy.ID)
	setIf(meta, "parent_type", p.Parent.Type)
	setIf(meta, "parent_id", p.Parent.ID())
	if len(unsupported) > 0 {
		meta["unsupported_blocks"] = strings.Join(unsupported, ",")
	}
	title := ""
	for name, raw := range p.Properties {
		kind, value := renderProperty(raw)
		if kind == "title" {
			title = value
		}
		setIf(meta, "property:"+name, value)
	}
	g.nodes[PageID(p.ID)] = framework.Node{ID: PageID(p.ID), Type: NodePage, SymbolName: title, Content: content, Metadata: meta}
}

// addDatabase emits a database node.
func (g *graph) addDatabase(d notionapi.Database) {
	meta := map[string]string{"database_id": d.ID}
	setIf(meta, "url", d.URL)
	setIf(meta, "created_time", d.CreatedTime)
	setIf(meta, "parent_type", d.Parent.Type)
	setIf(meta, "parent_id", d.Parent.ID())
	title := notionapi.PlainText(d.Title)
	g.nodes[DatabaseID(d.ID)] = framework.Node{ID: DatabaseID(d.ID), Type: NodeDatabase, SymbolName: title, Metadata: meta}
}

// addDataSource emits a data source node. databaseID is the database it
// belongs to, or "" when unknown.
func (g *graph) addDataSource(id, name, databaseID string) {
	if _, ok := g.nodes[DataSourceID(id)]; ok && name == "" {
		return
	}
	meta := map[string]string{"data_source_id": id}
	setIf(meta, "database_id", databaseID)
	g.nodes[DataSourceID(id)] = framework.Node{ID: DataSourceID(id), Type: NodeDataSource, SymbolName: name, Metadata: meta}
}

// addComment emits a comment on page, and the block id when the comment sits
// on a block of it.
func (g *graph) addComment(page string, c notionapi.Comment) {
	meta := map[string]string{"comment_id": c.ID, "page_id": page}
	setIf(meta, "discussion_id", c.DiscussionID)
	setIf(meta, "created_time", c.CreatedTime)
	setIf(meta, "created_by", c.CreatedBy.ID)
	if c.Parent.Type == "block_id" {
		meta["block_id"] = c.Parent.BlockID
	}
	g.nodes[CommentID(c.ID)] = framework.Node{
		ID: CommentID(c.ID), Type: NodeComment, Content: notionapi.PlainText(c.RichText), Metadata: meta,
	}
	g.addEdge(CommentID(c.ID), PageID(page), EdgeCommentsOn)
}

// result returns the nodes sorted by id and the edges sorted by from, type, to.
func (g *graph) result() ([]framework.Node, []framework.Edge) {
	nodeIDs := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		nodeIDs = append(nodeIDs, id)
	}
	slices.Sort(nodeIDs)
	nodes := make([]framework.Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		nodes = append(nodes, g.nodes[id])
	}
	keys := make([]string, 0, len(g.edges))
	for k := range g.edges {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	edges := make([]framework.Edge, 0, len(keys))
	for _, k := range keys {
		edges = append(edges, g.edges[k])
	}
	return nodes, edges
}

func setIf(meta map[string]string, key, value string) {
	if value != "" {
		meta[key] = value
	}
}

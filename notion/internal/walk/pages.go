// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"context"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/notionapi"
)

// pageText accumulates what one page's block tree yields, in document order.
type pageText struct {
	lines       []string
	unsupported []string
	// blocks are the ids of every block read, each of which may carry comments.
	blocks []string
	// childPages and childDatabases are the child_page and child_database
	// blocks met, read when the walk follows descendants.
	childPages     []string
	childDatabases []string
}

// readPage retrieves page id and reads it. named is true when the id came from
// the collect's params.
func (wk *walker) readPage(ctx context.Context, id string, named bool) error {
	if wk.stopped(ctx) {
		return nil
	}
	page, err := wk.client.Page(ctx, id)
	if err != nil {
		return wk.fail(failure{scope: "page " + id, named: named}, err)
	}
	return wk.readPageObject(ctx, page)
}

// readPageObject reads a page whose object is already in hand: its block tree,
// its comments and, when the walk follows descendants, its child pages and
// databases. A page already read is not read again.
func (wk *walker) readPageObject(ctx context.Context, page notionapi.Page) error {
	if wk.pages[page.ID] || wk.stopped(ctx) {
		return nil
	}
	wk.pages[page.ID] = true
	var text pageText
	if err := wk.readBlocks(ctx, page.ID, &text); err != nil {
		return err
	}
	wk.graph.addPage(page, strings.Join(text.lines, "\n"), text.unsupported)
	for _, target := range append([]string{page.ID}, text.blocks...) {
		if err := wk.readComments(ctx, page.ID, target); err != nil {
			return err
		}
	}
	if !wk.descendants {
		return nil
	}
	for _, child := range text.childPages {
		wk.graph.addEdge(PageID(child), PageID(page.ID), EdgeChildOf)
		if err := wk.readPage(ctx, child, false); err != nil {
			return err
		}
	}
	for _, child := range text.childDatabases {
		wk.graph.addEdge(DatabaseID(child), PageID(page.ID), EdgeChildOf)
		if err := wk.readDatabase(ctx, child, false); err != nil {
			return err
		}
	}
	return nil
}

// readBlocks reads every page of block id's children and, depth first, the
// children of every child that has any, appending what it reads to text.
func (wk *walker) readBlocks(ctx context.Context, id string, text *pageText) error {
	cursor := ""
	for !wk.stopped(ctx) {
		page, err := wk.client.BlockChildren(ctx, id, cursor)
		if err != nil {
			return wk.fail(failure{scope: "block children of " + id}, err)
		}
		for _, b := range page.Results {
			if err := wk.readBlock(ctx, b, text); err != nil {
				return err
			}
		}
		if cursor = page.Next; cursor == "" {
			return nil
		}
	}
	return nil
}

func (wk *walker) readBlock(ctx context.Context, b notionapi.Block, text *pageText) error {
	switch b.Type {
	case "child_page":
		text.childPages = append(text.childPages, b.ID)
		appendLine(text, b.Content.Title)
		return nil
	case "child_database":
		text.childDatabases = append(text.childDatabases, b.ID)
		appendLine(text, b.Content.Title)
		return nil
	case "unsupported":
		text.unsupported = append(text.unsupported, firstNonEmpty(b.Content.BlockType, "unknown"))
		text.blocks = append(text.blocks, b.ID)
		return nil
	}
	text.blocks = append(text.blocks, b.ID)
	appendLine(text, blockText(b))
	if b.HasChildren {
		return wk.readBlocks(ctx, b.ID, text)
	}
	return nil
}

// blockText is the text one block carries. A Notion-hosted file's signed URL
// is never part of it; an external file's URL is.
func blockText(b notionapi.Block) string {
	c := b.Content
	switch b.Type {
	case "equation":
		return c.Expression
	case "bookmark", "embed", "link_preview":
		return c.URL
	case "image", "file", "pdf", "video", "audio":
		caption := notionapi.PlainText(c.Caption)
		if c.Type == "external" && c.External.URL != "" {
			return strings.TrimSpace(caption + " " + c.External.URL)
		}
		return caption
	case "table_row":
		cells := make([]string, 0, len(c.Cells))
		for _, cell := range c.Cells {
			cells = append(cells, notionapi.PlainText(cell))
		}
		return strings.Join(cells, " | ")
	default:
		return notionapi.PlainText(c.RichText)
	}
}

func appendLine(text *pageText, line string) {
	if line != "" {
		text.lines = append(text.lines, line)
	}
}

// readComments reads every page of the unresolved comments on target, a page
// or one of its blocks, as comments of page.
func (wk *walker) readComments(ctx context.Context, page, target string) error {
	cursor := ""
	for !wk.stopped(ctx) {
		list, err := wk.client.Comments(ctx, target, cursor)
		if err != nil {
			return wk.fail(failure{scope: "comments on " + target}, err)
		}
		for _, c := range list.Results {
			wk.graph.addComment(page, c)
		}
		if cursor = list.Next; cursor == "" {
			return nil
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// SPDX-License-Identifier: Apache-2.0

package notionapi

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// RichText is one rich text object; this client reads its plain text.
type RichText struct {
	PlainText string `json:"plain_text"`
}

// PlainText concatenates the plain text of every rich text object.
func PlainText(rt []RichText) string {
	var b strings.Builder
	for _, r := range rt {
		b.WriteString(r.PlainText)
	}
	return b.String()
}

// Parent is the parent object of a page, database, data source, block or
// comment.
type Parent struct {
	Type         string `json:"type"`
	PageID       string `json:"page_id"`
	DatabaseID   string `json:"database_id"`
	DataSourceID string `json:"data_source_id"`
	BlockID      string `json:"block_id"`
}

// ID is the id the parent's type names.
func (p Parent) ID() string {
	switch p.Type {
	case "page_id":
		return p.PageID
	case "database_id":
		return p.DatabaseID
	case "data_source_id":
		return p.DataSourceID
	case "block_id":
		return p.BlockID
	default:
		return ""
	}
}

// UserRef is a partial user object.
type UserRef struct {
	ID string `json:"id"`
}

// Page is a page object.
type Page struct {
	Object         string                     `json:"object"`
	ID             string                     `json:"id"`
	CreatedTime    string                     `json:"created_time"`
	LastEditedTime string                     `json:"last_edited_time"`
	CreatedBy      UserRef                    `json:"created_by"`
	Parent         Parent                     `json:"parent"`
	InTrash        bool                       `json:"in_trash"`
	URL            string                     `json:"url"`
	Properties     map[string]json.RawMessage `json:"properties"`
}

// Block is a block object. Content is the object under the block's own type
// key.
type Block struct {
	ID          string
	Type        string
	HasChildren bool
	Content     BlockContent
}

// BlockContent is the subset of a block's type-keyed object this client reads.
type BlockContent struct {
	RichText   []RichText           `json:"rich_text"`
	Title      string               `json:"title"`
	Expression string               `json:"expression"`
	URL        string               `json:"url"`
	Caption    []RichText           `json:"caption"`
	Type       string               `json:"type"`
	External   struct{ URL string } `json:"external"`
	Cells      [][]RichText         `json:"cells"`
	Checked    bool                 `json:"checked"`
	BlockType  string               `json:"block_type"`
}

// UnmarshalJSON decodes a block and the object under its type key.
func (b *Block) UnmarshalJSON(raw []byte) error {
	var head struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		HasChildren bool   `json:"has_children"`
		BlockType   string `json:"block_type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return err
	}
	b.ID, b.Type, b.HasChildren = head.ID, head.Type, head.HasChildren
	b.Content = BlockContent{}
	if content, ok := all[head.Type]; ok && len(content) > 0 && content[0] == '{' {
		if err := json.Unmarshal(content, &b.Content); err != nil {
			return err
		}
	}
	if b.Content.BlockType == "" {
		b.Content.BlockType = head.BlockType
	}
	return nil
}

// list is the envelope every paginated answer carries.
type list struct {
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

// next is the cursor of the next page, or "" when the list has ended.
func (l list) next() string {
	if !l.HasMore {
		return ""
	}
	return l.NextCursor
}

// BlockPage is one page of a block's children.
type BlockPage struct {
	Results []Block
	Next    string
}

// BlockChildren reads one page of the children of block id. cursor is empty
// on the first page.
func (c *Client) BlockChildren(ctx context.Context, id, cursor string) (BlockPage, error) {
	q := url.Values{"page_size": {strconv.Itoa(PageSize)}}
	if cursor != "" {
		q.Set("start_cursor", cursor)
	}
	var out struct {
		list
		Results []Block `json:"results"`
	}
	if err := c.get(ctx, "/blocks/"+url.PathEscape(id)+"/children", q, &out); err != nil {
		return BlockPage{}, err
	}
	return BlockPage{Results: out.Results, Next: out.next()}, nil
}

// Page reads one page object.
func (c *Client) Page(ctx context.Context, id string) (Page, error) {
	var out Page
	err := c.get(ctx, "/pages/"+url.PathEscape(id), nil, &out)
	return out, err
}

// Database is a database object.
type Database struct {
	ID          string     `json:"id"`
	Title       []RichText `json:"title"`
	Parent      Parent     `json:"parent"`
	URL         string     `json:"url"`
	CreatedTime string     `json:"created_time"`
	DataSources []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"data_sources"`
}

// Database reads one database object.
func (c *Client) Database(ctx context.Context, id string) (Database, error) {
	var out Database
	err := c.get(ctx, "/databases/"+url.PathEscape(id), nil, &out)
	return out, err
}

// DataSource is a data source object.
type DataSource struct {
	Object         string     `json:"object"`
	ID             string     `json:"id"`
	Title          []RichText `json:"title"`
	Name           string     `json:"name"`
	Parent         Parent     `json:"parent"`
	DatabaseParent Parent     `json:"database_parent"`
	URL            string     `json:"url"`
	CreatedTime    string     `json:"created_time"`
}

// DisplayName is the data source's title, or its name when it has no title.
func (d DataSource) DisplayName() string {
	if t := PlainText(d.Title); t != "" {
		return t
	}
	return d.Name
}

// DataSource reads one data source object.
func (c *Client) DataSource(ctx context.Context, id string) (DataSource, error) {
	var out DataSource
	err := c.get(ctx, "/data_sources/"+url.PathEscape(id), nil, &out)
	return out, err
}

// RequestStatus is the request_status a data source query answer carries.
type RequestStatus struct {
	Type             string `json:"type"`
	IncompleteReason string `json:"incomplete_reason"`
}

// QueryPage is one page of a data source query.
type QueryPage struct {
	Results       []Page
	Next          string
	RequestStatus RequestStatus
}

// QueryDataSource posts body to the query endpoint of data source id. The
// caller sets filter, sorts and start_cursor in body; page_size is set here.
func (c *Client) QueryDataSource(ctx context.Context, id string, body map[string]any) (QueryPage, error) {
	body["page_size"] = PageSize
	var out struct {
		list
		Results       []Page        `json:"results"`
		RequestStatus RequestStatus `json:"request_status"`
	}
	if err := c.post(ctx, "/data_sources/"+url.PathEscape(id)+"/query", body, &out); err != nil {
		return QueryPage{}, err
	}
	return QueryPage{Results: out.Results, Next: out.next(), RequestStatus: out.RequestStatus}, nil
}

// SearchResult is one search hit: a page or a data source, by Object.
type SearchResult struct {
	Object string
	Page   Page
	Source DataSource
}

// UnmarshalJSON decodes a hit by its object field.
func (s *SearchResult) UnmarshalJSON(raw []byte) error {
	var head struct {
		Object string `json:"object"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return err
	}
	s.Object = head.Object
	switch head.Object {
	case "page":
		return json.Unmarshal(raw, &s.Page)
	case "data_source":
		return json.Unmarshal(raw, &s.Source)
	default:
		return nil
	}
}

// SearchPage is one page of a title search.
type SearchPage struct {
	Results []SearchResult
	Next    string
}

// Search posts one page of a title search for query.
func (c *Client) Search(ctx context.Context, query, cursor string) (SearchPage, error) {
	body := map[string]any{"query": query, "page_size": PageSize}
	if cursor != "" {
		body["start_cursor"] = cursor
	}
	var out struct {
		list
		Results []SearchResult `json:"results"`
	}
	if err := c.post(ctx, "/search", body, &out); err != nil {
		return SearchPage{}, err
	}
	return SearchPage{Results: out.Results, Next: out.next()}, nil
}

// Comment is a comment object.
type Comment struct {
	ID           string     `json:"id"`
	Parent       Parent     `json:"parent"`
	DiscussionID string     `json:"discussion_id"`
	CreatedTime  string     `json:"created_time"`
	CreatedBy    UserRef    `json:"created_by"`
	RichText     []RichText `json:"rich_text"`
}

// CommentPage is one page of a block's or page's unresolved comments.
type CommentPage struct {
	Results []Comment
	Next    string
}

// Comments reads one page of the unresolved comments on block or page id.
func (c *Client) Comments(ctx context.Context, id, cursor string) (CommentPage, error) {
	q := url.Values{"block_id": {id}, "page_size": {strconv.Itoa(PageSize)}}
	if cursor != "" {
		q.Set("start_cursor", cursor)
	}
	var out struct {
		list
		Results []Comment `json:"results"`
	}
	if err := c.get(ctx, "/comments", q, &out); err != nil {
		return CommentPage{}, err
	}
	return CommentPage{Results: out.Results, Next: out.next()}, nil
}

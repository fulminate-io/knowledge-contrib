// SPDX-License-Identifier: Apache-2.0

package slackapi

import (
	"context"
	"net/url"
	"strconv"
)

// PageLimit is the limit sent on conversations.history and
// conversations.replies: the largest value both method pages admit (history
// "Maximum of 999", replies "under 1000").
const PageLimit = 999

// SearchCount is the count sent on search.messages, the method's documented
// maximum.
const SearchCount = 100

// Message is the subset of a Slack message object this collector reads.
type Message struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	TS         string `json:"ts"`
	ThreadTS   string `json:"thread_ts"`
	User       string `json:"user"`
	BotID      string `json:"bot_id"`
	Username   string `json:"username"`
	Text       string `json:"text"`
	ReplyCount int    `json:"reply_count"`
	Permalink  string `json:"permalink"`
}

// SearchMatch is one search.messages match: a message and the channel it was
// posted in.
type SearchMatch struct {
	Message
	Channel MatchChannel `json:"channel"`
}

// MatchChannel is the channel a search.messages match names.
type MatchChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ResponseMetadata carries the cursor of the next page; empty means none.
type ResponseMetadata struct {
	NextCursor string `json:"next_cursor"`
}

// HistoryPage is one conversations.history answer.
type HistoryPage struct {
	Messages         []Message        `json:"messages"`
	HasMore          bool             `json:"has_more"`
	IsLimited        bool             `json:"is_limited"`
	ResponseMetadata ResponseMetadata `json:"response_metadata"`
}

// RepliesPage is one conversations.replies answer.
type RepliesPage struct {
	Messages         []Message        `json:"messages"`
	HasMore          bool             `json:"has_more"`
	ResponseMetadata ResponseMetadata `json:"response_metadata"`
}

// Window is a pair of Slack timestamps; an empty bound is not sent.
type Window struct {
	Oldest string
	Latest string
}

func (w Window) apply(args url.Values) {
	if w.Oldest != "" {
		args.Set("oldest", w.Oldest)
	}
	if w.Latest != "" {
		args.Set("latest", w.Latest)
	}
	if w.Oldest != "" || w.Latest != "" {
		args.Set("inclusive", "true")
	}
}

// History reads one page of conversations.history. cursor is empty on the
// first page.
func (c *Client) History(ctx context.Context, channel, cursor string, w Window) (HistoryPage, error) {
	args := url.Values{"channel": {channel}, "limit": {strconv.Itoa(PageLimit)}}
	if cursor != "" {
		args.Set("cursor", cursor)
	}
	w.apply(args)
	var page HistoryPage
	err := c.Call(ctx, "conversations.history", args, &page)
	return page, err
}

// Replies reads one page of conversations.replies for the thread whose parent
// is ts.
func (c *Client) Replies(ctx context.Context, channel, ts, cursor string, w Window) (RepliesPage, error) {
	args := url.Values{"channel": {channel}, "ts": {ts}, "limit": {strconv.Itoa(PageLimit)}}
	if cursor != "" {
		args.Set("cursor", cursor)
	}
	w.apply(args)
	var page RepliesPage
	err := c.Call(ctx, "conversations.replies", args, &page)
	return page, err
}

// Channel is the subset of a conversation object this collector reads.
type Channel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsChannel  bool   `json:"is_channel"`
	IsGroup    bool   `json:"is_group"`
	IsIM       bool   `json:"is_im"`
	IsMPIM     bool   `json:"is_mpim"`
	IsPrivate  bool   `json:"is_private"`
	IsArchived bool   `json:"is_archived"`
	Created    int64  `json:"created"`
	Topic      struct {
		Value string `json:"value"`
	} `json:"topic"`
	Purpose struct {
		Value string `json:"value"`
	} `json:"purpose"`
}

// ConversationInfo reads conversations.info for one channel.
func (c *Client) ConversationInfo(ctx context.Context, channel string) (Channel, error) {
	var out struct {
		Channel Channel `json:"channel"`
	}
	err := c.Call(ctx, "conversations.info", url.Values{"channel": {channel}}, &out)
	return out.Channel, err
}

// User is the subset of a user object this collector reads.
type User struct {
	ID       string `json:"id"`
	TeamID   string `json:"team_id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	Deleted  bool   `json:"deleted"`
	IsBot    bool   `json:"is_bot"`
	TZ       string `json:"tz"`
	Profile  struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
		Title       string `json:"title"`
	} `json:"profile"`
}

// UserInfo reads users.info for one user.
func (c *Client) UserInfo(ctx context.Context, user string) (User, error) {
	var out struct {
		User User `json:"user"`
	}
	err := c.Call(ctx, "users.info", url.Values{"user": {user}}, &out)
	return out.User, err
}

// SearchPage is one search.messages answer.
type SearchPage struct {
	Messages struct {
		Total   int           `json:"total"`
		Matches []SearchMatch `json:"matches"`
		Paging  struct {
			Count int `json:"count"`
			Total int `json:"total"`
			Page  int `json:"page"`
			Pages int `json:"pages"`
		} `json:"paging"`
	} `json:"messages"`
	ResponseMetadata ResponseMetadata `json:"response_metadata"`
}

// SearchFirstCursor is the cursor search.messages takes on its first page.
const SearchFirstCursor = "*"

// Search reads one page of search.messages for query.
func (c *Client) Search(ctx context.Context, query, cursor string) (SearchPage, error) {
	args := url.Values{"query": {query}, "count": {strconv.Itoa(SearchCount)}, "cursor": {cursor}}
	var page SearchPage
	err := c.Call(ctx, "search.messages", args, &page)
	return page, err
}

// SPDX-License-Identifier: Apache-2.0

package hubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// APIVersion is the date version every path names.
const APIVersion = "2026-09"

// BatchReadLimit is how many ids one batch read takes.
const BatchReadLimit = 100

// AssociationBatchLimit is how many inputs one associations batch read takes.
const AssociationBatchLimit = 1000

// SearchPageSize is the limit every search request sends, HubSpot's
// documented maximum.
const SearchPageSize = 200

// MembershipPageSize is the limit every list membership request sends:
// HubSpot's documented default limit for GET
// /crm/lists/2026-09/{listId}/memberships. Its reference documents no maximum.
const MembershipPageSize = 100

// ID is a record, list or association id, which HubSpot sends as a JSON
// string or a JSON number.
type ID string

// UnmarshalJSON accepts a JSON string or number.
func (i *ID) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		*i = ID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return err
	}
	*i = ID(n.String())
	return nil
}

// Record is a CRM record.
type Record struct {
	ID         ID                 `json:"id"`
	Properties map[string]*string `json:"properties"`
	CreatedAt  string             `json:"createdAt"`
	UpdatedAt  string             `json:"updatedAt"`
	Archived   bool               `json:"archived"`
}

// BatchError is one entry of a batch answer's errors.
type BatchError struct {
	Status   string              `json:"status"`
	Category string              `json:"category"`
	Message  string              `json:"message"`
	Context  map[string][]string `json:"context"`
}

// BatchRead is a batch read answer; Status is 200, or 207 when Errors names
// inputs that did not read.
type BatchRead struct {
	Status  int          `json:"-"`
	Results []Record     `json:"results"`
	Errors  []BatchError `json:"errors"`
}

// ReadRecords batch-reads up to BatchReadLimit records of objectType.
// properties, when not empty, names the properties to return.
func (c *Client) ReadRecords(ctx context.Context, objectType string, ids []string, properties []string) (BatchRead, error) {
	inputs := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		inputs = append(inputs, map[string]string{"id": id})
	}
	body := map[string]any{"inputs": inputs}
	if len(properties) > 0 {
		body["properties"] = properties
	}
	var out BatchRead
	status, err := c.do(ctx, request{method: http.MethodPost, path: "/crm/objects/" + APIVersion + "/" + objectType + "/batch/read", body: body}, &out)
	out.Status = status
	return out, err
}

// AssociationType is one association type an association carries.
type AssociationType struct {
	Category string  `json:"category"`
	TypeID   int     `json:"typeId"`
	Label    *string `json:"label"`
}

// AssociationInput is one input of an associations batch read; After is the
// cursor of the input's next page, empty on its first.
type AssociationInput struct {
	ID    string `json:"id"`
	After string `json:"after,omitempty"`
}

// AssociationRow is the associations batch read result for one input.
type AssociationRow struct {
	From struct {
		ID ID `json:"id"`
	} `json:"from"`
	To []struct {
		ToObjectID       ID                `json:"toObjectId"`
		AssociationTypes []AssociationType `json:"associationTypes"`
	} `json:"to"`
	Paging *struct {
		Next struct {
			After string `json:"after"`
		} `json:"next"`
	} `json:"paging"`
}

// NextAfter is the cursor of the input's next page, or "" when the result
// carries no paging.
func (r AssociationRow) NextAfter() string {
	if r.Paging == nil {
		return ""
	}
	return r.Paging.Next.After
}

// AssociationRead is an associations batch read answer; Status is 200, or 207
// when Errors names inputs that did not read.
type AssociationRead struct {
	Status  int              `json:"-"`
	Results []AssociationRow `json:"results"`
	Errors  []BatchError     `json:"errors"`
}

// ReadAssociations batch-reads the toType associations of up to
// AssociationBatchLimit fromType records.
func (c *Client) ReadAssociations(ctx context.Context, fromType, toType string, inputs []AssociationInput) (AssociationRead, error) {
	var out AssociationRead
	status, err := c.do(ctx, request{
		method: http.MethodPost,
		path:   "/crm/associations/" + APIVersion + "/" + fromType + "/" + toType + "/batch/read",
		body:   map[string]any{"inputs": inputs},
	}, &out)
	out.Status = status
	return out, err
}

// List is a list's definition.
type List struct {
	ListID       ID     `json:"listId"`
	Name         string `json:"name"`
	ObjectTypeID string `json:"objectTypeId"`
}

// ReadList reads one list's definition.
func (c *Client) ReadList(ctx context.Context, listID string) (List, error) {
	var out struct {
		List List `json:"list"`
	}
	_, err := c.do(ctx, request{method: http.MethodGet, path: "/crm/lists/" + APIVersion + "/" + listID}, &out)
	return out.List, err
}

// MembershipPage is one page of a list's memberships.
type MembershipPage struct {
	RecordIDs []string
	Next      string
}

// ReadMemberships reads one page of list listID's memberships; after is
// empty on the first page.
func (c *Client) ReadMemberships(ctx context.Context, listID, after string) (MembershipPage, error) {
	q := url.Values{"limit": {strconv.Itoa(MembershipPageSize)}}
	if after != "" {
		q.Set("after", after)
	}
	var out struct {
		Results []struct {
			RecordID ID `json:"recordId"`
		} `json:"results"`
		Paging *struct {
			Next struct {
				After string `json:"after"`
			} `json:"next"`
		} `json:"paging"`
	}
	if _, err := c.do(ctx, request{method: http.MethodGet, path: "/crm/lists/" + APIVersion + "/" + listID + "/memberships", query: q}, &out); err != nil {
		return MembershipPage{}, err
	}
	page := MembershipPage{}
	for _, r := range out.Results {
		page.RecordIDs = append(page.RecordIDs, string(r.RecordID))
	}
	if out.Paging != nil {
		page.Next = out.Paging.Next.After
	}
	return page, nil
}

// SearchPage is one page of a CRM search.
type SearchPage struct {
	Total   int
	Results []Record
	Next    string
}

// Search posts one page of a CRM search of objectType. The caller sets
// filterGroups, sorts, properties and after in body; limit is set here.
func (c *Client) Search(ctx context.Context, objectType string, body map[string]any) (SearchPage, error) {
	body["limit"] = SearchPageSize
	var out struct {
		Total   int      `json:"total"`
		Results []Record `json:"results"`
		Paging  *struct {
			Next struct {
				After string `json:"after"`
			} `json:"next"`
		} `json:"paging"`
	}
	if _, err := c.do(ctx, request{method: http.MethodPost, path: "/crm/objects/" + APIVersion + "/" + objectType + "/search", body: body, search: true}, &out); err != nil {
		return SearchPage{}, err
	}
	page := SearchPage{Total: out.Total, Results: out.Results}
	if out.Paging != nil {
		page.Next = out.Paging.Next.After
	}
	return page, nil
}

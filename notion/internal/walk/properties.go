// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/notionapi"
)

// propertyValue is the subset of a page property value this collector renders.
type propertyValue struct {
	Type     string               `json:"type"`
	Title    []notionapi.RichText `json:"title"`
	RichText []notionapi.RichText `json:"rich_text"`
	Number   *float64             `json:"number"`
	Select   *struct {
		Name string `json:"name"`
	} `json:"select"`
	Status *struct {
		Name string `json:"name"`
	} `json:"status"`
	MultiSelect []struct {
		Name string `json:"name"`
	} `json:"multi_select"`
	Date *struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"date"`
	Checkbox       *bool               `json:"checkbox"`
	URL            *string             `json:"url"`
	Email          *string             `json:"email"`
	PhoneNumber    *string             `json:"phone_number"`
	People         []notionapi.UserRef `json:"people"`
	Relation       []notionapi.UserRef `json:"relation"`
	CreatedTime    string              `json:"created_time"`
	LastEditedTime string              `json:"last_edited_time"`
	CreatedBy      *notionapi.UserRef  `json:"created_by"`
	LastEditedBy   *notionapi.UserRef  `json:"last_edited_by"`
	Files          []propertyFile      `json:"files"`
	UniqueID       *struct {
		Prefix *string `json:"prefix"`
		Number int     `json:"number"`
	} `json:"unique_id"`
	Formula *struct {
		Type    string   `json:"type"`
		String  *string  `json:"string"`
		Number  *float64 `json:"number"`
		Boolean *bool    `json:"boolean"`
		Date    *struct {
			Start string `json:"start"`
		} `json:"date"`
	} `json:"formula"`
}

// propertyFile is one entry of a files property. Only an external file keeps
// its URL; a Notion-hosted file's URL is a signed link that expires.
type propertyFile struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	External struct {
		URL string `json:"url"`
	} `json:"external"`
}

// renderProperty returns a property value's type and its value as text. A
// type this collector does not render returns its type and "".
func renderProperty(raw json.RawMessage) (string, string) {
	var v propertyValue
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", ""
	}
	switch v.Type {
	case "title":
		return v.Type, notionapi.PlainText(v.Title)
	case "rich_text":
		return v.Type, notionapi.PlainText(v.RichText)
	case "number":
		return v.Type, formatNumber(v.Number)
	case "select":
		if v.Select != nil {
			return v.Type, v.Select.Name
		}
	case "status":
		if v.Status != nil {
			return v.Type, v.Status.Name
		}
	case "multi_select":
		names := make([]string, 0, len(v.MultiSelect))
		for _, o := range v.MultiSelect {
			names = append(names, o.Name)
		}
		return v.Type, strings.Join(names, ", ")
	case "date":
		if v.Date != nil {
			return v.Type, strings.TrimSuffix(v.Date.Start+"/"+v.Date.End, "/")
		}
	case "checkbox":
		if v.Checkbox != nil {
			return v.Type, strconv.FormatBool(*v.Checkbox)
		}
	case "url", "email", "phone_number":
		return v.Type, deref(firstNonNil(v.URL, v.Email, v.PhoneNumber))
	}
	return v.Type, renderComputed(v)
}

// renderComputed renders the property types that reference other objects or
// are computed by Notion.
func renderComputed(v propertyValue) string {
	switch v.Type {
	case "people":
		return joinIDs(v.People)
	case "relation":
		return joinIDs(v.Relation)
	case "created_time":
		return v.CreatedTime
	case "last_edited_time":
		return v.LastEditedTime
	case "created_by":
		if v.CreatedBy != nil {
			return v.CreatedBy.ID
		}
	case "last_edited_by":
		if v.LastEditedBy != nil {
			return v.LastEditedBy.ID
		}
	case "files":
		return renderFiles(v.Files)
	case "unique_id":
		if v.UniqueID != nil {
			return deref(v.UniqueID.Prefix) + strconv.Itoa(v.UniqueID.Number)
		}
	case "formula":
		return renderFormula(v)
	}
	return ""
}

func renderFormula(v propertyValue) string {
	if v.Formula == nil {
		return ""
	}
	switch v.Formula.Type {
	case "string":
		return deref(v.Formula.String)
	case "number":
		return formatNumber(v.Formula.Number)
	case "boolean":
		if v.Formula.Boolean != nil {
			return strconv.FormatBool(*v.Formula.Boolean)
		}
	case "date":
		if v.Formula.Date != nil {
			return v.Formula.Date.Start
		}
	}
	return ""
}

func renderFiles(files []propertyFile) string {
	parts := make([]string, 0, len(files))
	for _, f := range files {
		if f.Type == "external" && f.External.URL != "" {
			parts = append(parts, f.Name+" "+f.External.URL)
		} else {
			parts = append(parts, f.Name)
		}
	}
	return strings.Join(parts, ", ")
}

func formatNumber(n *float64) string {
	if n == nil {
		return ""
	}
	return strconv.FormatFloat(*n, 'f', -1, 64)
}

func joinIDs(refs []notionapi.UserRef) string {
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	return strings.Join(ids, ", ")
}

func firstNonNil(values ...*string) *string {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

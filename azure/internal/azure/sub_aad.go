// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// sub_aad.go — Entra groups and their members, read from Microsoft Graph.
//
// WHY RAW HTTP AND NOT AN SDK. Microsoft Graph's Go SDK is an order of
// magnitude larger than every ARM package this collector uses put together, and
// this walk needs two endpoints from it. The token comes from the same
// credential every other subcollector uses; only the scope differs.
//
// WHY GROUPS AT ALL, in a subscription walk: a role assignment or a vault
// access policy names its principal by raw object id, and an object id says
// nothing about who is behind it. The group nodes are what turn those ids into
// something traversable, which is resolver 5's whole job.

const (
	graphGroupsPath  = "https://graph.microsoft.com/v1.0/groups"
	graphGroupSelect = "$select=id,displayName,mail,groupTypes,securityEnabled,membershipRule"
	graphScope       = "https://graph.microsoft.com/.default"
)

// aadPrincipalIDPrefix namespaces a member that is not itself a group. The
// member's object id may match a managed identity's principal id, which is how
// a reader joins the two.
const aadPrincipalIDPrefix = "azure:aad:principal/"

// aadGroupSub walks the tenant's Entra groups.
//
// baseURL and httpClient are unexported seams the tests set: the shipped binary
// leaves both zero and talks to Graph itself.
type aadGroupSub struct {
	subBase
	baseURL    string
	httpClient *http.Client
}

func (s *aadGroupSub) Collect(ctx context.Context) (subResult, error) {
	token, err := s.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{graphScope}})
	if err != nil {
		return subResult{}, fmt.Errorf(
			"acquiring a Microsoft Graph token: the credential this collector authenticates with cannot read "+
				"directory data, so no group membership is in this walk: %w", err)
	}

	groups, err := s.listGroups(ctx, token.Token)
	if err != nil {
		return subResult{}, err
	}

	var out subResult
	for _, g := range groups {
		if g.ID == "" {
			continue
		}
		r, err := groupResource(g)
		if err != nil {
			return out, err
		}
		out.resources = append(out.resources, r)
		members, err := s.listMembers(ctx, token.Token, g.ID)
		if err != nil {
			return out, err
		}
		out.edges = append(out.edges, membershipEdges(g.ID, members)...)
	}
	return out, nil
}

// graphGroup is the slice of a Graph group this walk reads.
type graphGroup struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"displayName"`
	Mail            string   `json:"mail"`
	GroupTypes      []string `json:"groupTypes"`
	SecurityEnabled bool     `json:"securityEnabled"`
	MembershipRule  string   `json:"membershipRule"`
}

// graphMember is the slice of a Graph directory object this walk reads. The
// OData type is what says whether the member is a nested group, a user or a
// service principal.
type graphMember struct {
	ODataType   string `json:"@odata.type"`
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// graphPage is Graph's pagination envelope. The next link is an absolute URL
// carrying an opaque skip token, so paging means following it rather than
// composing a query.
type graphPage[T any] struct {
	Value    []T    `json:"value"`
	NextLink string `json:"@odata.nextLink"`
}

func (s *aadGroupSub) listGroups(ctx context.Context, token string) ([]graphGroup, error) {
	url := s.groupsURL() + "?" + graphGroupSelect
	var all []graphGroup
	for url != "" {
		page, err := graphGet[graphGroup](ctx, s.client(), token, url)
		if err != nil {
			return nil, fmt.Errorf("listing directory groups: %w", err)
		}
		all = append(all, page.Value...)
		url = page.NextLink
	}
	return all, nil
}

func (s *aadGroupSub) listMembers(ctx context.Context, token, groupID string) ([]graphMember, error) {
	url := s.groupsURL() + "/" + groupID + "/members"
	var all []graphMember
	for url != "" {
		page, err := graphGet[graphMember](ctx, s.client(), token, url)
		if err != nil {
			return nil, fmt.Errorf("listing members of group %s: %w", groupID, err)
		}
		all = append(all, page.Value...)
		url = page.NextLink
	}
	return all, nil
}

func (s *aadGroupSub) groupsURL() string {
	if s.baseURL != "" {
		return s.baseURL
	}
	return graphGroupsPath
}

func (s *aadGroupSub) client() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	return http.DefaultClient
}

// graphGet performs one authenticated Graph read.
//
// A REFUSAL IS AN ERROR, not an empty page. A collector whose credential cannot
// read directory data produces no group nodes, so every grant naming a group
// stays an unresolvable object id — and reporting that as a successful walk
// would present a graph missing a whole class of relationship as complete.
func graphGet[T any](ctx context.Context, client *http.Client, token, url string) (graphPage[T], error) {
	var page graphPage[T]
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return page, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return page, fmt.Errorf("calling Microsoft Graph: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return page, fmt.Errorf("reading the Microsoft Graph response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return page, fmt.Errorf("the Microsoft Graph call returned %d: %s", resp.StatusCode, firstBytes(body, 200))
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return page, fmt.Errorf("decoding the Microsoft Graph response: %w", err)
	}
	return page, nil
}

// firstBytes bounds an error message's quotation of a response body. A Graph
// error body carries an explanation worth reading and can also carry a page of
// HTML from an intercepting proxy.
func firstBytes(body []byte, n int) string {
	s := strings.TrimSpace(string(body))
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func groupResource(g graphGroup) (resource, error) {
	content, err := marshalContent(g)
	if err != nil {
		return resource{}, fmt.Errorf("projecting directory group %s: %w", g.ID, err)
	}
	r := resource{
		id:           aadGroupIDPrefix + g.ID,
		name:         g.DisplayName,
		resourceType: rtAADGroup,
		content:      content,
		metadata: map[string]string{
			"displayName":     g.DisplayName,
			"securityEnabled": strconv.FormatBool(g.SecurityEnabled),
		},
	}
	setIfNotEmpty(r.metadata, "mail", g.Mail)
	// A membership rule means the group is DYNAMIC: its members are recomputed
	// by Entra rather than assigned, so the membership this walk recorded is a
	// snapshot of an evaluation rather than a configuration.
	setIfNotEmpty(r.metadata, "membershipRule", g.MembershipRule)
	if len(g.GroupTypes) > 0 {
		r.metadata["groupTypes"] = strings.Join(g.GroupTypes, ",")
	}
	return r, nil
}

// membershipEdges draws membership in BOTH directions, so a query can start
// from either the group or the member.
func membershipEdges(groupID string, members []graphMember) []edge {
	groupNode := aadGroupIDPrefix + groupID
	var out []edge
	for _, m := range members {
		if m.ID == "" {
			continue
		}
		memberNode := memberNodeID(m)
		md := map[string]string{"member_type": m.ODataType}
		out = append(out,
			edge{from: groupNode, to: memberNode, relation: edgeHasMember, metadata: md},
			edge{from: memberNode, to: groupNode, relation: edgeMemberOf, metadata: md},
		)
	}
	return out
}

// memberNodeID names a group member. A nested group gets the group namespace so
// it converges with that group's own node when the walk reaches it; everything
// else gets the principal namespace, where its object id is what a role
// assignment or an access policy would name.
func memberNodeID(m graphMember) string {
	if m.ODataType == "#microsoft.graph.group" {
		return aadGroupIDPrefix + m.ID
	}
	return aadPrincipalIDPrefix + m.ID
}

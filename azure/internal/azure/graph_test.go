// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// graph_test.go — the directory walk, against a fake Microsoft Graph.
//
// THE REFUSAL ARM IS THE ONE THAT MATTERS. A credential that can read a
// subscription commonly cannot read the directory, and the collector this one
// replaces treated that as a shrug: it logged and returned an empty result, so
// a subscription whose group memberships were never read looked exactly like
// one with no groups. That is a silently degraded walk, which this repository's
// bad-input invariant forbids, so the refusal is an error here and the walk
// that contains it asserts itself incomplete.

// fakeGraph serves a canned directory: one page of groups, then their members,
// with an optional status to fail on.
func fakeGraph(t *testing.T, status int) *aadGroupSub {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":"Authorization_RequestDenied","message":"Insufficient privileges"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/members"):
			write(t, w, graphPage[graphMember]{Value: groupMembers()})
		case r.URL.Query().Get("page") == "2":
			write(t, w, graphPage[graphGroup]{Value: []graphGroup{{
				ID: "88888888-8888-8888-8888-888888888888", DisplayName: "second-page",
			}}})
		default:
			// A FIRST PAGE WITH A NEXT LINK, so the walk's paging is exercised
			// rather than assumed: Graph pages with an opaque absolute link,
			// which a walk that composed its own query would never follow.
			write(t, w, graphPage[graphGroup]{
				Value:    []graphGroup{directoryGroup()},
				NextLink: server.URL + "/groups?page=2",
			})
		}
	}))
	t.Cleanup(server.Close)

	return &aadGroupSub{
		subBase:    subBase{name: nameAADGroups, cred: fakeCredential{}},
		baseURL:    server.URL + "/groups",
		httpClient: server.Client(),
	}
}

func write(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("encoding the fake Graph response: %v", err)
	}
}

// TestAADGroupSub_FollowsPagingAndDrawsMembershipBothWays.
func TestAADGroupSub_FollowsPagingAndDrawsMembershipBothWays(t *testing.T) {
	sub := fakeGraph(t, http.StatusOK)

	out, err := sub.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	types := resourceTypesOf(out.resources)
	if types[rtAADGroup] != 2 {
		t.Errorf("the walk produced %d group nodes; the fake directory has two pages of one group each", types[rtAADGroup])
	}

	groupNode := aadGroupIDPrefix + groupObjectID
	userNode := aadPrincipalIDPrefix + memberUserID
	if _, ok := edgeBetween(out.edges, groupNode, userNode, edgeHasMember); !ok {
		t.Error("no membership edge from the group to its member")
	}
	if _, ok := edgeBetween(out.edges, userNode, groupNode, edgeMemberOf); !ok {
		t.Error("no membership edge from the member back to its group")
	}
}

// TestAADGroupSub_ARefusedDirectoryReadIsAnErrorRatherThanAnEmptyWalk. The
// error is what makes the walk containing it assert itself incomplete, which is
// the difference between "this subscription has no group memberships" and "this
// run could not read them".
func TestAADGroupSub_ARefusedDirectoryReadIsAnErrorRatherThanAnEmptyWalk(t *testing.T) {
	sub := fakeGraph(t, http.StatusForbidden)

	out, err := sub.Collect(context.Background())
	if err == nil {
		t.Fatal("a refused directory read produced a successful empty walk, which is indistinguishable from a directory with no groups")
	}
	if len(out.resources) != 0 {
		t.Errorf("a refused read still produced %d resources", len(out.resources))
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("the error does not carry the status the directory returned: %v", err)
	}
	if !strings.Contains(err.Error(), "Insufficient privileges") {
		t.Errorf("the error does not carry the directory's own explanation, which is what an operator acts on: %v", err)
	}
}

// TestAADGroupSub_AFailedDirectoryReadMakesTheWalkIncomplete. The end of the
// same chain, at the level the assertion is made.
func TestAADGroupSub_AFailedDirectoryReadMakesTheWalkIncomplete(t *testing.T) {
	sub := fakeGraph(t, http.StatusForbidden)

	c := testCollector(sub, staticSub{name: "azure-vms", result: childWalkResult()})
	result, err := c.Walk(context.Background(), "sub-1", Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if result.Complete.IsComplete() {
		t.Fatal("a walk whose directory read was refused asserted itself complete")
	}
	if !strings.Contains(result.Complete.Reason(), nameAADGroups) {
		t.Errorf("the incomplete reason does not name the directory walk: %s", result.Complete.Reason())
	}
	// And the resources the other subcollectors gathered survive.
	if len(result.Nodes) != 1 {
		t.Errorf("the healthy subcollector's nodes were discarded: %v", result.Nodes)
	}
}

// TestAADGroupSub_AnUnreadableTokenIsAnErrorToo. A credential that cannot get a
// directory token is the same failure one step earlier, and it must not be a
// quiet skip either.
func TestAADGroupSub_AnUnreadableTokenIsAnErrorToo(t *testing.T) {
	sub := &aadGroupSub{subBase: subBase{name: nameAADGroups, cred: refusingCredential{}}}
	_, err := sub.Collect(context.Background())
	if err == nil {
		t.Fatal("a credential that cannot read directory data produced a successful walk")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("the error does not say what could not be read: %v", err)
	}
}

// TestMemberNodeID_NestedGroupsConvergeWithTheirOwnNodes. A nested group that
// got the principal namespace would sit beside its own node forever.
func TestMemberNodeID_NestedGroupsConvergeWithTheirOwnNodes(t *testing.T) {
	nested := memberNodeID(graphMember{ODataType: "#microsoft.graph.group", ID: "abc"})
	if nested != aadGroupIDPrefix+"abc" {
		t.Errorf("a nested group is addressed as %q", nested)
	}
	user := memberNodeID(graphMember{ODataType: "#microsoft.graph.user", ID: "abc"})
	if user != aadPrincipalIDPrefix+"abc" {
		t.Errorf("a user member is addressed as %q", user)
	}
	if nested == user {
		t.Error("a nested group and a user with the same object id are addressed identically")
	}
}

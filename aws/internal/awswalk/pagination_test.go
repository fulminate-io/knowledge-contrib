// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// pagination_test.go — THE MULTI-PAGE CLASS, which is the recurring defect in
// this kind of code and the one that hides best.
//
// An AWS list call returns one page and a continuation token. A walk that reads
// the first page reports a truncated account that looks EXACTLY like a small one:
// no error, no warning, a plausible graph. Nothing else in this suite would
// notice, because every other fixture fits in one page.
//
// FOUR PAGING CONVENTIONS ARE EXERCISED, not one, because the SDK has four and a
// walk that adapted three correctly would still lose the fourth silently:
// NextToken (EC2), Marker/IsTruncated (IAM), Marker/NextMarker (Lambda), and a
// continuation token (S3).

// pager answers a fixed number of pages, one item each, and records how many
// times it was called.
type pager struct {
	pages int
	calls int
}

// next returns the token for page n, or nil on the last page.
func (p *pager) next(n int) *string {
	if n+1 >= p.pages {
		return nil
	}
	tok := "page-" + string(rune('a'+n))
	return &tok
}

// index maps an incoming token back to the page number it asks for.
func (p *pager) index(token *string) int {
	if token == nil {
		return 0
	}
	return int((*token)[len(*token)-1] - 'a' + 1)
}

func TestPagination_EC2NextTokenIsFollowedToExhaustion(t *testing.T) {
	p := &pager{pages: 3}
	clients := emptyClients()
	clients.EC2.(*fakeEC2).describeVpcs = func(in *ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
		p.calls++
		n := p.index(in.NextToken)
		return &ec2.DescribeVpcsOutput{
			Vpcs:      []ec2types.Vpc{{VpcId: new("vpc-page" + string(rune('a'+n)))}},
			NextToken: p.next(n),
		}, nil
	}
	res := runWalk(t, clients, Params{})
	if p.calls != 3 {
		t.Fatalf("the vpc walk called the API %d times over a 3-page response; it stopped short", p.calls)
	}
	if got := len(nodesOfType(res, ResourceTypeVPC)); got != 3 {
		t.Fatalf("a 3-page vpc response yielded %d nodes; a walk that reads page one reports a truncated "+
			"account that looks exactly like a small one", got)
	}
}

func TestPagination_IAMMarkerAndIsTruncatedAreFollowed(t *testing.T) {
	p := &pager{pages: 3}
	clients := emptyClients()
	clients.IAM.(*fakeIam).listRoles = func(in *iam.ListRolesInput) (*iam.ListRolesOutput, error) {
		p.calls++
		n := p.index(in.Marker)
		marker := p.next(n)
		return &iam.ListRolesOutput{
			Roles: []iamtypes.Role{{
				Arn:      new("arn:aws:iam::123456789012:role/page" + string(rune('a'+n))),
				RoleName: new("page" + string(rune('a'+n))),
			}},
			// IAM SIGNALS WITH IsTruncated AND CARRIES THE MARKER SEPARATELY, so a
			// walk that read the marker alone would stop on the last page's nil and
			// one that read IsTruncated alone would loop forever on page one.
			IsTruncated: marker != nil,
			Marker:      marker,
		}, nil
	}
	res := runWalk(t, clients, Params{})
	if p.calls != 3 {
		t.Fatalf("the iam-role walk called the API %d times over a 3-page response", p.calls)
	}
	if got := len(nodesOfType(res, ResourceTypeIAMRole)); got != 3 {
		t.Fatalf("a 3-page role response yielded %d nodes", got)
	}
}

func TestPagination_LambdaNextMarkerIsFollowed(t *testing.T) {
	p := &pager{pages: 3}
	clients := emptyClients()
	clients.Lambda.(*fakeLambda).listFunctions = func(in *lambda.ListFunctionsInput) (*lambda.ListFunctionsOutput, error) {
		p.calls++
		n := p.index(in.Marker)
		return &lambda.ListFunctionsOutput{
			Functions: []lambdatypes.FunctionConfiguration{{
				FunctionArn:  new("arn:aws:lambda:us-east-1:123456789012:function:page" + string(rune('a'+n))),
				FunctionName: new("page" + string(rune('a'+n))),
			}},
			NextMarker: p.next(n),
		}, nil
	}
	res := runWalk(t, clients, Params{})
	if p.calls != 3 {
		t.Fatalf("the lambda walk called the API %d times over a 3-page response", p.calls)
	}
	if got := len(nodesOfType(res, ResourceTypeLambda)); got != 3 {
		t.Fatalf("a 3-page function response yielded %d nodes", got)
	}
}

func TestPagination_S3ContinuationTokenIsFollowed(t *testing.T) {
	p := &pager{pages: 3}
	clients := emptyClients()
	clients.S3.(*fakeS3).listBuckets = func(in *s3.ListBucketsInput) (*s3.ListBucketsOutput, error) {
		p.calls++
		n := p.index(in.ContinuationToken)
		return &s3.ListBucketsOutput{
			Buckets:           []s3types.Bucket{{Name: new("bucket-page" + string(rune('a'+n)))}},
			ContinuationToken: p.next(n),
		}, nil
	}
	res := runWalk(t, clients, Params{})
	if p.calls != 3 {
		t.Fatalf("the s3 walk called the API %d times over a 3-page response", p.calls)
	}
	if got := len(nodesOfType(res, ResourceTypeS3Bucket)); got != 3 {
		t.Fatalf("a 3-page bucket response yielded %d nodes", got)
	}
}

// TestPagination_ANonAdvancingTokenIsRefused covers the guard rather than the
// happy path.
//
// An API returning the same token twice would otherwise be an infinite loop
// producing a plausible-looking result forever. The refusal is the walk's, so it
// surfaces as that service's failure and an INCOMPLETE account rather than as a
// hang nobody can attribute.
func TestPagination_ANonAdvancingTokenIsRefused(t *testing.T) {
	stuck := "always-the-same"
	err := paginate(context.Background(),
		func(context.Context, *string) (int, error) { return 1, nil },
		func(int) *string { return &stuck },
		func(int) error { return nil })
	if err == nil {
		t.Fatal("a service returning the same continuation token twice must be refused, not looped on")
	}
	if !strings.Contains(err.Error(), "did not advance") {
		t.Errorf("the refusal must name the cause; got %v", err)
	}
}

// TestPagination_CancellationStopsTheLoop covers the context arm of paginate: a
// cancelled collect stops paginating rather than draining a large account nobody
// is waiting for.
func TestPagination_CancellationStopsTheLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	tok := "more"
	err := paginate(ctx,
		func(context.Context, *string) (int, error) {
			calls++
			cancel()
			return 1, nil
		},
		func(int) *string { return &tok },
		func(int) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled pagination must return the cancellation; got %v", err)
	}
	if calls != 1 {
		t.Errorf("the loop made %d calls after cancellation; it must stop at the next iteration", calls)
	}
}

// TestPagination_APageErrorSurfaces asserts an error on page two is not swallowed
// by the pages already collected.
func TestPagination_APageErrorSurfaces(t *testing.T) {
	boom := errors.New("ThrottlingException: Rate exceeded")
	page := 0
	tok := "more"
	err := paginate(context.Background(),
		func(context.Context, *string) (int, error) {
			page++
			if page == 2 {
				return 0, boom
			}
			return page, nil
		},
		func(int) *string { return &tok },
		func(int) error { return nil })
	if !errors.Is(err, boom) {
		t.Fatalf("an error on a later page must surface; got %v", err)
	}
}

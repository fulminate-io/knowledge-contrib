// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	gl "gitlab.com/gitlab-org/api/client-go"
)

// fixture_paging_test.go — the two things the recorded provider does that a map
// lookup would not: it PAGES in the provider's own convention, and it FAILS with
// the provider's own error type.
//
// BOTH ARE HERE RATHER THAN BESIDE THE FAKE'S METHODS because they are the only
// parts of the fixture with behavior of their own — everything in
// fixture_api_test.go is a lookup that calls one of them.

// pages is a paginated answer: one slice per page, served in order. A single
// page is the ordinary case and is written as one element.
type pages[T any] [][]*T

// page returns the requested page and the response carrying the next page
// number, in the provider's own convention: page 0 and page 1 are both the
// first, and NextPage is 0 on the last.
func (p pages[T]) page(requested int64) ([]*T, *gl.Response) {
	if len(p) == 0 {
		return nil, &gl.Response{}
	}
	index := max(requested, 1) - 1
	if index >= int64(len(p)) {
		return nil, &gl.Response{}
	}
	resp := &gl.Response{}
	if index+1 < int64(len(p)) {
		resp.NextPage = index + 2
	}
	return p[index], resp
}

// refusal is the provider's answer when a credential may not read something. It
// is built from the SDK's own error type carrying the real status, because that
// is what this collector's classifier reads — an error built any other way would
// exercise a classification path the provider never produces.
//
// THE REQUEST CARRIES A METHOD AND A URL, and that is not decoration. The SDK's
// Error method dereferences Response.Request.URL unconditionally, so a request
// built as &http.Request{} makes every RENDERING of this error panic; fmt
// recovers and substitutes a panic marker. Classification still works — it reads
// the status — so a suite built that way measures the right verdicts while every
// reason text it observes carries a marker where the provider's own message
// belongs, and no test ever sees what an operator will read.
func refusal(status int, message string) error {
	return &gl.ErrorResponse{
		Response: &http.Response{
			StatusCode: status,
			Request: &http.Request{
				Method: http.MethodGet,
				URL: &url.URL{
					Scheme: "https",
					Host:   "gitlab.example.com",
					Path:   "/api/v4/groups/acme/projects",
				},
			},
		},
		Message: message,
	}
}

// THE TWO READS THAT DO NOT PAGE, kept beside the paging transport because
// they are the exceptions to it: a runner's detail is one object and a
// repository file is one document, so neither carries a page and neither is
// logged. The file read answers the provider's own 404 for a file that is not
// there, which is the input class the collector must NOT report as a failure.

func (f *fakeAPI) GetRunnerDetails(
	_ context.Context, rid any,
) (*gl.RunnerDetails, *gl.Response, error) {
	id, _ := rid.(int64)
	if err, bad := f.runnerDetailsErr[id]; bad {
		return nil, nil, err
	}
	return f.runnerDetails[id], &gl.Response{}, nil
}

func (f *fakeAPI) GetFile(
	_ context.Context, pid any, fileName string, _ *gl.GetFileOptions,
) (*gl.File, *gl.Response, error) {
	key := fmt.Sprintf("%d/%s", projectKey(pid), fileName)
	if err, bad := f.filesErr[key]; bad {
		return nil, nil, err
	}
	file, found := f.files[key]
	if !found {
		// THE PROVIDER'S OWN ANSWER FOR A FILE THAT IS NOT THERE, which is the
		// input class this collector must NOT report as an incompleteness.
		return nil, nil, refusal(http.StatusNotFound, "404 File Not Found")
	}
	return file, &gl.Response{}, nil
}

// SPDX-License-Identifier: Apache-2.0

package collect

// export_test.go — the one production constant the external test package needs
// to READ rather than to restate.
//
// WHY IT EXISTS. The boundary table splits every recorded answer at a page size
// and asserts the collector asked the provider for that same size. Written as two
// literals — production's `perPage` and a `fixturePageSize` beside it — those are
// two independent numbers with nothing joining them: change production's and the
// arm named "exactly 100 items, one full page that is still the last" stops being
// a full page in production terms, and the row keeps passing while the boundary it
// is named for is no longer exercised. It goes QUIET rather than red, which is the
// one thing a boundary test must not do.
//
// WHY IT IS A TEST FILE AND NOT AN EXPORTED CONSTANT. `perPage` is an
// implementation detail: nothing outside this package chooses it, and exporting it
// would invite a caller to. A `_test.go` file in the package under test is the
// language's own answer to this — it is compiled only under `go test` and ships in
// no binary, so the test package gets the real value while the module's surface is
// unchanged.
//
// THE VALUE IS NEVER COPIED. Everything downstream reads this, so there is exactly
// one place the number is written.
const PageSizeForTest = perPage

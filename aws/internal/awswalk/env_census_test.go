// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// env_census_test.go — THE INSTRUMENTS the environment census in
// env_test.go runs, split out for length.
//
// EVERY ONE READS THE PINNED MODULE CACHE, which is why the census's own fence
// opens this module's go.mod and go.sum: those two files decide the dependency
// set these helpers walk, and they are the only part of it a cache key can see.

// awsEnvNameRE matches an AWS_ environment variable name as a Go string literal.
var awsEnvNameRE = regexp.MustCompile(`"(AWS_[A-Z0-9_]+)"`)

// moduleDir resolves one dependency module's directory in the pinned module
// cache.
func moduleDir(t *testing.T, pkg string) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", moduleOf(pkg)).Output() //nolint:gosec // a literal module path
	if err != nil {
		t.Fatalf("resolve the module directory for %s: %v", pkg, err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatalf("the module for %s resolved to an empty directory", pkg)
	}
	// A service PACKAGE lives under its module directory; the module for an
	// aws-sdk-go-v2 service is the package path itself.
	return dir
}

// moduleOf returns the module path a package belongs to. Every aws-sdk-go-v2
// service is its own module, so the package path IS the module path.
func moduleOf(pkg string) string { return pkg }

// closureServicePackages enumerates the aws-sdk-go-v2 service packages in this
// module's dependency closure, excluding the generated types and schema
// sub-packages that declare no client.
func closureServicePackages(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", "./...").Output()
	if err != nil {
		t.Fatalf("enumerate the dependency closure: %v", err)
	}
	var pkgs []string
	for line := range strings.FieldsSeq(string(out)) {
		if !strings.Contains(line, "/aws-sdk-go-v2/service/") || strings.Contains(line, "/internal/") {
			continue
		}
		if strings.HasSuffix(line, "/types") || strings.HasSuffix(line, "/schemas") ||
			strings.HasSuffix(line, "/document") {
			continue
		}
		pkgs = append(pkgs, line)
	}
	slices.Sort(pkgs)
	return slices.Compact(pkgs)
}

// awsNamesInPackage returns every AWS_ name appearing as a string literal in a
// package's non-test Go sources.
func awsNamesInPackage(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		names = append(names, awsNamesInFile(t, filepath.Join(dir, e.Name()))...)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// awsNamesInFile returns every AWS_ name appearing as a string literal in one
// file. A file that does not exist yields nothing, which is correct for a service
// package with no endpoints.go.
func awsNamesInFile(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var names []string
	for _, m := range awsEnvNameRE.FindAllStringSubmatch(string(body), -1) {
		names = append(names, m[1])
	}
	slices.Sort(names)
	return slices.Compact(names)
}

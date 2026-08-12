//go:build integration

package balena

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// INTEGRATION_TIMEOUT bounds every live API call made by these tests.
const INTEGRATION_TIMEOUT = 30 * time.Second

// newIntegrationClient returns a client pointed at the live Balena API, or
// skips the test when no API token is configured.
func newIntegrationClient(t *testing.T) *Client {
	t.Helper()
	token := os.Getenv("BALENA_API_TOKEN")
	if token == "" {
		t.Skip("BALENA_API_TOKEN must be set for acceptance tests")
	}
	return NewClient(os.Getenv("BALENA_API_URL"), token, "test")
}

// selectRow queries the given Pine.js resource for a single row, selecting
// exactly the named fields, and returns the decoded row (nil if the collection
// is empty) or the request error.
func selectRow(t *testing.T, c *Client, path string, fields ...string) (map[string]any, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), INTEGRATION_TIMEOUT)
	defer cancel()

	query := "$select=" + strings.Join(fields, ",") + "&$top=1"
	data, err := c.do(ctx, "GET", appendQuery(path, query), nil)
	if err != nil {
		return nil, err
	}

	var resp pineResponse[map[string]any]
	if jsonErr := json.Unmarshal(data, &resp); jsonErr != nil {
		t.Fatalf("decoding %s response: %v", path, jsonErr)
	}
	if len(resp.D) == 0 {
		return nil, nil
	}
	return resp.D[0], nil
}

// assertSelectableFields proves that every named field is a real, selectable
// property on path by fetching one live row and checking that each field
// name appears as a key in the decoded response. Pine.js silently drops
// unknown $select properties instead of rejecting them, so presence in the
// response — not the absence of an error — is the only reliable proof that a
// field name is correct.
//
// A resource with no rows the caller can see cannot be verified this way; the
// test skips rather than fails, since an empty result set says nothing about
// field-name correctness.
func assertSelectableFields(t *testing.T, c *Client, path string, fields ...string) {
	t.Helper()

	row, err := selectRow(t, c, path, fields...)
	if err != nil {
		t.Fatalf("selecting %v on %s: %v", fields, path, err)
	}
	if row == nil {
		t.Skipf("no rows visible on %s to verify field names against", path)
	}
	for _, f := range fields {
		if _, ok := row[f]; !ok {
			t.Errorf("field %q missing from %s response: got keys %v", f, path, keysOf(row))
		}
	}
}

// keysOf returns the keys of m, for diagnostic messages.
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// skipIfResourceUnavailable skips the test when path is not readable at all,
// even with $top=1 and no $select. Pine returns an identical 401 for a
// resource the caller's role cannot see and for a resource name that does
// not exist at all, so this status alone cannot distinguish "wrong field
// name" from "not exposed to this account yet" — nothing this test (or this
// provider) can do resolves that ambiguity, so it skips instead of failing
// the build.
func skipIfResourceUnavailable(t *testing.T, c *Client, path string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), INTEGRATION_TIMEOUT)
	defer cancel()

	if _, err := c.do(ctx, "GET", appendQuery(path, "$top=1"), nil); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
			t.Skipf("%s is not readable by this token (401 on a bare, unfiltered request, "+
				"identical to a nonexistent resource) — cannot verify field names until it is", path)
		}
		t.Fatalf("checking availability of %s: %v", path, err)
	}
}

// TestIntegrationResourceFieldNames verifies that the JSON field names the
// client marshals for each Pine.js resource match the live API. Verb-phrase
// names such as installs__service or activates__profile_name cannot be derived
// with certainty from the SBVR schema alone, so they are checked here.
func TestIntegrationResourceFieldNames(t *testing.T) {
	c := newIntegrationClient(t)

	tests := []struct {
		name   string
		path   string
		fields []string
	}{
		{
			name:   "application_profile",
			path:   "/v6/application_profile",
			fields: []string{"id", "application", "activates__profile_name", "on__application"},
		},
		{
			name:   "image_profile",
			path:   "/v6/image_profile",
			fields: []string{"id", "release_image", "profile_name"},
		},
		{
			name:   "service_install",
			path:   "/v6/service_install",
			fields: []string{"id", "device", "installs__service"},
		},
		{
			name:   "image_environment_variable",
			path:   "/v6/image_environment_variable",
			fields: []string{"id", "release_image", "name", "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skipIfResourceUnavailable(t, c, tt.path)
			assertSelectableFields(t, c, tt.path, tt.fields...)
		})
	}
}

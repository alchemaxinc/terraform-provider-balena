//go:build integration

package balena

import (
	"context"
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

// selectFields queries the given Pine.js resource for a single row, selecting
// exactly the named fields, and returns the resulting error (nil on success).
func selectFields(t *testing.T, c *Client, path string, fields ...string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), INTEGRATION_TIMEOUT)
	defer cancel()

	query := "$select=" + strings.Join(fields, ",") + "&$top=1"
	_, err := c.do(ctx, "GET", appendQuery(path, query), nil)
	return err
}

// assertSelectableFields fails when the API rejects any of the named fields.
// Pine parses $select against its model before applying resource permissions,
// so an unknown property yields a 400 while a well-formed query on a resource
// the token may not read yields a 401. Only the former indicates a wrong field
// name; the latter is reported as a skip.
func assertSelectableFields(t *testing.T, c *Client, path string, fields ...string) {
	t.Helper()

	err := selectFields(t, c, path, fields...)
	if err == nil {
		return
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
		t.Skipf("token cannot read %s; field names %v parsed without a 400", path, fields)
	}
	t.Fatalf("selecting %v on %s: %v", fields, path, err)
}

// assertUnknownFieldRejected is the negative control for assertSelectableFields:
// it proves that Pine still returns a 400 for a nonexistent property, so that a
// skipped 401 subtest cannot silently hide a misspelled field name.
func assertUnknownFieldRejected(t *testing.T, c *Client, path string) {
	t.Helper()

	err := selectFields(t, c, path, "id", "definitely_not_a_real_field")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("selecting an unknown field on %s: got %v, want status 400", path, err)
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
			assertUnknownFieldRejected(t, c, tt.path)
			assertSelectableFields(t, c, tt.path, tt.fields...)
		})
	}
}

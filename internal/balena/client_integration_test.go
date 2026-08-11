//go:build integration

package balena

import (
	"context"
	"encoding/json"
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

// assertSelectableFields queries the given Pine.js resource for a single row,
// selecting exactly the named fields. Pine rejects an unknown property with a
// 400, so a successful response proves every field name exists on the resource.
func assertSelectableFields(t *testing.T, c *Client, path string, fields ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), INTEGRATION_TIMEOUT)
	defer cancel()

	query := "$select=" + strings.Join(fields, ",") + "&$top=1"
	data, err := c.do(ctx, "GET", appendQuery(path, query), nil)
	if err != nil {
		t.Fatalf("selecting %v on %s: %v", fields, path, err)
	}

	var resp pineResponse[map[string]any]
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decoding %s response: %v", path, err)
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
			assertSelectableFields(t, c, tt.path, tt.fields...)
		})
	}
}

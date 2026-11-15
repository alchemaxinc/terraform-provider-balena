//go:build integration

package generated

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/alchemaxinc/terraform-provider-balena/internal/engine"
)

// PROBE_TIMEOUT bounds each probe request.
const PROBE_TIMEOUT = 30 * time.Second

// TestIntegrationGeneratedFieldsExist checks every generated resource against
// the live API: selecting all of its fields, with no rows, must succeed. This
// catches drift between the pinned upstream release and production. A 401
// means the token cannot read the collection, which the API also returns for
// an unknown collection, so such resources are skipped, not failed.
func TestIntegrationGeneratedFieldsExist(t *testing.T) {
	token := os.Getenv("BALENA_API_TOKEN")
	if token == "" {
		t.Skip("BALENA_API_TOKEN must be set for acceptance tests")
	}
	c := balena.NewClient(os.Getenv("BALENA_API_URL"), token, "test")
	for _, d := range RESOURCES {
		t.Run(d.TypeName, func(t *testing.T) {
			fields := []string{engine.ID_ATTRIBUTE}
			for _, a := range d.Attributes {
				fields = append(fields, a.Wire)
			}
			ctx, cancel := context.WithTimeout(context.Background(), PROBE_TIMEOUT)
			defer cancel()
			err := c.SelectFields(ctx, d.Path, fields)
			var apiErr *balena.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
				t.Skipf("%s is not readable with this token", d.Path)
			}
			if err != nil {
				t.Errorf("selecting %v on %s: %v", fields, d.Path, err)
			}
		})
	}
}

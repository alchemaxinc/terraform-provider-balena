//go:build integration

package gen

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
)

// TestIntegrationContract checks every spec's table, every column it reads and every column a hook declares against
// the live API, read-only. It replaces the hand-kept field-name checks of the old client.
func TestIntegrationContract(t *testing.T) {
	token := os.Getenv("BALENA_API_TOKEN")
	if token == "" {
		t.Skip("BALENA_API_TOKEN must be set for integration tests")
	}
	res, err := Generate(REPO)
	if err != nil {
		t.Fatal(err)
	}
	client := balena.NewClient(os.Getenv("BALENA_API_URL"), token, "test")
	ctx := context.Background()
	if _, err := client.Do(ctx, "GET", "/actor/v1/whoami", nil); err != nil {
		t.Fatalf("BALENA_API_TOKEN does not authenticate: %v", err)
	}
	checks := ContractChecks(res.IR, res.Specs, res.Overrides)
	RunContract(ctx, client, checks)
	if failures := ContractFailures(checks); len(failures) > 0 {
		t.Errorf("contract failures:\n%s", strings.Join(failures, "\n"))
	}
}

package provider

import (
	"fmt"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// configureClient validates that provider data is a *balena.Client. On mismatch it appends a diagnostic and returns
// ok=false. role is "Resource" or "Data Source" for the error message.
func configureClient(providerData interface{}, diags *diag.Diagnostics, role string) (*balena.Client, bool) {
	if providerData == nil {
		return nil, false
	}
	client, ok := providerData.(*balena.Client)
	if !ok {
		diags.AddError(
			fmt.Sprintf("Unexpected %s Configure Type", role),
			fmt.Sprintf("Expected *balena.Client, got %T. This is a bug in the provider — please report it.", providerData),
		)
		return nil, false
	}
	return client, true
}

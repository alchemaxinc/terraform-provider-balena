package provider

import (
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// parseID parses a string ID into an int64.
func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// optionalString converts a framework string into a *string, returning nil for
// null or unknown values so that optional API fields are omitted or cleared.
func optionalString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

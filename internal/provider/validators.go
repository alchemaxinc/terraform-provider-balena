package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// stringValidators returns the validators a spec derives for a string attribute: SBVR length bounds (in characters,
// as the SBVR counts them) and enums, the variable-name rules of /config/vars, and the RFC 3339 format of date-times.
func stringValidators(a *AttributeSpec) []validator.String {
	var out []validator.String
	switch {
	case a.MinLength > 0 && a.MaxLength > 0:
		out = append(out, stringvalidator.UTF8LengthBetween(a.MinLength, a.MaxLength))
	case a.MaxLength > 0:
		out = append(out, stringvalidator.UTF8LengthAtMost(a.MaxLength))
	case a.MinLength > 0:
		out = append(out, stringvalidator.UTF8LengthAtLeast(a.MinLength))
	}
	if a.DateTime {
		out = append(out, dateTimeValidator{})
	}
	if len(a.OneOf) > 0 {
		out = append(out, stringvalidator.OneOf(a.OneOf...))
	}
	if a.VarName != nil {
		out = append(out, varNameValidator{rule: a.VarName, invalid: regexp.MustCompile(a.VarName.Invalid)})
	}
	return out
}

// dateTimeValidator accepts RFC 3339 date-times, the notation the API reads and writes.
type dateTimeValidator struct{}

func (dateTimeValidator) Description(_ context.Context) string {
	return "value must be an RFC 3339 date-time, for example 2026-12-01T00:00:00Z"
}

func (v dateTimeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (dateTimeValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := time.Parse(time.RFC3339Nano, req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid date-time",
			fmt.Sprintf("%q is not an RFC 3339 date-time such as 2026-12-01T00:00:00Z.", req.ConfigValue.ValueString()))
	}
}

// varNameValidator applies a VarNameRule.
type varNameValidator struct {
	rule    *VarNameRule
	invalid *regexp.Regexp
}

func (v varNameValidator) Description(_ context.Context) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("must not match %s", v.rule.Invalid))
	if len(v.rule.Reserved) > 0 {
		parts = append(parts, "must not be one of "+strings.Join(v.rule.Reserved, ", "))
	}
	if len(v.rule.RequiredPrefixes) > 0 {
		parts = append(parts, "must start with "+strings.Join(v.rule.RequiredPrefixes, " or "))
	}
	if len(v.rule.ForbiddenPrefixes) > 0 {
		parts = append(parts, "must not start with "+strings.Join(v.rule.ForbiddenPrefixes, " or "))
	}
	return "value " + strings.Join(parts, "; ")
}

func (v varNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

// check returns why a name is invalid, or "".
func (v varNameValidator) check(name string) string {
	if name == "" || v.invalid.MatchString(name) {
		return fmt.Sprintf("the name matches the invalid pattern %s", v.rule.Invalid)
	}
	for _, r := range v.rule.Reserved {
		if name == r {
			return "the name " + r + " is reserved"
		}
	}
	if len(v.rule.RequiredPrefixes) > 0 {
		ok := false
		for _, p := range v.rule.RequiredPrefixes {
			ok = ok || strings.HasPrefix(name, p)
		}
		if !ok {
			return "the name must start with " + strings.Join(v.rule.RequiredPrefixes, " or ")
		}
	}
	for _, p := range v.rule.ForbiddenPrefixes {
		if strings.HasPrefix(name, p) {
			return "names starting with " + p + " are reserved"
		}
	}
	return ""
}

func (v varNameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if reason := v.check(req.ConfigValue.ValueString()); reason != "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid variable name", fmt.Sprintf("%q is not a valid name: %s.", req.ConfigValue.ValueString(), reason))
	}
}

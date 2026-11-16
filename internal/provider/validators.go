package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// stringValidators returns the validators a spec derives for a string attribute: SBVR length bounds and enums, and
// the variable-name rules of /config/vars.
func stringValidators(a *AttributeSpec) []validator.String {
	var out []validator.String
	switch {
	case a.MinLength > 0 && a.MaxLength > 0:
		out = append(out, stringvalidator.LengthBetween(a.MinLength, a.MaxLength))
	case a.MaxLength > 0:
		out = append(out, stringvalidator.LengthAtMost(a.MaxLength))
	case a.MinLength > 0:
		out = append(out, stringvalidator.LengthAtLeast(a.MinLength))
	}
	if len(a.OneOf) > 0 {
		out = append(out, stringvalidator.OneOf(a.OneOf...))
	}
	if a.VarName != nil {
		out = append(out, varNameValidator{rule: a.VarName, invalid: regexp.MustCompile(a.VarName.Invalid)})
	}
	return out
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

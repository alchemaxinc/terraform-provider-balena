package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// envVarNameValidator enforces POSIX-ish environment variable naming: must
// begin with a letter or underscore, followed by letters, digits or underscores.
// Balena additionally reserves the RESIN_ and BALENA_ prefixes for config vars,
// not env vars, but that's validated server-side.
var envVarNameValidator = regexpStringValidator(
	regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`),
	"must start with a letter or underscore and contain only letters, digits, and underscores",
)

// configVarNameValidator allows the dotted host-config keys used by Balena
// (e.g. RESIN_HOST_CONFIG_gpu_mem, BALENA_HOST_CONFIG_dtoverlay).
var configVarNameValidator = regexpStringValidator(
	regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`),
	"must start with a letter or underscore and contain only letters, digits, underscores, and dots",
)

// tagKeyValidator enforces Balena's tag key rules: non-empty, no whitespace,
// max 100 characters. The full rules are enforced server-side.
var tagKeyValidator = regexpStringValidator(
	regexp.MustCompile(`^[^\s]{1,100}$`),
	"must be non-empty, contain no whitespace, and be at most 100 characters",
)

// labelNameValidator matches Docker/Balena service label naming (dotted reverse
// DNS plus hyphens are allowed).
var labelNameValidator = regexpStringValidator(
	regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`),
	"must start with a letter or underscore and contain only letters, digits, underscores, dots, and hyphens",
)

// regexpStringValidator builds a validator.String that enforces a regexp match.
// The framework's built-in stringvalidator.RegexMatches would be preferable but
// keeping this helper avoids an extra dependency module import.
func regexpStringValidator(re *regexp.Regexp, msg string) validator.String {
	return &regexpValidator{re: re, msg: msg}
}

type regexpValidator struct {
	re  *regexp.Regexp
	msg string
}

func (v *regexpValidator) Description(_ context.Context) string {
	return "value " + v.msg
}

func (v *regexpValidator) MarkdownDescription(_ context.Context) string {
	return "value " + v.msg
}

func (v *regexpValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	val := req.ConfigValue.ValueString()
	if !v.re.MatchString(val) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid value",
			"Value "+v.msg+", got: "+val,
		)
	}
}

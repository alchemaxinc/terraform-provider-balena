package gen

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ConfigVars holds the variable-name rules balena-cloud publishes at GET /config/vars.
type ConfigVars struct {
	ReservedNames         []string `json:"reservedNames"`
	ReservedNamespaces    []string `json:"reservedNamespaces"`
	InvalidRegex          string   `json:"invalidRegex"`
	ConfigVarInvalidRegex string   `json:"configVarInvalidRegex"`
	BlackListedNames      []string `json:"blackListedNames"`
}

// JS_REGEX_RE matches a JavaScript regular expression literal without flags.
var JS_REGEX_RE = regexp.MustCompile(`^/(.+)/$`)

func parseConfigVars(data []byte) (*ConfigVars, error) {
	var cv ConfigVars
	if err := json.Unmarshal(data, &cv); err != nil {
		return nil, fmt.Errorf("config vars: %w", err)
	}
	for name, re := range map[string]*string{"invalidRegex": &cv.InvalidRegex, "configVarInvalidRegex": &cv.ConfigVarInvalidRegex} {
		m := JS_REGEX_RE.FindStringSubmatch(*re)
		if m == nil {
			return nil, fmt.Errorf("config vars: %s %q is not a flagless JavaScript regex literal", name, *re)
		}
		if _, err := regexp.Compile(m[1]); err != nil {
			return nil, fmt.Errorf("config vars: %s: %w", name, err)
		}
		*re = m[1]
	}
	if len(cv.ReservedNamespaces) == 0 {
		return nil, fmt.Errorf("config vars: no reserved namespaces")
	}
	for _, list := range [][]string{cv.ReservedNames, cv.ReservedNamespaces, cv.BlackListedNames} {
		sort.Strings(list)
	}
	return &cv, nil
}

// VarNameRule is a variable-name validator derived from /config/vars.
type VarNameRule struct {
	// Invalid is a regular expression a valid name must not match.
	Invalid string `json:"invalid"`
	// Reserved are names a valid name must not equal.
	Reserved []string `json:"reserved,omitempty"`
	// RequiredPrefixes, when set, are prefixes a valid name must start with one of.
	RequiredPrefixes []string `json:"required_prefixes,omitempty"`
	// ForbiddenPrefixes are prefixes a valid name must not start with.
	ForbiddenPrefixes []string `json:"forbidden_prefixes,omitempty"`
}

// Describe renders the rule for documentation.
func (r *VarNameRule) Describe() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("must not match `%s`", r.Invalid))
	if len(r.Reserved) > 0 {
		parts = append(parts, "must not be "+strings.Join(quoteAll(r.Reserved), ", "))
	}
	if len(r.RequiredPrefixes) > 0 {
		parts = append(parts, "must start with "+strings.Join(quoteAll(r.RequiredPrefixes), " or "))
	}
	if len(r.ForbiddenPrefixes) > 0 {
		parts = append(parts, "must not start with "+strings.Join(quoteAll(r.ForbiddenPrefixes), " or "))
	}
	return strings.Join(parts, "; ")
}

func quoteAll(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = "`" + s + "`"
	}
	return out
}

// EnvVarRule is the rule open-balena-api applies to environment variable names (checkEnvVarNameValidity).
func (cv *ConfigVars) EnvVarRule() *VarNameRule {
	return &VarNameRule{Invalid: cv.InvalidRegex, Reserved: cv.ReservedNames, ForbiddenPrefixes: cv.ReservedNamespaces}
}

// ConfigVarRule is the rule open-balena-api applies to configuration variable names (checkConfigVarNameValidity).
func (cv *ConfigVars) ConfigVarRule() *VarNameRule {
	return &VarNameRule{Invalid: cv.ConfigVarInvalidRegex, Reserved: cv.ReservedNames, RequiredPrefixes: cv.ReservedNamespaces}
}

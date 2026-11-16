package gen

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	USER_ROLE_BLOCK_RE  = regexp.MustCompile(`(?s)'named-user-api-key': \[(.*?)\n\t\],`)
	USER_EXTRA_BLOCK_RE = regexp.MustCompile(`(?s)DEFAULT_USER_EXTRA_PERMISSIONS = \[(.*?)\n\];`)
	PERMISSION_RE       = regexp.MustCompile("[`'\"]resin\\.([a-z_]+)\\.([a-z0-9-]+)")
)

// readUserPermissions extracts, from open-balena-api src/lib/auth.ts, the verbs that a named user API key (and a
// logged-in user) holds per resin table. It is informational for balena-cloud, whose permissions are not public, and
// it decides whether a table that only the unversioned model has is internal.
func readUserPermissions(src string) (map[string][]string, error) {
	role := USER_ROLE_BLOCK_RE.FindStringSubmatch(src)
	extra := USER_EXTRA_BLOCK_RE.FindStringSubmatch(src)
	if role == nil || extra == nil {
		return nil, fmt.Errorf("auth.ts: user permission blocks not found")
	}
	verbs := map[string]map[string]bool{}
	for _, block := range []string{role[1], extra[1]} {
		for _, m := range PERMISSION_RE.FindAllStringSubmatch(block, -1) {
			if verbs[m[1]] == nil {
				verbs[m[1]] = map[string]bool{}
			}
			verbs[m[1]][m[2]] = true
		}
	}
	if len(verbs) == 0 {
		return nil, fmt.Errorf("auth.ts: no resin permissions found")
	}
	out := map[string][]string{}
	for table, vs := range verbs {
		var list []string
		for v := range vs {
			list = append(list, v)
		}
		sort.Strings(list)
		out[table] = list
	}
	return out, nil
}

// canCreate reports whether the verbs include create.
func canCreate(verbs []string) bool {
	for _, v := range verbs {
		if v == "all" || v == "create" {
			return true
		}
	}
	return false
}

// verbsString renders verbs for reports.
func verbsString(verbs []string) string {
	if len(verbs) == 0 {
		return "none"
	}
	return strings.Join(verbs, ",")
}

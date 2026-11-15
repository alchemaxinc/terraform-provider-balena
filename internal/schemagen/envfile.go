package schemagen

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// PLACEHOLDER_ENV_ANCHOR marks the environment block of upstream's test compose file.
const PLACEHOLDER_ENV_ANCHOR = "environment: &env"

// PLACEHOLDER_ENV_SKIP are test-runner switches that must not reach the extraction run.
var PLACEHOLDER_ENV_SKIP = map[string]bool{
	"DEBUG":               true,
	"GENERATE_CONFIG":     true,
	"NODE_EXTRA_CA_CERTS": true,
	"NUM_WORKERS":         true,
	"RUN_TESTS":           true,
	"TEST_FILES":          true,
	"TEST_VERSIONS":       true,
}

// PlaceholderEnv builds a docker --env-file from the environment block of
// upstream's docker-compose.test-custom.yml. Upstream's public test values are
// placeholders: the extraction run reads them at import time but never uses
// them to connect anywhere. Some variable names are built at runtime, so the
// whole block is taken rather than a list scanned from the source.
func PlaceholderEnv(compose []byte) ([]byte, error) {
	sc := bufio.NewScanner(bytes.NewReader(compose))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	vars := map[string]string{}
	inBlock, blockIndent := false, 0
	for sc.Scan() {
		line := sc.Text()
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if trimmed == PLACEHOLDER_ENV_ANCHOR {
				inBlock, blockIndent = true, indent
			}
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if indent <= blockIndent {
			break
		}
		name, value, ok := strings.Cut(trimmed, ":")
		if !ok || !isEnvName(name) {
			return nil, fmt.Errorf("unexpected line in environment block: %q", trimmed)
		}
		if PLACEHOLDER_ENV_SKIP[name] {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if strings.ContainsAny(value, "\n\r") {
			return nil, fmt.Errorf("%s: multi-line value", name)
		}
		vars[name] = value
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !inBlock || len(vars) == 0 {
		return nil, fmt.Errorf("no %q block found", PLACEHOLDER_ENV_ANCHOR)
	}
	vars["NUM_WORKERS"] = "1"

	names := make([]string, 0, len(vars))
	for n := range vars {
		names = append(names, n)
	}
	sort.Strings(names)
	var out bytes.Buffer
	for _, n := range names {
		fmt.Fprintf(&out, "%s=%s\n", n, vars[n])
	}
	return out.Bytes(), nil
}

func isEnvName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		upper := c >= 'A' && c <= 'Z'
		digit := c >= '0' && c <= '9'
		if !upper && !digit && c != '_' {
			return false
		}
	}
	return true
}

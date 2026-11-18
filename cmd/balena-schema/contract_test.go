package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestContractJSONIsPure checks that `contract -json` prints the JSON document alone, whether the checks pass or fail.
func TestContractJSONIsPure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if fail && r.URL.Path == "/v7/application" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"d":[]}`))
		}))
		t.Setenv("BALENA_API_TOKEN", "test-token")
		var stdout, stderr bytes.Buffer
		code := run([]string{"contract", "-repo", "../..", "-api-url", srv.URL, "-json"}, &stdout, &stderr)
		srv.Close()
		if (code != 0) != fail {
			t.Errorf("fail=%v: exit %d, stderr %s", fail, code, stderr.String())
		}
		var doc struct {
			Checks []map[string]any `json:"checks"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil || len(doc.Checks) == 0 {
			t.Errorf("fail=%v: stdout is not one JSON document with checks (%v):\n%s", fail, err, stdout.String())
		}
		if strings.Contains(stdout.String(), "test-token") || strings.Contains(stderr.String(), "test-token") {
			t.Errorf("fail=%v: the token is printed", fail)
		}
	}
}

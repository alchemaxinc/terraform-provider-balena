package gen

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
)

// TestContractNeedsTheRowsArray checks that a 200 without Pine.js's "d" array of rows fails a probe.
func TestContractNeedsTheRowsArray(t *testing.T) {
	for body, want := range map[string]string{
		`{"d":[]}`:            CONTRACT_OK,
		`{"d":[{"id":1}]}`:    CONTRACT_OK,
		`{"d":[{"other":1}]}`: CONTRACT_FAIL,
		`{}`:                  CONTRACT_FAIL,
		`{"value":[]}`:        CONTRACT_FAIL,
		`{"d":null}`:          CONTRACT_FAIL,
		`{"d":{}}`:            CONTRACT_FAIL,
		`[]`:                  CONTRACT_FAIL,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		checks := []*ContractCheck{{What: "test", Root: "v7", Table: "application", Select: []string{"id"}}}
		RunContract(context.Background(), balena.NewClient(srv.URL, "t", "test", balena.WithMaxRetries(0)), checks)
		srv.Close()
		if checks[0].Status != want {
			t.Errorf("%s: status %s (%s), want %s", body, checks[0].Status, checks[0].Detail, want)
		}
	}
}

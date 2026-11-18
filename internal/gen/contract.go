package gen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/alchemaxinc/terraform-provider-balena/internal/balena"
)

// Contract result statuses.
const (
	CONTRACT_OK   = "ok"
	CONTRACT_FAIL = "fail"
)

// ContractCheck is one read-only probe of the live API.
type ContractCheck struct {
	// What names the spec or hook the probe covers.
	What   string   `json:"what"`
	Root   string   `json:"root"`
	Table  string   `json:"table"`
	Select []string `json:"select"`
	Expand []string `json:"expand,omitempty"`
	Status string   `json:"status"`
	Detail string   `json:"detail"`
}

// ContractChecks lists the probes for the specs: every table with every column a spec reads, every reference exposed
// by a column of the referenced row, and every column a hook declares.
func ContractChecks(ir *IR, specs []*Spec, o *Overrides) []*ContractCheck {
	var checks []*ContractCheck
	for _, s := range specs {
		c := &ContractCheck{What: s.TypeName, Root: s.Root, Table: s.Table}
		seen := map[string]bool{}
		for _, a := range s.Attributes {
			if !seen[a.Field] {
				seen[a.Field] = true
				c.Select = append(c.Select, a.Field)
			}
			if a.Encoding == ENC_REF_BY {
				c.Expand = append(c.Expand, a.Field+"($select="+a.RefField+")")
			}
		}
		checks = append(checks, c)
	}
	for _, name := range sortedKeys(o.Hooks) {
		byTable := map[string][]string{}
		for _, use := range o.Hooks[name].Uses {
			table, field, _ := strings.Cut(use, ".")
			byTable[table] = append(byTable[table], field)
		}
		for _, table := range sortedKeys(byTable) {
			fields := byTable[table]
			sort.Strings(fields)
			root := ir.DefaultRoot
			if t := ir.Table(table); t != nil {
				root = t.Root
			}
			checks = append(checks, &ContractCheck{What: "hook " + name, Root: root, Table: table, Select: fields})
		}
	}
	return checks
}

// AllTableChecks lists a probe for every table that is not excluded, with every IR column, for reports on tables
// outside the scope.
func AllTableChecks(ir *IR) []*ContractCheck {
	var checks []*ContractCheck
	for _, t := range ir.Tables {
		if t.Class == nil || t.Class.Status == STATUS_EXCLUDED {
			continue
		}
		c := &ContractCheck{What: t.Class.TypeName + " (" + t.Class.Status + ")", Root: t.Root, Table: t.Name}
		for _, f := range t.Fields {
			c.Select = append(c.Select, f.Name)
		}
		checks = append(checks, c)
	}
	return checks
}

// RunContract runs the probes with GET requests only. Pine.js answers 401 to a $select of an unknown column, so a
// 200 proves the table and every selected column exist; when a row comes back, every selected column must be in it.
func RunContract(ctx context.Context, c *balena.Client, checks []*ContractCheck) {
	for _, chk := range checks {
		params := []string{"$top=1", "$select=" + strings.Join(chk.Select, ",")}
		if len(chk.Expand) > 0 {
			params = append(params, "$expand="+strings.Join(chk.Expand, ","))
		}
		data, err := c.Do(ctx, http.MethodGet, "/"+chk.Root+"/"+chk.Table+"?"+strings.Join(params, "&"), nil)
		if err != nil {
			chk.Status = CONTRACT_FAIL
			chk.Detail = err.Error()
			if len(chk.Detail) > 300 {
				chk.Detail = chk.Detail[:300]
			}
			continue
		}
		// Pine.js wraps rows in a "d" array; anything else is not the API this contract describes.
		var resp struct {
			D *[]map[string]json.RawMessage `json:"d"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			chk.Status = CONTRACT_FAIL
			chk.Detail = "undecodable response: " + err.Error()
			continue
		}
		if resp.D == nil {
			chk.Status = CONTRACT_FAIL
			chk.Detail = `response lacks the "d" array of rows`
			continue
		}
		rows := *resp.D
		chk.Status = CONTRACT_OK
		if len(rows) == 0 {
			chk.Detail = "200, no rows visible; the selected columns exist"
			continue
		}
		var missing []string
		for _, f := range chk.Select {
			if _, ok := rows[0][f]; !ok {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			chk.Status = CONTRACT_FAIL
			chk.Detail = "row lacks " + strings.Join(missing, ", ")
			continue
		}
		chk.Detail = "200, every selected column present in a row"
	}
}

// ContractFailures returns one line per failed probe.
func ContractFailures(checks []*ContractCheck) []string {
	var out []string
	for _, c := range checks {
		if c.Status != CONTRACT_OK {
			out = append(out, fmt.Sprintf("%s: GET /%s/%s: %s", c.What, c.Root, c.Table, c.Detail))
		}
	}
	return out
}

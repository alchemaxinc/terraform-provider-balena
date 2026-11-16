package gen

import (
	"fmt"
	"strings"

	"github.com/alchemaxinc/terraform-provider-balena/internal/gen/sources"
)

// Classification statuses.
const (
	// STATUS_GENERATED marks an in-scope table the generator emits specs for.
	STATUS_GENERATED = "generated"
	// STATUS_HANDWRITTEN marks an in-scope table whose resource relies on hand-written hooks for a CRUD step.
	STATUS_HANDWRITTEN = "handwritten"
	// STATUS_EXCLUDED marks a table the rules exclude, with a reason.
	STATUS_EXCLUDED = "excluded"
	// STATUS_UNSCOPED marks a table outside the scope list, with the classification the rules would give it.
	STATUS_UNSCOPED = "unscoped"
)

// Classification is what the generator decided for a table.
type Classification struct {
	Status string `json:"status"`
	// Resource and DataSource tell which Terraform types the table gets (or would get, when unscoped).
	Resource   bool `json:"resource"`
	DataSource bool `json:"data_source"`
	// Hooks lists named Go hooks that replace CRUD steps of the resource.
	Hooks []string `json:"hooks,omitempty"`
	// TypeName is the Terraform type name.
	TypeName string `json:"type_name"`
	// Preview marks a table served from a root that may change without notice.
	Preview bool   `json:"preview,omitempty"`
	Reason  string `json:"reason"`
}

// PAYMENT_TOKENS are table-name words that mark payment and billing tables, which the provider never manages.
var PAYMENT_TOKENS = map[string]bool{
	"addon": true, "billing": true, "credit": true, "discount": true, "feature": true, "invoice": true,
	"plan": true, "subscription": true, "support": true,
}

// PAYMENT_FIELD_TOKENS are column-name words that mark payment and billing columns of otherwise managed tables.
var PAYMENT_FIELD_TOKENS = map[string]bool{"billing": true, "credit": true, "discount": true, "invoice": true, "subscription": true}

// isPaymentField reports whether a column name contains a payment token.
func isPaymentField(field string) bool {
	for _, tok := range strings.FieldsFunc(field, func(r rune) bool { return r == '_' }) {
		if PAYMENT_FIELD_TOKENS[tok] {
			return true
		}
	}
	return false
}

// TypeName returns the Terraform type name of a table: the provider prefix and the API resource name, with the
// double underscores of link-table names collapsed.
func TypeName(table string) string {
	return "balena_" + strings.ReplaceAll(table, "__", "_")
}

// isPayment reports whether a table name contains a payment token.
func isPayment(table string) bool {
	for _, tok := range strings.FieldsFunc(table, func(r rune) bool { return r == '_' }) {
		if PAYMENT_TOKENS[tok] {
			return true
		}
	}
	return false
}

// hasDataColumns reports whether a table has any column besides id and timestamps.
func hasDataColumns(t *Table) bool {
	for _, f := range t.Fields {
		if !AUTO_FIELDS[f.Name] {
			return true
		}
	}
	return false
}

// exclusionReason applies the exclusion rules: payment tables, tables without data columns, and internal tables that
// only the unversioned model has and that open-balena-api grants users no access to.
func exclusionReason(t *Table) string {
	switch {
	case isPayment(t.Name):
		return "payment or billing table"
	case !hasDataColumns(t):
		return "no columns besides its id"
	case len(t.Roots) == 1 && t.Roots[0] == sources.ROOT_UNVERSIONED && len(t.OpenBalenaPermissions) == 0:
		return "internal: only the unversioned model has it and open-balena-api grants users no access"
	}
	return ""
}

// Classify sets the Class of every table. It fails when a scoped table is excluded by the rules or does not exist.
func Classify(ir *IR, o *Overrides, previewRoots map[string]bool) error {
	scope := map[string]bool{}
	for _, s := range o.Scope {
		if ir.Table(s) == nil {
			return fmt.Errorf("%s: scope lists %s, which no locked root has", OVERRIDES_PATH, s)
		}
		scope[s] = true
	}
	types := map[string]string{}
	for _, t := range ir.Tables {
		c := &Classification{TypeName: TypeName(t.Name), Preview: previewRoots[t.Root]}
		if other, dup := types[c.TypeName]; dup {
			return fmt.Errorf("tables %s and %s both map to %s", other, t.Name, c.TypeName)
		}
		types[c.TypeName] = t.Name
		t.Class = c
		if reason := exclusionReason(t); reason != "" {
			if scope[t.Name] {
				return fmt.Errorf("%s: scope lists %s, which the rules exclude (%s)", OVERRIDES_PATH, t.Name, reason)
			}
			c.Status = STATUS_EXCLUDED
			c.Reason = reason
			continue
		}
		c.DataSource = true
		c.Resource = t.Writable
		var reasons []string
		switch {
		case !t.Writable:
			reasons = append(reasons, "no user-writable columns: data source only")
		case !contains(t.Roots, ir.DefaultRoot) && !canCreate(t.OpenBalenaPermissions):
			// Only the open model describes the table, so open-balena-api's permissions are the only source on whether
			// users may create rows.
			c.Resource = false
			reasons = append(reasons, "data source only: open-balena-api grants users "+verbsString(t.OpenBalenaPermissions)+" but not create")
		default:
			reasons = append(reasons, "writable columns: resource and data source")
		}
		if to := o.Tables[t.Name]; to != nil && to.Resource != nil {
			if *to.Resource == c.Resource {
				return fmt.Errorf("%s: tables.%s.resource is a no-op", OVERRIDES_PATH, t.Name)
			}
			c.Resource = *to.Resource
			reasons = []string{"data source only (override: " + to.Reason + ")"}
		}
		if !scope[t.Name] {
			c.Status = STATUS_UNSCOPED
			c.Reason = "proposed: " + strings.Join(reasons, "; ")
			if t.Provenance == PROVENANCE_CLOUD {
				c.Reason += "; cloud-only"
			}
			continue
		}
		c.Status = STATUS_GENERATED
		if to := o.Tables[t.Name]; to != nil && len(to.Hooks) > 0 {
			c.Status = STATUS_HANDWRITTEN
			for _, step := range sortedKeys(to.Hooks) {
				c.Hooks = append(c.Hooks, step+"="+to.Hooks[step])
			}
			reasons = append(reasons, "hand-written hooks: "+to.Reason)
		}
		if c.Preview {
			reasons = append(reasons, "preview: served from the unversioned /"+t.Root+" root")
		}
		c.Reason = strings.Join(reasons, "; ")
	}
	for name := range o.Tables {
		if !scope[name] {
			return fmt.Errorf("%s: tables.%s overrides a table outside the scope", OVERRIDES_PATH, name)
		}
	}
	return nil
}

// GapCheck fails when any table lacks a classification or a reason.
func GapCheck(ir *IR) []string {
	var problems []string
	for _, t := range ir.Tables {
		c := t.Class
		switch {
		case c == nil:
			problems = append(problems, "gap: "+t.Name+" is not classified")
		case c.Reason == "":
			problems = append(problems, "gap: "+t.Name+" has no reason")
		case c.Status != STATUS_GENERATED && c.Status != STATUS_HANDWRITTEN && c.Status != STATUS_EXCLUDED && c.Status != STATUS_UNSCOPED:
			problems = append(problems, "gap: "+t.Name+" has unknown status "+c.Status)
		case c.Status != STATUS_EXCLUDED && !c.Resource && !c.DataSource:
			problems = append(problems, "gap: "+t.Name+" gets neither a resource nor a data source")
		}
	}
	return problems
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

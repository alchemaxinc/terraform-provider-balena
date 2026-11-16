package gen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alchemaxinc/terraform-provider-balena/internal/gen/sbvr"
	"github.com/alchemaxinc/terraform-provider-balena/internal/gen/sources"
	"github.com/alchemaxinc/terraform-provider-balena/internal/gen/tsmodel"
)

// crossCheck compares a root's TypeScript model with the root's own SBVR file. Every SBVR table and column must be in
// the TypeScript model with the same type, nullability and reference target. Every TypeScript column must come from the
// SBVR, from the known code-level additions, or, for a balena-cloud root, be marked cloud-only. It returns one line per
// unexplained difference.
func crossCheck(rootName string, model *tsmodel.Model, own *sbvr.Model, tables map[string]*Table) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("cross-check %s: ", rootName)+fmt.Sprintf(format, args...))
	}
	cloudRoot := rootName != sources.ROOT_UNVERSIONED
	byCanonical := map[string]*Table{}
	for _, t := range tables {
		byCanonical[t.Canonical] = t
	}
	covered := map[string]bool{}
	for _, st := range own.SortedTables() {
		t := tables[st.Name]
		if t == nil {
			t = byCanonical[st.Canonical]
		}
		if t == nil {
			if cloudRoot && KNOWN_CLOUD_OMISSIONS[st.Canonical] != "" {
				continue
			}
			add("SBVR table %s (%s) is missing from the TypeScript model", st.Name, st.Canonical)
			continue
		}
		covered[t.Name] = true
		seen := map[string]bool{}
		for _, sf := range st.Fields {
			col := renamed(t.Name, sf.Name)
			seen[col] = true
			f := t.Field(col)
			if f == nil {
				if cloudRoot && KNOWN_CLOUD_OMISSIONS[t.Name+"."+col] != "" {
					continue
				}
				add("%s.%s is in the SBVR but not in the TypeScript model", t.Name, col)
				continue
			}
			sfType := sf.Type
			if changed, ok := KNOWN_TYPE_CHANGES[t.Name+"."+col]; ok {
				sfType = changed
			}
			switch {
			case sf.Target != "":
				if f.Kind != KIND_FK {
					add("%s.%s is a reference in the SBVR but a %s in the TypeScript model", t.Name, col, f.Kind)
				} else if sf.Target != f.Target && sbvrTableName(own, f.Target) != sf.Target {
					add("%s.%s references %s in the SBVR but %s in the TypeScript model", t.Name, col, sf.Target, f.Target)
				}
			case f.Kind == KIND_FK:
				add("%s.%s is a %s column in the SBVR but a reference in the TypeScript model", t.Name, col, sfType)
			case sfType != f.Type && !sameRepresentation(sfType, f.Type):
				add("%s.%s has type %s in the SBVR but %s in the TypeScript model", t.Name, col, sfType, f.Type)
			}
			if !f.Computed && sf.Required == f.Nullable {
				add("%s.%s is required=%v in the SBVR but nullable=%v in the TypeScript model", t.Name, col, sf.Required, f.Nullable)
			}
			if len(sf.Enum) > 0 && f.Kind == KIND_ENUM && !sameSet(sf.Enum, f.Enum) {
				add("%s.%s enum %v in the SBVR differs from %v in the TypeScript model", t.Name, col, sf.Enum, f.Enum)
			}
			if len(sf.Enum) > 0 && f.Kind != KIND_ENUM && !cloudRoot {
				add("%s.%s is an enum in the SBVR but not in the TypeScript model", t.Name, col)
			}
		}
		for _, f := range t.Fields {
			if seen[f.Name] || AUTO_FIELDS[f.Name] || f.Computed {
				continue
			}
			if cloudRoot && f.Provenance == PROVENANCE_CLOUD {
				continue
			}
			add("%s.%s is in the TypeScript model but not in the SBVR, and is not a known computed field", t.Name, f.Name)
		}
	}
	names := make([]string, 0, len(tables))
	for n := range tables {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t := tables[n]
		if covered[n] || KNOWN_VIEWS[t.Canonical] {
			continue
		}
		if cloudRoot && t.Provenance == PROVENANCE_CLOUD {
			continue
		}
		add("table %s is in the TypeScript model but not in the SBVR, and is not a known view", n)
	}
	return problems
}

// sbvrTableName maps a model resource name to the SBVR table name (they differ for implicit link tables).
func sbvrTableName(m *sbvr.Model, resource string) string {
	if t, ok := m.Tables[resource]; ok {
		return t.Name
	}
	for _, t := range m.Tables {
		if t.Canonical == resource {
			return t.Name
		}
	}
	return ""
}

// sameRepresentation accepts the type differences the TypeScript generator introduces: serial ids are integers, and
// the cloud typings type a serial as Integer.
func sameRepresentation(sbvrType, tsType string) bool {
	equiv := map[string]string{"Serial": "Integer", "Big Serial": "Big Integer"}
	if e, ok := equiv[sbvrType]; ok {
		sbvrType = e
	}
	if e, ok := equiv[tsType]; ok {
		tsType = e
	}
	return sbvrType == tsType
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, "\x00") == strings.Join(y, "\x00")
}

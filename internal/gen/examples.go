package gen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Example directories the generator owns: every balena_* directory below them is generated.
const (
	EXAMPLES_RESOURCES    = "examples/resources"
	EXAMPLES_DATA_SOURCES = "examples/data-sources"
)

// EXAMPLE_REF_VALUES are example values for references exposed by a column of the referenced row.
var EXAMPLE_REF_VALUES = map[string]string{
	"slug":        "raspberrypi4-64",
	"commit":      "f2a97d4338b09ef613a17961f4fafbc2",
	"raw_version": "6.0.10+rev1",
}

// exampleString returns an example value that passes the attribute's validators.
func exampleString(a *Attr) string {
	switch {
	case a.Encoding == ENC_REF_BY && EXAMPLE_REF_VALUES[a.RefField] != "":
		return EXAMPLE_REF_VALUES[a.RefField]
	case a.VarName != nil && len(a.VarName.RequiredPrefixes) > 0:
		return a.VarName.RequiredPrefixes[0] + "HOST_CONFIG_gpu_mem"
	case a.VarName != nil:
		return "MQTT_HOST"
	case len(a.OneOf) > 0:
		return a.OneOf[0]
	case a.Field == "uuid":
		return "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
	}
	v := "example-" + strings.ReplaceAll(a.Name, "_", "-")
	if a.MinLength > 0 && a.MinLength == a.MaxLength {
		return strings.Repeat("0", a.MinLength)
	}
	if a.MaxLength > 0 && len(v) > a.MaxLength {
		v = v[:a.MaxLength]
	}
	for len(v) < a.MinLength {
		v += "x"
	}
	return v
}

func exampleValue(a *Attr) string {
	switch a.Type {
	case TYPE_INT64:
		return "123456"
	case TYPE_BOOL:
		return "true"
	case TYPE_FLOAT64:
		return "1.5"
	}
	return strconv.Quote(exampleString(a))
}

func renderBlock(kind, typ, name string, attrs []*Attr) []byte {
	width := 0
	for _, a := range attrs {
		if len(a.Name) > width {
			width = len(a.Name)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %q %q {\n", kind, typ, name)
	for _, a := range attrs {
		fmt.Fprintf(&b, "  %-*s = %s\n", width, a.Name, exampleValue(a))
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

// renderExamples renders one example per resource (required attributes, plus references given by a column) and per
// data source (selected by its first unique column, or by id).
func renderExamples(specs []*Spec) map[string][]byte {
	out := map[string][]byte{}
	for _, s := range specs {
		if s.Resource {
			var attrs []*Attr
			for _, a := range s.Attributes {
				if a.ResourceMode == MODE_REQUIRED || (a.Encoding == ENC_REF_BY && a.Writable()) {
					attrs = append(attrs, a)
				}
			}
			out[EXAMPLES_RESOURCES+"/"+s.TypeName+"/resource.tf"] = renderBlock("resource", s.TypeName, "example", attrs)
			importID := "123456"
			if s.ImportBy != "" {
				for _, a := range s.Attributes {
					if a.Name == s.ImportBy {
						importID = exampleString(a)
					}
				}
			}
			out[EXAMPLES_RESOURCES+"/"+s.TypeName+"/import.sh"] = []byte("terraform import " + s.TypeName + ".example " + importID + "\n")
		}
		if s.DataSource {
			selector := s.Attributes[0]
			for _, a := range s.Attributes {
				if a.field != nil && a.field.Unique && a.Name != "id" && a.DataSourceMode == DS_FILTER && a.Type == TYPE_STRING {
					selector = a
					break
				}
			}
			out[EXAMPLES_DATA_SOURCES+"/"+s.TypeName+"/data-source.tf"] = renderBlock("data", s.TypeName, "example", []*Attr{selector})
		}
	}
	return out
}

// orphanExamples lists generated example directories that no spec produces any more.
func orphanExamples(repoDir string, files map[string][]byte) []string {
	var orphans []string
	for _, dir := range []string{EXAMPLES_RESOURCES, EXAMPLES_DATA_SOURCES} {
		entries, err := os.ReadDir(filepath.Join(repoDir, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "balena_") {
				continue
			}
			sub := filepath.Join(repoDir, dir, e.Name())
			_ = filepath.WalkDir(sub, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(repoDir, p)
				if _, ok := files[filepath.ToSlash(rel)]; !ok {
					orphans = append(orphans, filepath.ToSlash(rel))
				}
				return nil
			})
		}
	}
	sort.Strings(orphans)
	return orphans
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"slices"
	"strings"
	"text/template"
)

type model struct {
	Tables   map[string]table
	Synonyms map[string]string
}

type table struct {
	IDField        string
	Fields         []field
	WritableFields []string
}

type field struct {
	FieldName  string
	DataType   string
	Required   bool
	Computed   *json.RawMessage
	References *struct {
		ResourceName string
		FieldName    string
	}
}

type binding struct {
	GoName string
	GoType string
	Tag    string
}

type envVarSpec struct {
	Resource string
	Type     string
	Parent   string
	Relation string
}

var (
	wireNames   = strings.NewReplacer("-", "__", " ", "_")
	envVarSpecs = []envVarSpec{
		{Resource: "application environment variable", Type: "ApplicationEnvVar", Parent: "application", Relation: "App"},
	}
)

func generate(data []byte, version string) ([]byte, error) {
	var config struct {
		Models []struct {
			APIRoot     string
			AbstractSQL model
		}
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("decoding compiled Pine model: %w", err)
	}
	for _, entry := range config.Models {
		if entry.APIRoot == version {
			return generateModel(entry.AbstractSQL, version)
		}
	}
	return nil, fmt.Errorf("compiled Pine model is missing API root %q", version)
}

func generateModel(m model, version string) ([]byte, error) {
	type envVar struct {
		Type         string
		Path         string
		PathConst    string
		ParentField  string
		ReadFields   []binding
		CreateFields []binding
		UpdateField  binding
	}

	resources := make([]envVar, 0, len(envVarSpecs))
	for _, spec := range envVarSpecs {
		t, ok := m.lookup(spec.Resource)
		if !ok {
			return nil, fmt.Errorf("%s: missing resource %q", version, spec.Resource)
		}
		wireName := wireNames.Replace(spec.Resource)
		resource := envVar{
			Type: spec.Type, Path: "/" + version + "/" + wireName,
			PathConst:   strings.ToUpper(wireName) + "_PATH",
			ParentField: wireNames.Replace(spec.Parent),
		}
		for _, column := range []struct {
			name, goName, goType string
			write                bool
		}{
			{"id", "ID", "int64", false},
			{spec.Parent, spec.Relation, "ODataRef", true},
			{"name", "Name", "string", true},
			{"value", "Value", "string", true},
		} {
			read, err := m.bind(t, column.name, column.goName, column.goType, false)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", version, spec.Resource, err)
			}
			resource.ReadFields = append(resource.ReadFields, read)
			if column.write {
				writeType := column.goType
				if writeType == "ODataRef" {
					writeType = "int64"
				}
				write, err := m.bind(t, column.name, column.goName, writeType, true)
				if err != nil {
					return nil, fmt.Errorf("%s/%s: %w", version, spec.Resource, err)
				}
				resource.CreateFields = append(resource.CreateFields, write)
				if column.name == "value" {
					resource.UpdateField = write
				}
			}
		}
		resources = append(resources, resource)
	}

	var out bytes.Buffer
	if err := clientTemplate.Execute(&out, resources); err != nil {
		return nil, fmt.Errorf("rendering Pine client: %w", err)
	}
	return format.Source(out.Bytes())
}

func (m model) lookup(name string) (table, bool) {
	if canonical, ok := m.Synonyms[name]; ok {
		name = canonical
	}
	t, ok := m.Tables[name]
	return t, ok
}

func (m model) bind(t table, name, goName, wantType string, write bool) (binding, error) {
	for _, f := range t.Fields {
		if f.FieldName != name || (write && (f.Computed != nil || !slices.Contains(t.WritableFields, name))) {
			continue
		}
		if !f.Required || (goName == "ID" && t.IDField != name) {
			return binding{}, fmt.Errorf("field %q no longer matches the required/key contract", name)
		}
		goType, err := m.fieldType(f, write)
		if err != nil {
			return binding{}, fmt.Errorf("field %q: %w", name, err)
		}
		if goType != wantType {
			return binding{}, fmt.Errorf("field %q has type %s, expected %s", name, goType, wantType)
		}
		return binding{GoName: goName, GoType: goType, Tag: fmt.Sprintf("`json:%q`", wireNames.Replace(f.FieldName))}, nil
	}
	if write {
		return binding{}, fmt.Errorf("field %q is missing or not writable", name)
	}
	return binding{}, fmt.Errorf("missing readable field %q", name)
}

func (m model) fieldType(f field, write bool) (string, error) {
	switch f.DataType {
	case "ForeignKey", "ConceptType":
		if f.References == nil {
			break
		}
		target, ok := m.lookup(f.References.ResourceName)
		if !ok || target.IDField != f.References.FieldName {
			break
		}
		for _, key := range target.Fields {
			if key.FieldName == target.IDField && key.Required && isInteger(key.DataType) {
				if write {
					return "int64", nil
				}
				return "ODataRef", nil
			}
		}
	case "Short Text", "Text":
		return "string", nil
	default:
		if isInteger(f.DataType) {
			return "int64", nil
		}
	}
	return "", fmt.Errorf("unsupported Pine type %q or reference", f.DataType)
}

func isInteger(dataType string) bool {
	return dataType == "Integer" || dataType == "Serial"
}

var clientTemplate = template.Must(template.New("client").Parse(`// Code generated by pinegen; DO NOT EDIT.

package balena

import (
    "context"
    "fmt"
)

{{ range . }}
const {{ .PathConst }} = {{ printf "%q" .Path }}

// {{ .Type }} represents a Pine environment variable.
type {{ .Type }} struct {
    {{ range .ReadFields -}}
    {{ .GoName }} {{ .GoType }} {{ .Tag }}
    {{ end -}}
}

// List{{ .Type }}s lists the environment variables for a parent.
func (c *Client) List{{ .Type }}s(ctx context.Context, parentID int64) ([]{{ .Type }}, error) {
    filter := fmt.Sprintf("$filter={{ .ParentField }} eq %d", parentID)
    return doListFiltered[{{ .Type }}](ctx, c, {{ .PathConst }}, filter)
}

// Get{{ .Type }} retrieves the environment variable by ID.
func (c *Client) Get{{ .Type }}(ctx context.Context, id int64) (*{{ .Type }}, error) {
    return doGetByID[{{ .Type }}](ctx, c, {{ .PathConst }}, id)
}

// Create{{ .Type }} creates an environment variable.
func (c *Client) Create{{ .Type }}(ctx context.Context, parentID int64, name, value string) (*{{ .Type }}, error) {
    payload := struct {
        {{ range .CreateFields -}}
        {{ .GoName }} {{ .GoType }} {{ .Tag }}
        {{ end -}}
    }{parentID, name, value}
    return doCreate[{{ .Type }}](ctx, c, {{ .PathConst }}, payload)
}

// Update{{ .Type }} updates the environment variable's value.
func (c *Client) Update{{ .Type }}(ctx context.Context, id int64, value string) error {
    payload := struct {
        {{ .UpdateField.GoName }} {{ .UpdateField.GoType }} {{ .UpdateField.Tag }}
    }{Value: value}
    return doPatch(ctx, c, {{ .PathConst }}, id, payload)
}

// Delete{{ .Type }} removes an environment variable by ID.
func (c *Client) Delete{{ .Type }}(ctx context.Context, id int64) error {
    return doDelete(ctx, c, {{ .PathConst }}, id)
}
{{ end -}}`))

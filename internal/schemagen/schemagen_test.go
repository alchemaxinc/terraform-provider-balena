//go:build !integration

package schemagen

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alchemaxinc/terraform-provider-balena/internal/engine"
)

const (
	specPath       = "../../schema/api/v6.json"
	provenancePath = "../../schema/api/provenance.json"
	versionPath    = "../../schema/VERSION"
	generatedPath  = "../generated/zz_generated.go"
)

// rawFixture is a three-model chain (v6 → v7 → resin) exercising every
// normalization rule. It is handwritten, not derived from upstream.
const rawFixture = `{
  "models": [
    {
      "apiRoot": "resin", "modelName": "base",
      "abstractSql": {
        "synonyms": {},
        "tables": {
          "thing": {"name": "thing", "idField": "id", "fields": [
            {"fieldName": "id", "dataType": "Serial", "required": true},
            {"fieldName": "name", "dataType": "Short Text", "required": true},
            {"fieldName": "kind", "dataType": "Short Text", "required": true},
            {"fieldName": "secret", "dataType": "Hashed", "required": false},
            {"fieldName": "score", "dataType": "Integer", "required": false, "computed": ["Count", "*"]},
            {"fieldName": "created at", "dataType": "Date Time", "required": true, "defaultValue": "CURRENT_TIMESTAMP"}
          ]},
          "thing-has-label": {"name": "thing label", "idField": "id", "fields": [
            {"fieldName": "id", "dataType": "Serial", "required": true},
            {"fieldName": "thing", "dataType": "ForeignKey", "required": true, "references": {"resourceName": "thing", "fieldName": "id"}},
            {"fieldName": "tag key", "dataType": "Short Text", "required": true},
            {"fieldName": "value", "dataType": "Text", "required": true}
          ]}
        }
      }
    },
    {
      "apiRoot": "v7", "modelName": "v7", "translateTo": "resin",
      "translations": {"thing": {"title": "name", "status": ["Cast", ["Null"], "Short Text"]}},
      "abstractSql": {
        "synonyms": {},
        "tables": {
          "thing": {"name": "thing", "idField": "id", "fields": [
            {"fieldName": "id", "dataType": "Serial", "required": true},
            {"fieldName": "title", "dataType": "Short Text", "required": true},
            {"fieldName": "kind", "dataType": "Short Text", "required": true},
            {"fieldName": "secret", "dataType": "Hashed", "required": false},
            {"fieldName": "score", "dataType": "Integer", "required": false},
            {"fieldName": "status", "dataType": "Short Text", "required": false},
            {"fieldName": "created at", "dataType": "Date Time", "required": true, "defaultValue": "CURRENT_TIMESTAMP"}
          ]},
          "thing-has-label": {"name": "thing label", "idField": "id", "fields": [
            {"fieldName": "id", "dataType": "Serial", "required": true},
            {"fieldName": "thing", "dataType": "ForeignKey", "required": true, "references": {"resourceName": "thing", "fieldName": "id"}},
            {"fieldName": "tag key", "dataType": "Short Text", "required": true},
            {"fieldName": "value", "dataType": "Text", "required": true}
          ]}
        }
      }
    },
    {
      "apiRoot": "v6", "modelName": "v6", "translateTo": "v7",
      "translations": {
        "thing": {"legacy": ["Cast", ["Null"], "Integer"]},
        "thing-has-tag key": {"$toResource": "thing-has-label"}
      },
      "abstractSql": {
        "synonyms": {"thing tag": "thing-has-tag key"},
        "tables": {
          "thing": {"name": "thing", "idField": "id", "fields": [
            {"fieldName": "id", "dataType": "Serial", "required": true},
            {"fieldName": "title", "dataType": "Short Text", "required": true},
            {"fieldName": "kind", "dataType": "Short Text", "required": true,
             "checks": [["In", ["Field", "kind"], ["Text", "a"], ["Text", "b"]]]},
            {"fieldName": "secret", "dataType": "Hashed", "required": false},
            {"fieldName": "score", "dataType": "Integer", "required": false},
            {"fieldName": "status", "dataType": "Short Text", "required": false},
            {"fieldName": "legacy", "dataType": "ForeignKey", "required": false, "references": {"resourceName": "thing", "fieldName": "id"}},
            {"fieldName": "blob", "dataType": "Interval", "required": false},
            {"fieldName": "created at", "dataType": "Date Time", "required": true, "defaultValue": "CURRENT_TIMESTAMP"}
          ]},
          "thing-has-tag key": {"name": "thing tag", "idField": "id", "fields": [
            {"fieldName": "id", "dataType": "Serial", "required": true},
            {"fieldName": "thing", "dataType": "ForeignKey", "required": true, "references": {"resourceName": "thing", "fieldName": "id"}},
            {"fieldName": "tag key", "dataType": "Short Text", "required": true},
            {"fieldName": "value", "dataType": "Text", "required": true, "references": {"resourceName": "value", "fieldName": null}}
          ]},
          "thing$v7": {"name": "thing", "idField": "id", "fields": []}
        }
      }
    }
  ]
}`

func TestNormalize(t *testing.T) {
	got, err := Normalize([]byte(rawFixture), "v6")
	if err != nil {
		t.Fatal(err)
	}
	want := &Spec{Format: SPEC_FORMAT, APIRoot: "v6", Resources: []Resource{
		{
			Name: "thing", Term: "thing", Key: "thing",
			Fields: []Field{
				{Name: "created_at", Term: "created at", Type: TYPE_DATE_TIME, SQLType: "Date Time", Required: true, ServerDefault: true},
				{Name: "id", Term: "id", Type: TYPE_INTEGER, SQLType: "Serial", Required: true, ServerDefault: true},
				{Name: "kind", Term: "kind", Type: TYPE_STRING, SQLType: "Short Text", Required: true, Enum: []string{"a", "b"}},
				// A computed translation two hops down still makes the field read-only.
				{Name: "legacy", Term: "legacy", Type: TYPE_FOREIGN_KEY, SQLType: "ForeignKey", ReadOnly: true, References: "thing"},
				// Computed in the base model.
				{Name: "score", Term: "score", Type: TYPE_INTEGER, SQLType: "Integer", ReadOnly: true},
				{Name: "secret", Term: "secret", Type: TYPE_HASHED, SQLType: "Hashed"},
				// Cast to null by the v7 translation.
				{Name: "status", Term: "status", Type: TYPE_STRING, SQLType: "Short Text", ReadOnly: true},
				// Renamed by the v7 translation: still writable.
				{Name: "title", Term: "title", Type: TYPE_STRING, SQLType: "Short Text", Required: true},
			},
			Unsupported: []UnsupportedField{{Name: "blob", SQLType: "Interval"}},
		},
		{
			// Served under the term, which resolves to the key through the synonyms.
			Name: "thing_tag", Term: "thing tag", Key: "thing-has-tag key",
			Fields: []Field{
				{Name: "id", Term: "id", Type: TYPE_INTEGER, SQLType: "Serial", Required: true, ServerDefault: true},
				// Writable through the table-level $toResource.
				{Name: "tag_key", Term: "tag key", Type: TYPE_STRING, SQLType: "Short Text", Required: true},
				{Name: "thing", Term: "thing", Type: TYPE_FOREIGN_KEY, SQLType: "ForeignKey", Required: true, References: "thing"},
				// A concept-type reference on a Text field is not a foreign key.
				{Name: "value", Term: "value", Type: TYPE_STRING, SQLType: "Text", Required: true},
			},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		g, _ := MarshalSpec(got)
		w, _ := MarshalSpec(want)
		t.Errorf("Normalize() =\n%s\nwant\n%s", g, w)
	}
}

func TestNormalize_Errors(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(string) string
		root   string
		want   string
	}{
		"unknown api root": {func(s string) string { return s }, "v5", `no model with apiRoot "v5"`},
		"unresolvable name": {func(s string) string {
			return strings.Replace(s, `{"thing tag": "thing-has-tag key"}`, `{"thing": "other"}`, 1)
		}, "v6", "no wire name resolves"},
		"dangling reference": {func(s string) string {
			return strings.Replace(s, `"legacy", "dataType": "ForeignKey", "required": false, "references": {"resourceName": "thing"`,
				`"legacy", "dataType": "ForeignKey", "required": false, "references": {"resourceName": "nothing"`, 1)
		}, "v6", `references unknown table "nothing"`},
		"duplicate root": {func(s string) string { return strings.Replace(s, `"apiRoot": "v7"`, `"apiRoot": "v6"`, 1) }, "v6", "duplicate apiRoot"},
		"not json":       {func(string) string { return "{" }, "v6", "decoding raw config"},
	} {
		t.Run(name, func(t *testing.T) {
			raw := tc.mutate(rawFixture)
			if raw == rawFixture && name != "unknown api root" {
				t.Fatal("mutation did not apply")
			}
			_, err := Normalize([]byte(raw), tc.root)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestNormalize_IsDeterministic(t *testing.T) {
	var outs [][]byte
	for range 5 {
		s, err := Normalize([]byte(rawFixture), "v6")
		if err != nil {
			t.Fatal(err)
		}
		b, err := MarshalSpec(s)
		if err != nil {
			t.Fatal(err)
		}
		outs = append(outs, b)
	}
	for _, b := range outs[1:] {
		if !bytes.Equal(b, outs[0]) {
			t.Fatal("Normalize output differs between runs")
		}
	}
}

func TestPlaceholderEnv(t *testing.T) {
	compose := `services:
  sut:
    image: x
    environment: &env
      API_HOST: 127.0.0.1
      # a comment
      COOKIE_SESSION_SECRET: "quoted value"
      RUN_TESTS: 1
      GENERATE_CONFIG: ${GENERATE_CONFIG:-}
      NUM_WORKERS: 8
      EMPTY:
  other:
    environment:
      NOT_THIS: 1
`
	got, err := PlaceholderEnv([]byte(compose))
	if err != nil {
		t.Fatal(err)
	}
	want := "API_HOST=127.0.0.1\nCOOKIE_SESSION_SECRET=quoted value\nEMPTY=\nNUM_WORKERS=1\n"
	if string(got) != want {
		t.Errorf("PlaceholderEnv() =\n%s\nwant\n%s", got, want)
	}
	if _, err := PlaceholderEnv([]byte("services: {}\n")); err == nil {
		t.Error("missing environment block accepted")
	}
	if _, err := PlaceholderEnv([]byte("    environment: &env\n      lower: x\n")); err == nil {
		t.Error("malformed variable name accepted")
	}
}

func TestDescriptors(t *testing.T) {
	spec, err := Normalize([]byte(rawFixture), "v6")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := Descriptors(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[0].TypeName != "thing" || ds[0].Path != "/v6/thing" || ds[1].TypeName != "thing_tag" {
		t.Fatalf("descriptors = %+v", ds)
	}
	type summary struct {
		Type       engine.AttrType
		Codec      engine.Codec
		Mode       engine.Mode
		Sensitive  bool
		WriteOnly  bool
		Filterable bool
		Enum       []string
	}
	got := map[string]summary{}
	for _, d := range ds {
		for _, a := range d.Attributes {
			if a.Name != a.Wire {
				t.Errorf("%s: name %q differs from wire %q", d.TypeName, a.Name, a.Wire)
			}
			got[d.TypeName+"."+a.Name] = summary{a.Type, a.Codec, a.Mode, a.Sensitive, a.WriteOnly, a.Filterable, a.Enum}
		}
	}
	want := map[string]summary{
		"thing.created_at":  {engine.TYPE_STRING, engine.CODEC_PLAIN, engine.MODE_OPTIONAL_COMPUTED, false, false, false, nil},
		"thing.kind":        {engine.TYPE_STRING, engine.CODEC_PLAIN, engine.MODE_REQUIRED, false, false, true, []string{"a", "b"}},
		"thing.legacy":      {engine.TYPE_INT64, engine.CODEC_FOREIGN_KEY, engine.MODE_COMPUTED, false, false, true, nil},
		"thing.score":       {engine.TYPE_INT64, engine.CODEC_PLAIN, engine.MODE_COMPUTED, false, false, true, nil},
		"thing.secret":      {engine.TYPE_STRING, engine.CODEC_PLAIN, engine.MODE_OPTIONAL, true, true, false, nil},
		"thing.status":      {engine.TYPE_STRING, engine.CODEC_PLAIN, engine.MODE_COMPUTED, false, false, true, nil},
		"thing.title":       {engine.TYPE_STRING, engine.CODEC_PLAIN, engine.MODE_REQUIRED, false, false, true, nil},
		"thing_tag.tag_key": {engine.TYPE_STRING, engine.CODEC_PLAIN, engine.MODE_REQUIRED, false, false, true, nil},
		"thing_tag.thing":   {engine.TYPE_INT64, engine.CODEC_FOREIGN_KEY, engine.MODE_REQUIRED, false, false, true, nil},
		"thing_tag.value":   {engine.TYPE_STRING, engine.CODEC_PLAIN, engine.MODE_REQUIRED, false, false, true, nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("attributes =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDescriptors_SkipsReservedNames(t *testing.T) {
	spec := &Spec{Format: SPEC_FORMAT, APIRoot: "v6", Resources: []Resource{{
		Name: "r", Term: "r", Key: "r",
		Fields: []Field{
			{Name: "id", Type: TYPE_INTEGER},
			{Name: "count", Type: TYPE_INTEGER},
			{Name: "ok", Type: TYPE_STRING},
		},
	}}}
	ds, err := Descriptors(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds[0].Attributes) != 1 || ds[0].Attributes[0].Name != "ok" {
		t.Errorf("attributes = %+v, want only ok", ds[0].Attributes)
	}
}

// TestCommittedSpecIsCanonical guards against hand edits of the spec file.
func TestCommittedSpecIsCanonical(t *testing.T) {
	spec, err := LoadSpec(specPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := MarshalSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is not in canonical form: regenerate it with make api-spec", specPath)
	}
}

// TestGeneratedCodeIsCurrent fails when the committed generated code does not
// match the committed spec.
func TestGeneratedCodeIsCurrent(t *testing.T) {
	spec, err := LoadSpec(specPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is stale: run make generate", generatedPath)
	}
}

func TestProvenanceMatchesVersion(t *testing.T) {
	data, err := os.ReadFile(provenancePath)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	version, err := os.ReadFile(versionPath)
	if err != nil {
		t.Fatal(err)
	}
	if v := strings.TrimSpace(string(version)); p.Tag != v {
		t.Errorf("provenance tag %q differs from schema/VERSION %q: run make api-spec", p.Tag, v)
	}
	if len(p.Commit) != 40 {
		t.Errorf("provenance commit %q is not a full SHA", p.Commit)
	}
}

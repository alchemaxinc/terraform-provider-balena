//go:build !integration

package schemagen

import (
	"strings"
	"testing"
)

func diffSpec(fields ...Field) *Spec {
	return &Spec{Format: SPEC_FORMAT, APIRoot: "v6", Resources: []Resource{{
		Name: "thing", Term: "thing", Key: "thing",
		Fields: append([]Field{{Name: "id", Term: "id", Type: TYPE_INTEGER, SQLType: "Serial", Required: true, ServerDefault: true}}, fields...),
	}}}
}

var (
	optionalName = Field{Name: "name", Term: "name", Type: TYPE_STRING, SQLType: "Short Text"}
	requiredName = Field{Name: "name", Term: "name", Type: TYPE_STRING, SQLType: "Short Text", Required: true}
	readOnlyName = Field{Name: "name", Term: "name", Type: TYPE_STRING, SQLType: "Short Text", ReadOnly: true}
	intName      = Field{Name: "name", Term: "name", Type: TYPE_INTEGER, SQLType: "Short Text"}
	enumAB       = Field{Name: "kind", Term: "kind", Type: TYPE_STRING, SQLType: "Short Text", Enum: []string{"a", "b"}}
	enumABC      = Field{Name: "kind", Term: "kind", Type: TYPE_STRING, SQLType: "Short Text", Enum: []string{"a", "b", "c"}}
	enumA        = Field{Name: "kind", Term: "kind", Type: TYPE_STRING, SQLType: "Short Text", Enum: []string{"a"}}
	enumFree     = Field{Name: "kind", Term: "kind", Type: TYPE_STRING, SQLType: "Short Text"}
)

func TestDiffSurfaces(t *testing.T) {
	other := &Spec{Format: SPEC_FORMAT, APIRoot: "v6", Resources: append(diffSpec().Resources,
		Resource{Name: "other", Term: "other", Key: "other", Fields: diffSpec().Resources[0].Fields})}

	for name, tc := range map[string]struct {
		oldSpec, newSpec *Spec
		want             ChangeKind
		mention          string
	}{
		"identical":            {diffSpec(optionalName), diffSpec(optionalName), CHANGE_NONE, ""},
		"resource added":       {diffSpec(), other, CHANGE_ADDITIVE, "resource `other` added"},
		"resource removed":     {other, diffSpec(), CHANGE_BREAKING, "resource `other` removed"},
		"optional field added": {diffSpec(), diffSpec(optionalName), CHANGE_ADDITIVE, "`thing.name` added"},
		"required field added": {diffSpec(), diffSpec(requiredName), CHANGE_BREAKING, "added as required"},
		"field removed":        {diffSpec(optionalName), diffSpec(), CHANGE_BREAKING, "`thing.name` removed"},
		"became required":      {diffSpec(optionalName), diffSpec(requiredName), CHANGE_BREAKING, "optional to required"},
		"relaxed to optional":  {diffSpec(requiredName), diffSpec(optionalName), CHANGE_ADDITIVE, "required to optional"},
		"became read-only":     {diffSpec(optionalName), diffSpec(readOnlyName), CHANGE_BREAKING, "optional to computed"},
		"became writable":      {diffSpec(readOnlyName), diffSpec(optionalName), CHANGE_ADDITIVE, "computed to optional"},
		"type changed":         {diffSpec(optionalName), diffSpec(intName), CHANGE_BREAKING, "type or wire field changed"},
		"enum widened":         {diffSpec(enumAB), diffSpec(enumABC), CHANGE_ADDITIVE, "allowed values changed"},
		"enum narrowed":        {diffSpec(enumAB), diffSpec(enumA), CHANGE_BREAKING, "value `b` no longer allowed"},
		"enum introduced":      {diffSpec(enumFree), diffSpec(enumAB), CHANGE_BREAKING, "values now restricted"},
		"enum lifted":          {diffSpec(enumAB), diffSpec(enumFree), CHANGE_ADDITIVE, "allowed values changed"},
	} {
		t.Run(name, func(t *testing.T) {
			d, err := DiffSurfaces(tc.oldSpec, tc.newSpec)
			if err != nil {
				t.Fatal(err)
			}
			if d.Kind != tc.want {
				t.Errorf("kind = %s, want %s (breaking %v, additive %v)", d.Kind, tc.want, d.Breaking, d.Additive)
			}
			if md := d.Markdown(); tc.mention != "" && !strings.Contains(md, tc.mention) {
				t.Errorf("markdown does not mention %q:\n%s", tc.mention, md)
			}
		})
	}
}

func TestDiffMarkdown_DropsControlCharacters(t *testing.T) {
	d := &SurfaceDiff{Breaking: []string{"a\nb\x1b[31m"}}
	if md := d.Markdown(); strings.Contains(md, "\x1b") || strings.Contains(md, "a\nb") {
		t.Errorf("markdown keeps control characters: %q", md)
	}
}

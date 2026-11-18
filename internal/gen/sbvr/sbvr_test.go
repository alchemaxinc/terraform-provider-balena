package sbvr

import (
	"os"
	"strings"
	"testing"
)

const SAMPLE = `Vocabulary: balena

Term: name
	Concept Type: Short Text (Type)
Term: tag key
	Concept Type: Short Text (Type)
Term: value
	Concept Type: Text (Type)
Term: date
	Concept Type: Date Time (Type)
Term: class
	Concept Type: Short Text (Type)

Term: organization
Term: application

	Fact type: application [should track latest release]

	Fact type: application is of class
		Necessity: each application is of exactly one class.
		Definition: "fleet" or "block"

	Fact type: application has tag key
		Term Form: application tag
		Necessity: each application tag has a tag key that has a Length (Type) that is greater than 0 and is less than or equal to 100.

Fact type: application tag has value
	Necessity: each application tag has exactly one value.

Fact type: application has organization
	Synonymous Form: organization has application
	Necessity: each application has exactly one organization.

Fact type: application has name
	Necessity: each application has exactly one name
	Necessity: each name is of exactly one application
	Necessity: each application has a name that has a Length (Type) that is equal to 32.

Fact type: application is updated by application1
	Necessity: each application is updated by at most one application.

Fact type: application [was archived on] date
	Necessity: each application [was archived on] at most one date.

Fact type: organization has application1 has tag key
	Term Form: organization tag
	Synonymous Form: application1 is tagged in organization

Rule: It is necessary that each application has a name.
`

func TestParseSample(t *testing.T) {
	m, err := Parse(SAMPLE)
	if err != nil {
		t.Fatal(err)
	}
	app := m.Tables["application"]
	if app == nil {
		t.Fatal("no application table")
	}
	check := func(tb *Table, name string, want Field) {
		t.Helper()
		f := tb.Field(name)
		if f == nil {
			t.Fatalf("%s.%s missing; have %v", tb.Name, name, fieldNames(tb))
		}
		if f.Type != want.Type || f.Target != want.Target || f.Required != want.Required || f.Unique != want.Unique ||
			f.MinLength != want.MinLength || f.MaxLength != want.MaxLength || strings.Join(f.Enum, ",") != strings.Join(want.Enum, ",") {
			t.Errorf("%s.%s = %+v, want %+v", tb.Name, name, *f, want)
		}
	}
	check(app, "should_track_latest_release", Field{Type: "Boolean", Required: true})
	check(app, "is_of__class", Field{Type: "Short Text", Required: true, Enum: []string{"fleet", "block"}})
	check(app, "organization", Field{Target: "organization", Required: true})
	check(app, "name", Field{Type: "Short Text", Required: true, Unique: true, MinLength: 32, MaxLength: 32})
	check(app, "is_updated_by__application", Field{Target: "application"})
	check(app, "was_archived_on__date", Field{Type: "Date Time"})

	tag := m.Tables["application_tag"]
	if tag == nil || !tag.Link || tag.Canonical != "application__has__tag_key" {
		t.Fatalf("application_tag = %+v", tag)
	}
	check(tag, "application", Field{Target: "application", Required: true})
	check(tag, "tag_key", Field{Type: "Short Text", Required: true, MinLength: 1, MaxLength: 100})
	check(tag, "value", Field{Type: "Text", Required: true})

	otag := m.Tables["organization_tag"]
	if otag == nil {
		t.Fatal("no organization_tag")
	}
	if got := strings.Join(fieldNames(otag), ","); got != "organization,application,tag_key" {
		t.Errorf("organization_tag fields = %s", got)
	}
	if got := strings.Join(otag.Aliases, ","); got != "is_tagged_in__organization" {
		t.Errorf("organization_tag aliases = %s", got)
	}
	if len(m.Rules) != 1 {
		t.Errorf("rules = %v", m.Rules)
	}
}

func fieldNames(tb *Table) []string {
	var out []string
	for _, f := range tb.Fields {
		out = append(out, f.Name)
	}
	return out
}

func TestParseRejectsUnknownLines(t *testing.T) {
	if _, err := Parse(SAMPLE + "\nSomething: else\n"); err == nil {
		t.Error("expected an error for an unknown line")
	}
	if _, err := Parse("Term: a\nNecessity: each a has exactly one b.\n"); err == nil {
		t.Error("expected an error for a necessity outside a fact type")
	}
}

func TestParseVendoredSBVR(t *testing.T) {
	for _, path := range []string{"../../../schema/upstream/open-balena-api/src/balena.sbvr", "../../../schema/upstream/open-balena-api/src/translations/v7/v7.sbvr"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		m, err := Parse(string(data))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(m.Tables) < 40 {
			t.Errorf("%s: %d tables", path, len(m.Tables))
		}
	}
}

// TestLengthNecessities checks that only the unconditional length forms bound a column; any other length necessity,
// a conditional one in particular, is recorded as unsupported instead of applied.
func TestLengthNecessities(t *testing.T) {
	const src = `Vocabulary: balena

Term: name
	Concept Type: Short Text (Type)
Term: note
	Concept Type: Text (Type)
Term: handle
	Concept Type: Short Text (Type)
Term: composition
	Concept Type: JSON (Type)

Term: application

Fact type: application has name
	Necessity: each application has at most one name.
	Necessity: each application has a name that has a Length (Type) that is greater than 3 and is less than or equal to 50.
Fact type: application has note
	Necessity: each application has at most one note.
	Necessity: each application that has a note, has a note that has a Length (Type) that is less than or equal to 100.
Fact type: application has handle
	Necessity: each application has at most one handle.
	Necessity: each handle of an application, has a Length (Type) that is greater than or equal to 2.
	Necessity: each application that is archived, has a handle that has a Length (Type) that is less than or equal to 5.
	Necessity: each application that has a note, has a handle that has a Length (Type) that is less than or equal to 6.
	Necessity: it is necessary that each application has a handle that has a Length (Type) that is less than or equal to 7.
	Necessity: each application has a handle that has a Length (Type) that is less than or equal to 8 or is equal to 0.
Fact type: application has composition
	Necessity: each application has at most one composition.
	Necessity: each application has a composition that is represented by a Text (Type) that has a Length (Type) that is less than or equal to 9.
`
	m, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	app := m.Tables["application"]
	for _, tc := range []struct {
		field    string
		min, max int
	}{{"name", 4, 50}, {"note", 0, 100}, {"handle", 2, 0}, {"composition", 0, 9}} {
		f := app.Field(tc.field)
		if f == nil || f.MinLength != tc.min || f.MaxLength != tc.max {
			t.Errorf("%s = %+v, want lengths %d..%d", tc.field, f, tc.min, tc.max)
		}
	}
	if len(app.Unsupported) != 4 {
		t.Errorf("unsupported = %q, want the 4 conditional or unknown forms", app.Unsupported)
	}
	for _, u := range app.Unsupported {
		if !strings.Contains(u, "handle") {
			t.Errorf("unexpected unsupported necessity %q", u)
		}
	}
}

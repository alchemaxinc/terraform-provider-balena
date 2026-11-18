package gen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alchemaxinc/terraform-provider-balena/internal/gen/tsmodel"
)

const REPO = "../.."

func loadInputs(t *testing.T) *Inputs {
	t.Helper()
	in, err := LoadInputs(REPO)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// TestCommittedOutputsAreUpToDate is the `check` gate as a unit test: generation succeeds, is deterministic, and
// reproduces every committed generated file byte for byte.
func TestCommittedOutputsAreUpToDate(t *testing.T) {
	first, err := Generate(REPO)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(REPO)
	if err != nil {
		t.Fatal(err)
	}
	for p, data := range first.Files {
		if !bytes.Equal(data, second.Files[p]) {
			t.Errorf("%s differs between two generations", p)
		}
	}
	if stale := first.Stale(REPO); len(stale) > 0 {
		t.Errorf("stale generated files (run `make generate`): %v", stale)
	}
}

func interfaceOf(m *tsmodel.Model, resource string) *tsmodel.Interface {
	if name := m.Synonyms[resource]; name != "" {
		return m.Interfaces[name]
	}
	return m.Interfaces[m.Canonical[resource]]
}

func removeRead(i *tsmodel.Interface, field string) {
	var kept []tsmodel.Field
	for _, f := range i.Read {
		if f.Name != field {
			kept = append(kept, f)
		}
	}
	i.Read = kept
}

// TestCrossCheckFailsClosed mutates the parsed models and expects the cross-check to report each difference.
func TestCrossCheckFailsClosed(t *testing.T) {
	if _, problems, err := BuildIR(loadInputs(t), nil); err != nil || len(problems) > 0 {
		t.Fatalf("pinned sources must cross-check cleanly: %v %v", err, problems)
	}
	tests := []struct {
		name   string
		mutate func(in *Inputs)
		want   string
	}{
		{
			name:   "SBVR column missing from the unversioned model",
			mutate: func(in *Inputs) { removeRead(interfaceOf(in.Models["resin"], "application"), "app_name") },
			want:   "cross-check resin: application.app_name is in the SBVR but not in the TypeScript model",
		},
		{
			name: "unexplained column in the unversioned model",
			mutate: func(in *Inputs) {
				i := interfaceOf(in.Models["resin"], "device")
				i.Read = append(i.Read, tsmodel.Field{Name: "mystery", Kind: tsmodel.KIND_SCALAR, Type: "Text"})
				i.Write = append(i.Write, tsmodel.Field{Name: "mystery", Kind: tsmodel.KIND_SCALAR, Type: "Text"})
			},
			want: "cross-check resin: device.mystery is in the TypeScript model but not in the SBVR",
		},
		{
			name: "type disagreement",
			mutate: func(in *Inputs) {
				i := interfaceOf(in.Models["resin"], "device")
				for k := range i.Read {
					if i.Read[k].Name == "device_name" {
						i.Read[k].Type = "Integer"
					}
				}
			},
			want: "cross-check resin: device.device_name has type Short Text in the SBVR but Integer in the TypeScript model",
		},
		{
			name: "cloud typings lose an open table",
			mutate: func(in *Inputs) {
				m := in.Models["v7"]
				delete(m.Canonical, "device__has__tag_key")
				delete(m.Synonyms, "device_tag")
			},
			want: "cross-check v7: SBVR table device_tag (device__has__tag_key) is missing from the TypeScript model",
		},
		{
			name: "nullability disagreement",
			mutate: func(in *Inputs) {
				i := interfaceOf(in.Models["v7"], "application_tag")
				for k := range i.Read {
					if i.Read[k].Name == "value" {
						i.Read[k].Nullable = true
					}
				}
			},
			want: "cross-check v7: application_tag.value is required=true in the SBVR but nullable=true in the TypeScript model",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := loadInputs(t)
			tc.mutate(in)
			_, problems, err := BuildIR(in, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("problems = %v, want %q", problems, tc.want)
			}
		})
	}
	// A column only balena-cloud has is not a disagreement: it is recorded as cloud-only.
	in := loadInputs(t)
	i := interfaceOf(in.Models["v7"], "device")
	i.Read = append(i.Read, tsmodel.Field{Name: "cloud_thing", Kind: tsmodel.KIND_SCALAR, Type: "Text"})
	ir, problems, _ := BuildIR(in, nil)
	if len(problems) > 0 || ir.Table("device").Field("cloud_thing").Provenance != PROVENANCE_CLOUD {
		t.Errorf("cloud-only column: problems %v", problems)
	}
}

// attrs builds a spec from field names: a trailing "!" marks a reference exposed by id, a trailing "~" a reference
// exposed by a column of the referenced row.
func attrs(fields ...string) *Spec {
	s := &Spec{TypeName: "balena_t"}
	for _, f := range fields {
		a := &Attr{Field: f, Encoding: ENC_PLAIN}
		switch {
		case strings.HasSuffix(f, "!"):
			a.Field = strings.TrimSuffix(f, "!")
			a.Encoding = ENC_REF
		case strings.HasSuffix(f, "~"):
			a.Field = strings.TrimSuffix(f, "~")
			a.Encoding = ENC_REF_BY
		}
		s.Attributes = append(s.Attributes, a)
	}
	return s
}

func names(s *Spec) string {
	var out []string
	for _, a := range s.Attributes {
		out = append(out, a.Field+"="+a.Name)
	}
	return strings.Join(out, " ")
}

func TestNamingRules(t *testing.T) {
	roleNames := map[string]*NameEntry{
		"should_be_running__release": {Name: "target_release", Reason: "r"},
		"is_pinned_on__release":      {Name: "pinned_release", Reason: "r"},
	}
	tests := []struct {
		name  string
		spec  *Spec
		lock  *SurfaceLock
		to    *TableOverride
		names map[string]*NameEntry
		want  string
		error string
	}{
		{
			name: "verbs are stripped and references get _id",
			spec: attrs("id", "is_for__device_type!", "organization!", "is_of__class", "app_name"),
			want: "id=id is_for__device_type=device_type_id organization=organization_id is_of__class=class app_name=app_name",
		},
		{
			name: "sharers without a verbless member all keep their verb",
			spec: attrs("is_pinned_on__release!", "is_running__release!", "device_name"),
			want: "is_pinned_on__release=is_pinned_on_release_id is_running__release=is_running_release_id device_name=device_name",
		},
		{
			name: "the verbless sharer wins the stripped name",
			spec: attrs("application!", "on__application!"),
			want: "application=application_id on__application=on_application_id",
		},
		{
			name: "a generic remainder is dropped instead of the verb",
			spec: attrs("is_finalized_at__date", "is_locked_until__date", "changed_api_heartbeat_state_on__date"),
			want: "is_finalized_at__date=is_finalized_at is_locked_until__date=is_locked_until changed_api_heartbeat_state_on__date=changed_api_heartbeat_state_on",
		},
		{
			name: "a dropped remainder that collides with a column keeps it",
			spec: attrs("is_locked_until", "is_locked_until__date"),
			want: "is_locked_until=is_locked_until is_locked_until__date=is_locked_until_date",
		},
		{
			name: "locked names are sticky; a new colliding field keeps its verb",
			spec: attrs("should_be_running__release!", "is_pinned_on__release!"),
			lock: &SurfaceLock{Resources: map[string]*SurfaceType{"balena_t": {Attributes: map[string]*SurfaceAttr{
				"release_id": {Field: "should_be_running__release"},
			}}}},
			want: "should_be_running__release=release_id is_pinned_on__release=is_pinned_on_release_id",
		},
		{
			name: "the names map wins over the lock; references by id get _id, by a column stay bare",
			spec: attrs("should_be_running__release!", "is_pinned_on__release~", "is_running__release!"),
			lock: &SurfaceLock{Resources: map[string]*SurfaceType{"balena_t": {Attributes: map[string]*SurfaceAttr{
				"release_id": {Field: "should_be_running__release"},
			}}}},
			names: roleNames,
			want:  "should_be_running__release=target_release_id is_pinned_on__release=pinned_release is_running__release=release_id",
		},
		{
			name:  "a table override name wins over the names map",
			spec:  attrs("is_pinned_on__release!"),
			to:    &TableOverride{Fields: map[string]*FieldOverride{"is_pinned_on__release": {Name: "pin_id", Reason: "r"}}},
			names: roleNames,
			want:  "is_pinned_on__release=pin_id",
		},
		{
			name: "rule names are lowercased",
			spec: attrs("includes__SLA"),
			want: "includes__SLA=sla",
		},
		{
			name: "a reserved rule name gets a suffix",
			spec: attrs("provider", "has__count"),
			want: "provider=provider_name has__count=count_name",
		},
		{
			name:  "a suffixed reserved name that collides fails",
			spec:  attrs("provider", "provider_name"),
			error: "is used by both",
		},
		{
			name:  "an invalid rule name fails",
			spec:  attrs("rank__1st"),
			error: "not a valid Terraform attribute name",
		},
		{
			name:  "a reserved override name fails",
			spec:  attrs("x"),
			to:    &TableOverride{Fields: map[string]*FieldOverride{"x": {Name: "provider", Reason: "r"}}},
			error: "reserved by Terraform",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := nameAttributes(tc.spec.Attributes, tc.spec.TypeName, tc.to, tc.names, tc.lock)
			if tc.error != "" {
				if err == nil || !strings.Contains(err.Error(), tc.error) {
					t.Errorf("err = %v, want %q", err, tc.error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := names(tc.spec); got != tc.want {
				t.Errorf("names = %s\nwant    %s", got, tc.want)
			}
		})
	}
}

// TestNamingAllTables names every table of the IR, excluded ones included, with the repository's names map and no
// surface lock: every name must be valid, and dropping a `__date` remainder must never collide.
func TestNamingAllTables(t *testing.T) {
	in := loadInputs(t)
	o, err := LoadOverrides(REPO)
	if err != nil {
		t.Fatal(err)
	}
	ir, _, err := BuildIR(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Classify(ir, o, map[string]bool{"resin": true}); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, tb := range ir.Tables {
		to := o.Tables[tb.Name]
		if to == nil {
			to = &TableOverride{}
		}
		attrs, _, problems := exposedAttrs(ir, tb, o, to, nil, nil)
		if len(problems) > 0 {
			t.Errorf("%s: %v", tb.Name, problems)
			continue
		}
		if err := nameAttributes(attrs, tb.Class.TypeName, to, o.Names, nil); err != nil {
			t.Errorf("%s: %v", tb.Name, err)
			continue
		}
		for _, a := range attrs {
			got[tb.Name+"."+a.Field] = a.Name
			if strings.HasSuffix(a.Field, "__date") && a.NameSource == NAME_SRC_COLLISION {
				t.Errorf("%s.%s: dropping __date collides (%v)", tb.Name, a.Field, a.CollidesWith)
			}
		}
	}
	for field, want := range map[string]string{
		"social_service_account.provider":                                        "provider_name",
		"support_tier.includes__SLA":                                             "sla",
		"device.is_locked_until__date":                                           "is_locked_until",
		"release.is_finalized_at__date":                                          "is_finalized_at",
		"device.should_be_managed_by__release":                                   "supervisor_release_id",
		"user.has_legacy_link_to__organization":                                  "legacy_organization_id",
		"device_type_alias.device_type":                                          "device_type",
		"application__can_use__application_as_host.can_use__application_as_host": "host_application_id",
	} {
		if got[field] != want {
			t.Errorf("%s = %q, want %q", field, got[field], want)
		}
	}
}

func TestOverridesValidation(t *testing.T) {
	tests := map[string]string{
		"unknown key":             "scope: [application]\nbogus: 1\n",
		"field without reason":    "tables:\n  application:\n    fields:\n      slug: {mode: computed}\n",
		"table setting no reason": "tables:\n  release:\n    resource: false\n",
		"unknown mode":            "tables:\n  application:\n    fields:\n      slug: {mode: sometimes, reason: r}\n",
		"empty field entry":       "tables:\n  application:\n    fields:\n      slug: {reason: r}\n",
		"hook without uses":       "hooks:\n  someHook: {reason: r}\n",
		"resolver without ref":    "tables:\n  device:\n    fields:\n      x: {resolver: h, mode: optional, reason: r}\n",
		"rule needs one matcher":  "rules:\n  sensitive:\n    - {field: value, reason: r}\n",
		"duplicate scope":         "scope: [a, a]\n",
		"name without reason":     "names:\n  on__application: {name: host_application}\n",
		"name with _id":           "names:\n  on__application: {name: host_application_id, reason: r}\n",
		"invalid name":            "names:\n  on__application: {name: Host, reason: r}\n",
		"reserved name":           "names:\n  on__application: {name: provider, reason: r}\n",
		"reference without by":    "references:\n  device_type: {reason: r}\n",
		"unknown hook step":       "tables:\n  a:\n    reason: r\n    hooks: {update: h}\n",
	}
	for name, src := range tests {
		if _, err := ParseOverrides([]byte(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestOverridesSemanticChecks loads the repository overrides with one change each and expects generation to fail.
func TestOverridesSemanticChecks(t *testing.T) {
	in := loadInputs(t)
	base, err := LoadOverrides(REPO)
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := implementedHooks(REPO)
	if err != nil {
		t.Fatal(err)
	}
	run := func(o *Overrides) error {
		ir, _, err := BuildIR(in, nil)
		if err != nil {
			return err
		}
		if err := Classify(ir, o, map[string]bool{"resin": true}); err != nil {
			return err
		}
		_, err = BuildSpecs(ir, o, in.ConfigVars, &SurfaceLock{}, hooks)
		return err
	}
	if err := run(base); err != nil {
		t.Fatalf("repository overrides must pass: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(o *Overrides)
		want   string
	}{
		{"unknown table in scope", func(o *Overrides) { o.Scope = append(o.Scope, "no_such_table") }, "no locked root has"},
		{"excluded table in scope", func(o *Overrides) { o.Scope = append(o.Scope, "plan") }, "which the rules exclude"},
		{"unknown field", func(o *Overrides) {
			o.Tables["application"].Fields["no_such_field"] = &FieldOverride{Mode: MODE_OPTIONAL, Reason: "r"}
		}, "no such exposed column"},
		{"no-op mode", func(o *Overrides) {
			o.Tables["application"].Fields["app_name"] = &FieldOverride{Mode: MODE_REQUIRED, Reason: "r"}
		}, "is a no-op"},
		{"missing hook implementation", func(o *Overrides) {
			o.Hooks["notImplemented"] = &HookDecl{Uses: []string{"device.id"}, Reason: "r"}
			o.Tables["device"].Hooks["delete"] = "notImplemented"
		}, "has no implementation"},
		{"hook uses an unknown column", func(o *Overrides) {
			o.Hooks["deviceForget"].Uses = append(o.Hooks["deviceForget"].Uses, "device.no_such_column")
		}, "not an IR column"},
		{"rule that matches nothing", func(o *Overrides) {
			o.Rules.Sensitive = append(o.Rules.Sensitive, &FieldRule{TableSuffix: "_nothing", Field: "value", Reason: "r"})
		}, "matches no in-scope column"},
		{"override outside the scope", func(o *Overrides) {
			o.Tables["team"] = &TableOverride{Reason: "r", Resource: new(bool)}
		}, "outside the scope"},
		{"names entry matching no column", func(o *Overrides) {
			o.Names["no_such__field"] = &NameEntry{Name: "x", Reason: "r"}
		}, "no column of any table in any locked root"},
		{"no-op names entry", func(o *Overrides) {
			o.Names["app_name"] = &NameEntry{Name: "app_name", Reason: "r"}
		}, "a no-op"},
		{"reference by a column that is not unique", func(o *Overrides) {
			o.References["device_type"] = &ReferenceEntry{By: "name", Reason: "r"}
		}, "is not a unique column"},
		{"reference to a table nothing references", func(o *Overrides) {
			o.References["device_type_alias"] = &ReferenceEntry{By: "is_referenced_by__alias", Reason: "r"}
		}, "no exposed column of any table references"},
		{"field reference_by that repeats references", func(o *Overrides) {
			o.Tables["application"].Fields["is_for__device_type"] = &FieldOverride{ReferenceBy: "slug", Reason: "r"}
		}, "already exposes device_type by slug"},
		{"non-unique reference_by without resolver", func(o *Overrides) {
			o.Tables["device"].Fields["is_pinned_on__release"].Resolver = ""
		}, "needs a resolver hook"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o, err := LoadOverrides(REPO)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(o)
			if err := run(o); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSurfaceDiffLevels(t *testing.T) {
	base := func() *SurfaceLock {
		return &SurfaceLock{
			Format: SURFACE_FORMAT,
			Resources: map[string]*SurfaceType{"balena_x": {Table: "x", Root: "v7", Attributes: map[string]*SurfaceAttr{
				"id":   {Field: "id", Type: "int64", Mode: MODE_COMPUTED},
				"name": {Field: "name", Type: "string", Mode: MODE_REQUIRED, ForceNew: true, Validators: []string{"length:1..100", "one_of:a,b"}},
				"note": {Field: "note", Type: "string", Mode: MODE_OPTIONAL},
			}}},
			DataSources: map[string]*SurfaceType{"balena_x": {Table: "x", Root: "v7", Attributes: map[string]*SurfaceAttr{
				"name": {Field: "name", Type: "string", Mode: DS_FILTER},
			}}},
		}
	}
	tests := []struct {
		name   string
		mutate func(l *SurfaceLock)
		want   string
	}{
		{"identical", func(*SurfaceLock) {}, LEVEL_NONE},
		{"resource added", func(l *SurfaceLock) { l.Resources["balena_y"] = &SurfaceType{Attributes: map[string]*SurfaceAttr{}} }, LEVEL_FEAT},
		{"resource removed", func(l *SurfaceLock) { delete(l.Resources, "balena_x") }, LEVEL_BREAKING},
		{"optional attribute added", func(l *SurfaceLock) {
			l.Resources["balena_x"].Attributes["extra"] = &SurfaceAttr{Field: "extra", Type: "string", Mode: MODE_OPTIONAL}
		}, LEVEL_FEAT},
		{"required attribute added", func(l *SurfaceLock) {
			l.Resources["balena_x"].Attributes["extra"] = &SurfaceAttr{Field: "extra", Type: "string", Mode: MODE_REQUIRED}
		}, LEVEL_BREAKING},
		{"attribute renamed", func(l *SurfaceLock) {
			a := l.Resources["balena_x"].Attributes["note"]
			delete(l.Resources["balena_x"].Attributes, "note")
			l.Resources["balena_x"].Attributes["notes"] = a
		}, LEVEL_BREAKING},
		{"field remapped", func(l *SurfaceLock) { l.Resources["balena_x"].Attributes["note"].Field = "other" }, LEVEL_BREAKING},
		{"type changed", func(l *SurfaceLock) { l.Resources["balena_x"].Attributes["note"].Type = "int64" }, LEVEL_BREAKING},
		{"optional to required", func(l *SurfaceLock) { l.Resources["balena_x"].Attributes["note"].Mode = MODE_REQUIRED }, LEVEL_BREAKING},
		{"required to optional", func(l *SurfaceLock) { l.Resources["balena_x"].Attributes["name"].Mode = MODE_OPTIONAL }, LEVEL_FEAT},
		{"new force_new", func(l *SurfaceLock) { l.Resources["balena_x"].Attributes["note"].ForceNew = true }, LEVEL_BREAKING},
		{"force_new dropped", func(l *SurfaceLock) { l.Resources["balena_x"].Attributes["name"].ForceNew = false }, LEVEL_FEAT},
		{"length loosened", func(l *SurfaceLock) {
			l.Resources["balena_x"].Attributes["name"].Validators = []string{"length:1..164", "one_of:a,b"}
		}, LEVEL_FIX},
		{"length tightened", func(l *SurfaceLock) {
			l.Resources["balena_x"].Attributes["name"].Validators = []string{"length:1..50", "one_of:a,b"}
		}, LEVEL_BREAKING},
		{"enum widened", func(l *SurfaceLock) {
			l.Resources["balena_x"].Attributes["name"].Validators = []string{"length:1..100", "one_of:a,b,c"}
		}, LEVEL_FIX},
		{"validator removed", func(l *SurfaceLock) {
			l.Resources["balena_x"].Attributes["name"].Validators = []string{"length:1..100"}
		}, LEVEL_FIX},
		{"sensitive added", func(l *SurfaceLock) { l.Resources["balena_x"].Attributes["note"].Sensitive = true }, LEVEL_BREAKING},
		{"root moved", func(l *SurfaceLock) { l.Resources["balena_x"].Root = "v8" }, LEVEL_FEAT},
		{"data source filter dropped", func(l *SurfaceLock) { l.DataSources["balena_x"].Attributes["name"].Mode = DS_COMPUTED }, LEVEL_BREAKING},
	}
	for _, tc := range tests {
		n := base()
		tc.mutate(n)
		if _, level := SurfaceDiff(base(), n); level != tc.want {
			t.Errorf("%s: level = %s, want %s", tc.name, level, tc.want)
		}
	}
}

// TestStickyDrifts checks that a locked name the current rules would change is kept and reported.
func TestStickyDrifts(t *testing.T) {
	res, err := Generate(REPO)
	if err != nil {
		t.Fatal(err)
	}
	lock := &SurfaceLock{Format: SURFACE_FORMAT, Resources: map[string]*SurfaceType{}, DataSources: map[string]*SurfaceType{
		"balena_device": {Table: "device", Attributes: map[string]*SurfaceAttr{
			"is_locked_until_date": {Field: "is_locked_until__date"},
		}},
	}}
	sticky, problems := NamingPreview(res.IR, res.Overrides, lock)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	fresh, _ := NamingPreview(res.IR, res.Overrides, nil)
	drifts := StickyDrifts(sticky, fresh)
	want := StickyDrift{Table: "device", Field: "is_locked_until__date", Locked: "is_locked_until_date", Rule: "is_locked_until"}
	if len(drifts) != 1 || drifts[0] != want {
		t.Errorf("drifts = %+v", drifts)
	}
}

// TestGenerateForOtherRoot builds the surface the unversioned root would give, as a promotion of a new root would,
// and checks that names stay sticky across roots.
func TestGenerateForOtherRoot(t *testing.T) {
	res, err := GenerateForRoot(REPO, "resin")
	if err != nil {
		t.Fatal(err)
	}
	app := res.Surface.Resources["balena_application"]
	if app == nil || app.Root != "resin" || app.Attributes["app_name"] == nil || app.Attributes["device_type"] == nil {
		t.Fatalf("balena_application on resin = %+v", app)
	}
	def, err := Generate(REPO)
	if err != nil {
		t.Fatal(err)
	}
	changes, level := SurfaceDiff(def.Surface, res.Surface)
	if level == LEVEL_NONE || len(changes) == 0 {
		t.Errorf("moving every table to /resin must show in the diff")
	}
}

// TestModeChangeLevels pins the level of every mode change of resource and data source attributes.
func TestModeChangeLevels(t *testing.T) {
	type pair = [2]string
	want := map[bool]map[pair]string{
		true: {
			{MODE_REQUIRED, MODE_OPTIONAL}:          LEVEL_FEAT,
			{MODE_REQUIRED, MODE_OPTIONAL_COMPUTED}: LEVEL_FEAT,
			{MODE_REQUIRED, MODE_COMPUTED}:          LEVEL_BREAKING, // set in every configuration, then rejected
			{MODE_OPTIONAL, MODE_REQUIRED}:          LEVEL_BREAKING,
			{MODE_OPTIONAL, MODE_OPTIONAL_COMPUTED}: LEVEL_FEAT,
			{MODE_OPTIONAL, MODE_COMPUTED}:          LEVEL_BREAKING,
			{MODE_OPTIONAL_COMPUTED, MODE_REQUIRED}: LEVEL_BREAKING,
			{MODE_OPTIONAL_COMPUTED, MODE_OPTIONAL}: LEVEL_BREAKING,
			{MODE_OPTIONAL_COMPUTED, MODE_COMPUTED}: LEVEL_BREAKING,
			{MODE_COMPUTED, MODE_REQUIRED}:          LEVEL_BREAKING,
			{MODE_COMPUTED, MODE_OPTIONAL}:          LEVEL_BREAKING, // unset in every configuration, then planned as null
			{MODE_COMPUTED, MODE_OPTIONAL_COMPUTED}: LEVEL_FEAT,
		},
		false: {
			{DS_FILTER, DS_COMPUTED}: LEVEL_BREAKING,
			{DS_COMPUTED, DS_FILTER}: LEVEL_FEAT,
		},
	}
	modes := map[bool][]string{
		true:  {MODE_REQUIRED, MODE_OPTIONAL, MODE_OPTIONAL_COMPUTED, MODE_COMPUTED},
		false: {DS_FILTER, DS_COMPUTED},
	}
	for _, resource := range []bool{true, false} {
		table := DATA_SOURCE_MODE_CHANGES
		if resource {
			table = RESOURCE_MODE_CHANGES
		}
		if len(table) != len(want[resource]) {
			t.Errorf("resource=%v: %d classified mode changes, want %d", resource, len(table), len(want[resource]))
		}
		for _, o := range modes[resource] {
			for _, n := range modes[resource] {
				if o == n {
					continue
				}
				c, ok := table[pair{o, n}]
				if !ok || c.Reason == "" {
					t.Errorf("resource=%v %s -> %s is not classified with a reason", resource, o, n)
				}
				if c.Level != want[resource][pair{o, n}] {
					t.Errorf("resource=%v %s -> %s = %s, want %s", resource, o, n, c.Level, want[resource][pair{o, n}])
				}
				old := &SurfaceLock{Format: SURFACE_FORMAT, Resources: map[string]*SurfaceType{}, DataSources: map[string]*SurfaceType{}}
				new := &SurfaceLock{Format: SURFACE_FORMAT, Resources: map[string]*SurfaceType{}, DataSources: map[string]*SurfaceType{}}
				kind := old.DataSources
				newKind := new.DataSources
				if resource {
					kind, newKind = old.Resources, new.Resources
				}
				kind["balena_x"] = &SurfaceType{Table: "x", Root: "v7", Attributes: map[string]*SurfaceAttr{"a": {Field: "a", Type: "string", Mode: o}}}
				newKind["balena_x"] = &SurfaceType{Table: "x", Root: "v7", Attributes: map[string]*SurfaceAttr{"a": {Field: "a", Type: "string", Mode: n}}}
				if _, level := SurfaceDiff(old, new); level != want[resource][pair{o, n}] {
					t.Errorf("SurfaceDiff resource=%v %s -> %s = %s, want %s", resource, o, n, level, want[resource][pair{o, n}])
				}
			}
		}
	}
	if c := modeChangeOf(true, "write_only", MODE_OPTIONAL); c.Level != LEVEL_BREAKING {
		t.Errorf("an unknown mode change is %s, want %s", c.Level, LEVEL_BREAKING)
	}
}

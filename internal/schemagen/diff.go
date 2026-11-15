package schemagen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/alchemaxinc/terraform-provider-balena/internal/engine"
)

// ChangeKind classifies a change of the generated Terraform surface.
type ChangeKind string

// Change kinds, in increasing order of impact.
const (
	CHANGE_NONE     ChangeKind = "none"
	CHANGE_ADDITIVE ChangeKind = "additive"
	CHANGE_BREAKING ChangeKind = "breaking"
)

// SurfaceDiff is the difference between the surfaces generated from two specs.
type SurfaceDiff struct {
	Kind     ChangeKind `json:"kind"`
	Breaking []string   `json:"breaking"`
	Additive []string   `json:"additive"`
}

func (d *SurfaceDiff) breaking(format string, args ...any) {
	d.Breaking = append(d.Breaking, fmt.Sprintf(format, args...))
}

func (d *SurfaceDiff) additive(format string, args ...any) {
	d.Additive = append(d.Additive, fmt.Sprintf(format, args...))
}

// DiffSurfaces compares the surfaces generated from two specs. A change that
// can make an existing configuration or state invalid is breaking: a removed
// resource, attribute or filter, a changed type, a stricter mode, a narrowed
// enum or a newly sensitive value.
func DiffSurfaces(oldSpec, newSpec *Spec) (*SurfaceDiff, error) {
	oldDs, err := Descriptors(oldSpec)
	if err != nil {
		return nil, err
	}
	newDs, err := Descriptors(newSpec)
	if err != nil {
		return nil, err
	}
	d := &SurfaceDiff{Breaking: []string{}, Additive: []string{}}
	oldByName := map[string]engine.Resource{}
	for _, r := range oldDs {
		oldByName[r.TypeName] = r
	}
	newNames := map[string]bool{}
	for _, nr := range newDs {
		newNames[nr.TypeName] = true
		or, ok := oldByName[nr.TypeName]
		if !ok {
			d.additive("resource `%s` added", nr.TypeName)
			continue
		}
		diffResource(d, or, nr)
	}
	for _, or := range oldDs {
		if !newNames[or.TypeName] {
			d.breaking("resource `%s` removed", or.TypeName)
		}
	}
	switch {
	case len(d.Breaking) > 0:
		d.Kind = CHANGE_BREAKING
	case len(d.Additive) > 0:
		d.Kind = CHANGE_ADDITIVE
	default:
		d.Kind = CHANGE_NONE
	}
	return d, nil
}

func diffResource(d *SurfaceDiff, or, nr engine.Resource) {
	if or.Path != nr.Path {
		d.breaking("`%s`: path changed from `%s` to `%s`", nr.TypeName, or.Path, nr.Path)
	}
	if or.Description != nr.Description || or.DataSourceDescription != nr.DataSourceDescription {
		d.additive("`%s`: description changed", nr.TypeName)
	}
	oldAttrs := map[string]engine.Attribute{}
	for _, a := range or.Attributes {
		oldAttrs[a.Name] = a
	}
	newAttrs := map[string]bool{}
	for _, na := range nr.Attributes {
		newAttrs[na.Name] = true
		oa, ok := oldAttrs[na.Name]
		if !ok {
			if na.Mode == engine.MODE_REQUIRED {
				d.breaking("`%s.%s` added as required", nr.TypeName, na.Name)
			} else {
				d.additive("`%s.%s` added", nr.TypeName, na.Name)
			}
			continue
		}
		diffAttribute(d, nr.TypeName, oa, na)
	}
	for _, oa := range or.Attributes {
		if !newAttrs[oa.Name] {
			d.breaking("`%s.%s` removed", nr.TypeName, oa.Name)
		}
	}
}

func diffAttribute(d *SurfaceDiff, resource string, oa, na engine.Attribute) {
	name := resource + "." + na.Name
	if oa.Wire != na.Wire || oa.Type != na.Type || oa.Codec != na.Codec {
		d.breaking("`%s`: type or wire field changed", name)
	}
	if oa.Mode != na.Mode {
		if stricter(oa.Mode, na.Mode) {
			d.breaking("`%s`: mode changed from %s to %s", name, modeName[oa.Mode], modeName[na.Mode])
		} else {
			d.additive("`%s`: mode changed from %s to %s", name, modeName[oa.Mode], modeName[na.Mode])
		}
	}
	if !oa.Sensitive && na.Sensitive {
		d.breaking("`%s`: now sensitive", name)
	} else if oa.Sensitive && !na.Sensitive {
		d.additive("`%s`: no longer sensitive", name)
	}
	if oa.WriteOnly != na.WriteOnly {
		d.breaking("`%s`: write-only changed to %t", name, na.WriteOnly)
	}
	if oa.Filterable && !na.Filterable {
		d.breaking("`%s`: no longer a data source filter", name)
	} else if !oa.Filterable && na.Filterable {
		d.additive("`%s`: now a data source filter", name)
	}
	narrowed := false
	if len(oa.Enum) == 0 && len(na.Enum) > 0 {
		d.breaking("`%s`: values now restricted", name)
		narrowed = true
	} else if len(na.Enum) > 0 {
		for _, v := range oa.Enum {
			if !slices.Contains(na.Enum, v) {
				d.breaking("`%s`: value `%s` no longer allowed", name, stripBackticks(v))
				narrowed = true
			}
		}
	}
	if !narrowed && !slices.Equal(oa.Enum, na.Enum) {
		d.additive("`%s`: allowed values changed", name)
	}
	if oa.Description != na.Description {
		d.additive("`%s`: description changed", name)
	}
}

// stricter reports whether moving between modes can invalidate a configuration:
// becoming required, or no longer accepting a configured value.
func stricter(from, to engine.Mode) bool {
	if to == engine.MODE_REQUIRED {
		return true
	}
	return to == engine.MODE_COMPUTED && from != engine.MODE_COMPUTED
}

var modeName = map[engine.Mode]string{
	engine.MODE_REQUIRED:          "required",
	engine.MODE_OPTIONAL:          "optional",
	engine.MODE_OPTIONAL_COMPUTED: "optional+computed",
	engine.MODE_COMPUTED:          "computed",
}

// Markdown renders the diff for a pull request body. Names come from the
// upstream model, so backticks are dropped from them before rendering.
func (d *SurfaceDiff) Markdown() string {
	var b strings.Builder
	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "### %s\n\n", title)
		for _, it := range items {
			fmt.Fprintf(&b, "- %s\n", sanitizeLine(it))
		}
		b.WriteString("\n")
	}
	section("Breaking changes", d.Breaking)
	section("Additions and other changes", d.Additive)
	if b.Len() == 0 {
		return "No change to the generated Terraform surface.\n"
	}
	return b.String()
}

// sanitizeLine keeps a line printable and bounded. The backticks it keeps are
// the ones the diff itself added: names match WIRE_NAME_PATTERN and enum
// values have their backticks stripped.
func sanitizeLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// stripBackticks removes backticks from upstream text rendered as Markdown.
func stripBackticks(s string) string {
	return strings.ReplaceAll(s, "`", "")
}

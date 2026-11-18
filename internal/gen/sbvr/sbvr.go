// Package sbvr reads the subset of SBVR (Semantics of Business Vocabulary and Rules) that balena's models use, and
// derives tables, columns and column constraints from it. It is a from-scratch reader, not a port of the official
// grammar, so it fails on any line it does not understand and its output is cross-checked against the generated
// TypeScript models before it is trusted.
package sbvr

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Table is a table derived from a term, a term form or a many-to-many fact type.
type Table struct {
	// Name is the resource name: the term or term form with spaces replaced by underscores, or the canonical name of
	// an implicit link table.
	Name string
	// Canonical is the canonical resource name (fact type form for term forms).
	Canonical string
	// Link marks a table derived from a fact type (term form or implicit many-to-many).
	Link   bool
	Fields []*Field
	// Aliases are extra reference names a link table answers to (synonymous forms and canonical expansions).
	Aliases []string
	// Unsupported lists necessities the reader recognised but cannot turn into a column constraint.
	Unsupported []string
}

// Field returns the named field.
func (t *Table) Field(name string) *Field {
	for _, f := range t.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// Field is a column derived from a fact type.
type Field struct {
	Name string
	// Type is the SBVR primitive type ("Short Text", "Boolean", ...), empty for references.
	Type string
	// Target is the referenced table for references.
	Target    string
	Required  bool
	Unique    bool
	MinLength int
	MaxLength int
	Enum      []string
	Notes     []string
	// Key marks a role of a link table: the roles together are the table's natural key.
	Key bool
	// term is the SBVR term the field was derived from.
	term string
}

// Model is a parsed SBVR file.
type Model struct {
	Tables map[string]*Table
	Rules  []string
}

// SortedTables returns the tables ordered by name.
func (m *Model) SortedTables() []*Table {
	out := make([]*Table, 0, len(m.Tables))
	for _, t := range m.Tables {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type term struct {
	name      string
	primitive string
	concept   string
	// form is the fact type a term form was declared on.
	form *fact
}

type fact struct {
	line         int
	text         string
	necessities  []string
	definition   []string
	notes        []string
	synonyms     []string
	termForm     string
	parts        []part
	synonymParts [][]part
}

type part struct {
	term string
	verb string
}

func (p part) isTerm() bool { return p.term != "" }

var (
	LINE_RE       = regexp.MustCompile(`^(Vocabulary|Term|Fact [tT]ype|Rule|Necessity|Definition|Note|Synonymous [fF]orm|Term [fF]orm|Database Table Name|Reference Scheme|Reference Type|Concept Type):\s*(.*)$`)
	QUALIFIER_RE  = regexp.MustCompile(`\s*\((Auth|balena)\)`)
	PRIMITIVE_RE  = regexp.MustCompile(`^(.+) \(Type\)$`)
	DEFINITION_RE = regexp.MustCompile(`"([^"]*)"`)
	// The length necessities the reader understands. All of them bound a column unconditionally; the condition of
	// LENGTH_IF_SET_RE only says the column is set, which a length implies.
	//   each <subject> has a <term> [that is represented by a Text (Type)] that has a Length (Type) that is <bounds>
	//   each <subject> that has a <term>, has a <term> [...] that has a Length (Type) that is <bounds>
	//   each <term> of a <subject>, has a Length (Type) that is <bounds>
	LENGTH_PLAIN_RE  = regexp.MustCompile(`^each (.+?) (has|needs) an? (.+?)(?: that is represented by a text \(type\))? that has a length \(type\) that is (.+)$`)
	LENGTH_IF_SET_RE = regexp.MustCompile(`^each (.+?) that (has|needs) an? (.+?), (has|needs) an? (.+?)(?: that is represented by a text \(type\))? that has a length \(type\) that is (.+)$`)
	LENGTH_OF_RE     = regexp.MustCompile(`^each (.+?) of an? (.+?), has a length \(type\) that is (.+)$`)
	BOUND_RE         = regexp.MustCompile(`^(greater than or equal to|greater than|less than or equal to|less than|equal to) ([0-9]+)$`)
	CARDINALITY      = regexp.MustCompile(`(exactly|at most) one `)
)

// Parse reads an SBVR file.
func Parse(src string) (*Model, error) {
	terms := map[string]*term{}
	var facts []*fact
	var rules []string
	var lastTerm *term
	var lastFact *fact
	for i, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		m := LINE_RE.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("line %d: unrecognised SBVR line %q", i+1, line)
		}
		key, val := strings.ToLower(m[1]), strings.TrimSpace(m[2])
		switch key {
		case "vocabulary", "database table name", "reference scheme", "reference type":
			continue
		case "term":
			name := normalise(val)
			if _, dup := terms[name]; dup {
				return nil, fmt.Errorf("line %d: term %q declared twice", i+1, name)
			}
			lastTerm = &term{name: name}
			terms[name] = lastTerm
			lastFact = nil
		case "concept type":
			if lastTerm == nil || lastFact != nil {
				return nil, fmt.Errorf("line %d: concept type outside a term", i+1)
			}
			if pm := PRIMITIVE_RE.FindStringSubmatch(val); pm != nil {
				lastTerm.primitive = pm[1]
			} else {
				lastTerm.concept = normalise(val)
			}
		case "fact type":
			lastFact = &fact{line: i + 1, text: val}
			facts = append(facts, lastFact)
		case "rule":
			rules = append(rules, val)
			lastFact = nil
		case "necessity", "definition", "note", "synonymous form", "term form":
			if lastFact == nil {
				return nil, fmt.Errorf("line %d: %s outside a fact type", i+1, key)
			}
			switch key {
			case "necessity":
				lastFact.necessities = append(lastFact.necessities, val)
			case "definition":
				for _, dm := range DEFINITION_RE.FindAllStringSubmatch(val, -1) {
					lastFact.definition = append(lastFact.definition, dm[1])
				}
			case "note":
				lastFact.notes = append(lastFact.notes, val)
			case "synonymous form":
				lastFact.synonyms = append(lastFact.synonyms, val)
			case "term form":
				name := normalise(val)
				if _, dup := terms[name]; dup {
					return nil, fmt.Errorf("line %d: term form %q clashes with a term", i+1, name)
				}
				lastFact.termForm = name
				terms[name] = &term{name: name, form: lastFact}
			}
		}
	}

	names := make([]string, 0, len(terms))
	for n := range terms {
		names = append(names, n)
	}
	// Longest first, so "device type" wins over "device".
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})
	for _, f := range facts {
		parts, err := tokenize(f.text, names)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", f.line, err)
		}
		f.parts = parts
		for _, s := range f.synonyms {
			sp, err := tokenize(s, names)
			if err != nil {
				return nil, fmt.Errorf("line %d: synonymous form: %w", f.line, err)
			}
			f.synonymParts = append(f.synonymParts, sp)
		}
	}
	b := &builder{terms: terms, names: names, model: &Model{Tables: map[string]*Table{}, Rules: rules}}
	if err := b.build(facts); err != nil {
		return nil, err
	}
	return b.model, nil
}

// normalise strips vocabulary qualifiers and collapses whitespace.
func normalise(s string) string {
	return strings.Join(strings.Fields(QUALIFIER_RE.ReplaceAllString(s, "")), " ")
}

// underscore turns an SBVR name into a model name.
func underscore(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), " ", "_")
}

// tokenize splits a fact type into alternating terms and verbs. Bracketed text is always a verb; numbered terms
// (application1) resolve to their base term.
func tokenize(text string, names []string) ([]part, error) {
	text = normalise(text)
	var parts []part
	var verb strings.Builder
	flushVerb := func() {
		if v := strings.TrimSpace(verb.String()); v != "" {
			parts = append(parts, part{verb: v})
		}
		verb.Reset()
	}
	for i := 0; i < len(text); {
		if text[i] == '[' {
			end := strings.IndexByte(text[i:], ']')
			if end < 0 {
				return nil, fmt.Errorf("unclosed bracket in %q", text)
			}
			flushVerb()
			parts = append(parts, part{verb: strings.TrimSpace(text[i+1 : i+end])})
			i += end + 1
			continue
		}
		matched := ""
		next := i
		if i == 0 || text[i-1] == ' ' {
			for _, n := range names {
				if !strings.HasPrefix(text[i:], n) {
					continue
				}
				j := i + len(n)
				for j < len(text) && text[j] >= '0' && text[j] <= '9' {
					j++
				}
				if j == len(text) || text[j] == ' ' {
					matched, next = n, j
					break
				}
			}
		}
		if matched != "" {
			flushVerb()
			parts = append(parts, part{term: matched})
			i = next
			continue
		}
		verb.WriteByte(text[i])
		i++
	}
	flushVerb()
	if len(parts) == 0 || !parts[0].isTerm() {
		return nil, fmt.Errorf("fact type %q does not start with a term", text)
	}
	for k := 1; k < len(parts); k++ {
		if parts[k].isTerm() == parts[k-1].isTerm() {
			return nil, fmt.Errorf("fact type %q does not alternate terms and verbs", text)
		}
	}
	return parts, nil
}

type builder struct {
	terms map[string]*term
	names []string
	model *Model
}

func (b *builder) isPrimitive(t string) bool {
	return b.terms[t] != nil && b.terms[t].primitive != ""
}

// canonical returns the canonical model name of a term: term forms expand to their fact type.
func (b *builder) canonical(t string) string {
	if tt := b.terms[t]; tt != nil && tt.form != nil {
		return b.factCanonical(tt.form.parts)
	}
	return underscore(t)
}

func (b *builder) factCanonical(parts []part) string {
	var out []string
	for _, p := range parts {
		if p.isTerm() {
			out = append(out, b.canonical(p.term))
		} else {
			out = append(out, underscore(p.verb))
		}
	}
	return strings.Join(out, "__")
}

func (b *builder) table(t string) *Table {
	name := underscore(t)
	if tb, ok := b.model.Tables[name]; ok {
		return tb
	}
	tb := &Table{Name: name, Canonical: b.canonical(t)}
	if tt := b.terms[t]; tt != nil && tt.form != nil {
		tb.Link = true
	}
	b.model.Tables[name] = tb
	return tb
}

// fieldName names the column a role gets: the bare term after "has" or in first position, otherwise verb__term.
func fieldName(verb, t string) string {
	if verb == "" || verb == "has" {
		return underscore(t)
	}
	return underscore(verb) + "__" + underscore(t)
}

func (b *builder) addField(tb *Table, f *Field) *Field {
	if existing := tb.Field(f.Name); existing != nil {
		existing.Required = existing.Required || f.Required
		existing.Key = existing.Key || f.Key
		return existing
	}
	tb.Fields = append(tb.Fields, f)
	return f
}

func (b *builder) roleField(name, t string) *Field {
	f := &Field{Name: name, term: t}
	if b.isPrimitive(t) {
		f.Type = b.terms[t].primitive
	} else {
		f.Target = underscore(t)
	}
	return f
}

func (b *builder) build(facts []*fact) error {
	// Every non-primitive term is a table; a concept type that is a table becomes a required reference.
	for _, n := range b.names {
		t := b.terms[n]
		if t.primitive != "" {
			continue
		}
		tb := b.table(n)
		if t.concept != "" {
			if b.terms[t.concept] == nil {
				return fmt.Errorf("term %q has unknown concept type %q", n, t.concept)
			}
			if !b.isPrimitive(t.concept) {
				b.addField(tb, &Field{Name: underscore(t.concept), Target: underscore(t.concept), Required: true, term: t.concept})
			}
		}
	}
	for _, f := range facts {
		if err := b.fact(f); err != nil {
			return fmt.Errorf("line %d (%s): %w", f.line, f.text, err)
		}
	}
	for _, tb := range b.model.Tables {
		sort.Strings(tb.Aliases)
	}
	return nil
}

func (b *builder) fact(f *fact) error {
	var termParts []part
	var verbs []string
	for k, p := range f.parts {
		if p.isTerm() {
			termParts = append(termParts, p)
			if k > 0 {
				verbs = append(verbs, f.parts[k-1].verb)
			} else {
				verbs = append(verbs, "")
			}
		}
	}
	subject := termParts[0].term
	if b.isPrimitive(subject) {
		return fmt.Errorf("fact type starts with primitive term %q", subject)
	}
	if f.termForm != "" {
		return b.linkTable(b.table(f.termForm), f, termParts, verbs)
	}
	switch len(termParts) {
	case 1:
		// Unary fact type: a boolean column named after the verb.
		if len(f.parts) != 2 {
			return fmt.Errorf("unary fact type has %d parts", len(f.parts))
		}
		fld := b.addField(b.table(subject), &Field{Name: underscore(f.parts[1].verb), Type: "Boolean", Required: true})
		fld.Notes = append(fld.Notes, f.notes...)
		return nil
	case 2:
		object := termParts[1].term
		card := b.cardinality(f, subject, object)
		if card == "" {
			// Many-to-many without a term form: an implicit link table named after the fact type.
			name := b.factCanonical(f.parts)
			tb, ok := b.model.Tables[name]
			if !ok {
				tb = &Table{Name: name, Canonical: name, Link: true}
				b.model.Tables[name] = tb
			}
			return b.linkTable(tb, f, termParts, verbs)
		}
		fld := b.addField(b.table(subject), b.roleField(fieldName(verbs[1], object), object))
		fld.Required = fld.Required || card == "exactly"
		fld.Enum = append(fld.Enum, f.definition...)
		fld.Notes = append(fld.Notes, f.notes...)
		tb := b.table(subject)
		for _, n := range f.necessities {
			b.necessity(tb, n, subject, []*Field{fld})
		}
		return nil
	default:
		return fmt.Errorf("n-ary fact type without a term form")
	}
}

func (b *builder) linkTable(tb *Table, f *fact, termParts []part, verbs []string) error {
	var roles []*Field
	for k, p := range termParts {
		fld := b.addField(tb, b.roleField(fieldName(verbs[k], p.term), p.term))
		fld.Required = true
		fld.Key = true
		roles = append(roles, fld)
		// A role that refers to a term form also answers to the canonical (expanded) name.
		if !b.isPrimitive(p.term) {
			if alias := fieldName(verbs[k], b.canonical(p.term)); alias != fld.Name {
				tb.Aliases = appendUnique(tb.Aliases, alias)
			}
		}
	}
	// Synonymous forms add alias roles.
	for _, sp := range f.synonymParts {
		var sverb string
		first := true
		for _, p := range sp {
			if !p.isTerm() {
				sverb = p.verb
				continue
			}
			v := sverb
			if first {
				v = ""
				first = false
			}
			for _, name := range []string{fieldName(v, p.term), fieldName(v, b.canonical(p.term))} {
				if tb.Field(name) == nil {
					tb.Aliases = appendUnique(tb.Aliases, name)
				}
			}
		}
	}
	if len(f.definition) > 0 {
		return fmt.Errorf("definition on a link table")
	}
	tb.Link = true
	for _, n := range f.necessities {
		b.necessity(tb, n, strings.ReplaceAll(tb.Name, "_", " "), roles)
	}
	return nil
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// cardinality returns "exactly" or "at most" when a necessity bounds how many objects a subject has, "" otherwise.
func (b *builder) cardinality(f *fact, subject, object string) string {
	for _, n := range f.necessities {
		a, card, c := b.cardinalityTerms(n)
		if a == subject && c == object {
			return card
		}
	}
	return ""
}

// cardinalityTerms parses "each <a> ... (exactly|at most) one <c>".
func (b *builder) cardinalityTerms(n string) (string, string, string) {
	text := strings.ToLower(normalise(n))
	text = strings.TrimSuffix(text, ".")
	if !strings.HasPrefix(text, "each ") || strings.Contains(text, "length (type)") {
		return "", "", ""
	}
	loc := CARDINALITY.FindStringSubmatchIndex(text)
	if loc == nil {
		return "", "", ""
	}
	a := b.termAt(text[len("each "):])
	c := b.termAt(text[loc[1]:])
	if a == "" || c == "" {
		return "", "", ""
	}
	// The object must be the whole remainder (no trailing clauses such as "that is host").
	rest := strings.TrimSpace(text[loc[1]+len(c):])
	for rest != "" && rest[0] >= '0' && rest[0] <= '9' {
		rest = rest[1:]
	}
	if rest != "" {
		return "", "", ""
	}
	return a, text[loc[2]:loc[3]], c
}

// termAt returns the longest term at the start of s (ignoring a numeric suffix).
func (b *builder) termAt(s string) string {
	for _, n := range b.names {
		if strings.HasPrefix(s, n) {
			j := len(n)
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j == len(s) || s[j] == ' ' || s[j] == ',' {
				return n
			}
		}
	}
	return ""
}

// necessity applies a necessity to the fields of a fact type: required, unique and length bounds.
func (b *builder) necessity(tb *Table, n, subject string, fields []*Field) {
	text := strings.ToLower(normalise(n))
	text = strings.TrimSuffix(text, ".")
	if strings.Contains(text, "length (type)") {
		target, bounds := lengthTarget(text, subject, fields)
		if target == nil {
			tb.Unsupported = append(tb.Unsupported, n)
			return
		}
		minLength, maxLength := target.MinLength, target.MaxLength
		for _, clause := range strings.Split(bounds, " and is ") {
			bm := BOUND_RE.FindStringSubmatch(strings.TrimSpace(clause))
			if bm == nil {
				tb.Unsupported = append(tb.Unsupported, n)
				return
			}
			v, _ := strconv.Atoi(bm[2])
			switch bm[1] {
			case "greater than or equal to":
				minLength = maxInt(minLength, v)
			case "greater than":
				minLength = maxInt(minLength, v+1)
			case "less than or equal to":
				maxLength = minPositive(maxLength, v)
			case "less than":
				maxLength = minPositive(maxLength, v-1)
			case "equal to":
				minLength = maxInt(minLength, v)
				maxLength = minPositive(maxLength, v)
			}
		}
		target.MinLength, target.MaxLength = minLength, maxLength
		return
	}
	a, card, c := b.cardinalityTerms(n)
	if a == "" {
		tb.Unsupported = append(tb.Unsupported, n)
		return
	}
	for _, f := range fields {
		switch {
		case a == subject && f.term == c:
			if card == "exactly" {
				f.Required = true
			}
			return
		case f.term == a && (c == subject || underscore(c) == tb.Name || b.isSubjectRole(c, fields, f)):
			f.Unique = true
			return
		}
	}
	tb.Unsupported = append(tb.Unsupported, n)
}

// isSubjectRole reports whether t is another role of the same link table (e.g. "each alias references exactly one
// device type" makes the alias role unique).
func (b *builder) isSubjectRole(t string, fields []*Field, self *Field) bool {
	for _, f := range fields {
		if f != self && f.term == t {
			return true
		}
	}
	return false
}

// lengthTarget returns the primitive field of the fact type a length necessity bounds and the bounds text, when the
// necessity has one of the unconditional forms the reader understands (see LENGTH_PLAIN_RE); nil otherwise.
func lengthTarget(text, subject string, fields []*Field) (*Field, string) {
	var term, bounds string
	if m := LENGTH_IF_SET_RE.FindStringSubmatch(text); m != nil {
		if m[1] != subject || m[2] != m[4] || m[3] != m[5] {
			return nil, ""
		}
		term, bounds = m[3], m[6]
	} else if m := LENGTH_PLAIN_RE.FindStringSubmatch(text); m != nil {
		if m[1] != subject {
			return nil, ""
		}
		term, bounds = m[3], m[4]
	} else if m := LENGTH_OF_RE.FindStringSubmatch(text); m != nil {
		if m[2] != subject {
			return nil, ""
		}
		term, bounds = m[1], m[3]
	} else {
		return nil, ""
	}
	for _, f := range fields {
		if f.Type != "" && f.term == term {
			return f, bounds
		}
	}
	return nil, ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minPositive(current, v int) int {
	if current == 0 || v < current {
		return v
	}
	return current
}

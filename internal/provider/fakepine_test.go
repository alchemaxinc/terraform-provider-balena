//go:build !integration

package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakePine is an in-memory Pine.js server for the tables of the generated specs. It reproduces the quirks the
// provider relies on: the {"d": [...]} envelope, references read as {"__id": N} and written as N, an empty
// collection for a missing id, unexpanded references in POST answers, and 401 for a $select of an unknown column.
type fakePine struct {
	t      *testing.T
	mu     sync.Mutex
	fields map[string]map[string]bool   // table -> known columns
	refs   map[string]map[string]string // table -> reference column -> referenced table
	rows   map[string]map[int64]map[string]any
	nextID int64
	// lambda evaluates `.../any(...)` filter terms, which the fake does not parse.
	lambda func(table, term string, row map[string]any) bool
	// deleteStatus forces the status of DELETE on a table.
	deleteStatus map[string]int
	// getByIDStatus forces the status of GET by id on a table.
	getByIDStatus map[string]int
	// canonical rewrites written values of a column as the API stores them, by column name.
	canonical map[string]func(any) any
	// afterWrite derives server-set columns of a table after every POST or PATCH.
	afterWrite map[string]func(row map[string]any)
	requests   []fakeRequest
}

type fakeRequest struct {
	Method string
	Table  string
	ID     int64
	Query  map[string]string
	Body   map[string]any
}

var FAKE_PATH_RE = regexp.MustCompile(`^/([a-z0-9]+)/([a-z_]+)(?:\(([0-9]+)\))?$`)

func newFakePine(t *testing.T) *fakePine {
	f := &fakePine{
		t:             t,
		fields:        map[string]map[string]bool{},
		refs:          map[string]map[string]string{},
		rows:          map[string]map[int64]map[string]any{},
		nextID:        1000,
		deleteStatus:  map[string]int{},
		getByIDStatus: map[string]int{},
		canonical:     map[string]func(any) any{},
		afterWrite:    map[string]func(row map[string]any){},
	}
	known := func(table, field string) {
		if f.fields[table] == nil {
			f.fields[table] = map[string]bool{"id": true}
			f.refs[table] = map[string]string{}
			f.rows[table] = map[int64]map[string]any{}
		}
		f.fields[table][field] = true
	}
	for _, s := range GENERATED_SPECS {
		for _, a := range s.Attributes {
			known(s.Table, a.Field)
			if a.Encoding == ENC_REF || a.Encoding == ENC_REF_BY {
				f.refs[s.Table][a.Field] = a.RefTable
				known(a.RefTable, "id")
			}
			if a.Encoding == ENC_REF_BY {
				known(a.RefTable, a.RefField)
			}
		}
	}
	for _, uses := range GENERATED_HOOK_USES {
		for _, use := range uses {
			table, field, _ := strings.Cut(use, ".")
			known(table, field)
		}
	}
	// References hooks follow that no spec exposes.
	f.refs["release"]["belongs_to__application"] = "application"
	f.refs["application"]["is_for__device_type"] = "device_type"
	f.refs["device"]["is_of__device_type"] = "device_type"
	return f
}

// seed stores a row and returns its id. References are given as ids.
func (f *fakePine) seed(table string, values map[string]any) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows[table] == nil {
		f.t.Fatalf("seed: unknown table %s", table)
	}
	f.nextID++
	row := map[string]any{"id": f.nextID}
	for k, v := range values {
		row[k] = v
	}
	f.rows[table][f.nextID] = row
	return f.nextID
}

func (f *fakePine) row(table string, id int64) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows[table][id]
}

func (f *fakePine) count(table string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows[table])
}

// patches returns the bodies of every PATCH on a table.
func (f *fakePine) patches(table string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, r := range f.requests {
		if r.Method == http.MethodPatch && r.Table == table {
			out = append(out, r.Body)
		}
	}
	return out
}

func (f *fakePine) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(f.serve))
}

func (f *fakePine) fail(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg))
}

func (f *fakePine) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := FAKE_PATH_RE.FindStringSubmatch(r.URL.Path)
	if m == nil || f.fields[m[2]] == nil {
		f.fail(w, http.StatusUnauthorized, "")
		return
	}
	table := m[2]
	var id int64
	if m[3] != "" {
		id, _ = strconv.ParseInt(m[3], 10, 64)
	}
	query := map[string]string{}
	for k, v := range r.URL.Query() {
		query[k] = v[0]
	}
	var body map[string]any
	if data, _ := io.ReadAll(r.Body); len(data) > 0 {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			f.fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	f.requests = append(f.requests, fakeRequest{Method: r.Method, Table: table, ID: id, Query: query, Body: body})
	for k := range body {
		if !f.fields[table][k] {
			f.fail(w, http.StatusBadRequest, "unknown column "+k)
			return
		}
	}
	switch {
	case r.Method == http.MethodGet && m[3] != "" && f.getByIDStatus[table] != 0:
		f.fail(w, f.getByIDStatus[table], "forced failure")
	case r.Method == http.MethodGet:
		f.get(w, table, id, m[3] != "", query)
	case r.Method == http.MethodPost && m[3] == "":
		f.nextID++
		row := map[string]any{"id": f.nextID, "created_at": "2026-01-02T03:04:05.000Z"}
		for k := range f.fields[table] {
			if _, ok := row[k]; !ok {
				row[k] = nil
			}
		}
		for k, v := range body {
			row[k] = f.store(k, v)
		}
		if derive := f.afterWrite[table]; derive != nil {
			derive(row)
		}
		f.rows[table][f.nextID] = row
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(f.render(table, row, nil, nil))
	case r.Method == http.MethodPatch && m[3] != "":
		row := f.rows[table][id]
		if row == nil {
			f.fail(w, http.StatusNotFound, "")
			return
		}
		for k, v := range body {
			row[k] = f.store(k, v)
		}
		if derive := f.afterWrite[table]; derive != nil {
			derive(row)
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodDelete && m[3] != "":
		if status := f.deleteStatus[table]; status != 0 {
			f.fail(w, status, "")
			return
		}
		if f.rows[table][id] == nil {
			f.fail(w, http.StatusNotFound, "")
			return
		}
		delete(f.rows[table], id)
		w.WriteHeader(http.StatusOK)
	default:
		f.fail(w, http.StatusMethodNotAllowed, "")
	}
}

// store normalises a written value and applies the column's canonical form.
func (f *fakePine) store(column string, v any) any {
	v = normalise(v)
	if c := f.canonical[column]; c != nil && v != nil {
		v = c(v)
	}
	return v
}

// normalise turns JSON numbers into int64 (or float64).
func normalise(v any) any {
	if n, ok := v.(json.Number); ok {
		if i, err := n.Int64(); err == nil {
			return i
		}
		fl, _ := n.Float64()
		return fl
	}
	return v
}

func splitTop(s, sep string) []string {
	var out []string
	depth, start, inQuote := 0, 0, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(s[i:], sep) {
			out = append(out, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	return append(out, s[start:])
}

var FAKE_EQ_RE = regexp.MustCompile(`^([a-z_]+) eq (.+)$`)

func parseLiteral(s string) any {
	switch {
	case s == "null":
		return nil
	case s == "true":
		return true
	case s == "false":
		return false
	case strings.HasPrefix(s, "'"):
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	fl, _ := strconv.ParseFloat(s, 64)
	return fl
}

func (f *fakePine) matches(table, filter string, row map[string]any) (bool, error) {
	if filter == "" {
		return true, nil
	}
	for _, term := range splitTop(filter, " and ") {
		term = strings.TrimSpace(term)
		if strings.Contains(term, "/any(") {
			if f.lambda == nil {
				return false, fmt.Errorf("unsupported filter %q", term)
			}
			if !f.lambda(table, term, row) {
				return false, nil
			}
			continue
		}
		m := FAKE_EQ_RE.FindStringSubmatch(term)
		if m == nil || !f.fields[table][m[1]] {
			return false, fmt.Errorf("unsupported filter term %q", term)
		}
		if fmt.Sprint(row[m[1]]) != fmt.Sprint(parseLiteral(m[2])) {
			return false, nil
		}
	}
	return true, nil
}

func (f *fakePine) get(w http.ResponseWriter, table string, id int64, byID bool, query map[string]string) {
	var sel []string
	if s := query["$select"]; s != "" {
		sel = strings.Split(s, ",")
		for _, field := range sel {
			if !f.fields[table][field] {
				f.fail(w, http.StatusUnauthorized, "")
				return
			}
		}
	}
	expands := map[string][]string{}
	if e := query["$expand"]; e != "" {
		for _, part := range splitTop(e, ",") {
			field, rest, _ := strings.Cut(part, "($select=")
			if f.refs[table][field] == "" {
				f.fail(w, http.StatusUnauthorized, "")
				return
			}
			expands[field] = strings.Split(strings.TrimSuffix(rest, ")"), ",")
		}
	}
	var ids []int64
	if byID {
		if f.rows[table][id] != nil {
			ids = append(ids, id)
		}
	} else {
		for rid := range f.rows[table] {
			ids = append(ids, rid)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	}
	out := []any{}
	for _, rid := range ids {
		row := f.rows[table][rid]
		ok, err := f.matches(table, query["$filter"], row)
		if err != nil {
			f.fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if ok {
			out = append(out, f.render(table, row, sel, expands))
		}
	}
	if top, err := strconv.Atoi(query["$top"]); err == nil && len(out) > top {
		out = out[:top]
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"d": out})
}

func (f *fakePine) render(table string, row map[string]any, sel []string, expands map[string][]string) map[string]any {
	out := map[string]any{}
	fields := sel
	if fields == nil {
		for k := range row {
			fields = append(fields, k)
		}
	}
	for _, k := range fields {
		v := row[k]
		if target := f.refs[table][k]; target != "" && v != nil {
			v = map[string]any{"__id": v}
		}
		out[k] = v
	}
	for field, cols := range expands {
		target := f.refs[table][field]
		ref, _ := row[field].(int64)
		if refRow := f.rows[target][ref]; refRow != nil {
			sub := map[string]any{}
			for _, c := range cols {
				sub[c] = refRow[c]
			}
			out[field] = []any{sub}
		} else {
			out[field] = []any{}
		}
	}
	return out
}

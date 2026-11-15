package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// tfType returns the terraform-plugin-go type of an attribute.
func (a Attribute) tfType() tftypes.Type {
	switch a.Type {
	case TYPE_INT64:
		return tftypes.Number
	case TYPE_BOOL:
		return tftypes.Bool
	default:
		return tftypes.String
	}
}

func (a Attribute) null() tftypes.Value {
	return tftypes.NewValue(a.tfType(), nil)
}

// fromWire decodes a wire value. A missing or null value becomes a null value.
func (a Attribute) fromWire(raw json.RawMessage) (tftypes.Value, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return a.null(), nil
	}
	switch a.Type {
	case TYPE_INT64:
		n, err := decodeInt(raw, a.Codec == CODEC_FOREIGN_KEY)
		if err != nil {
			return tftypes.Value{}, fmt.Errorf("%s: %w", a.Wire, err)
		}
		return tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(n)), nil
	case TYPE_BOOL:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return tftypes.Value{}, fmt.Errorf("%s: %w", a.Wire, err)
		}
		return tftypes.NewValue(tftypes.Bool, b), nil
	default:
		if a.Codec == CODEC_JSON {
			var buf bytes.Buffer
			if err := json.Compact(&buf, raw); err != nil {
				return tftypes.Value{}, fmt.Errorf("%s: %w", a.Wire, err)
			}
			return tftypes.NewValue(tftypes.String, buf.String()), nil
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return tftypes.Value{}, fmt.Errorf("%s: %w", a.Wire, err)
		}
		return tftypes.NewValue(tftypes.String, s), nil
	}
}

// decodeInt accepts a JSON number, a numeric string (big integers), and for a
// foreign key the {"__id": N} reference form.
func decodeInt(raw json.RawMessage, foreignKey bool) (int64, error) {
	if foreignKey && raw[0] == '{' {
		var ref struct {
			ID json.RawMessage `json:"__id"`
		}
		if err := json.Unmarshal(raw, &ref); err != nil {
			return 0, err
		}
		if ref.ID == nil {
			return 0, fmt.Errorf("reference without __id: %s", raw)
		}
		return decodeInt(ref.ID, false)
	}
	s := string(raw)
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, err
		}
	}
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

// toWire encodes a known value for a request body. A null value becomes JSON null.
func (a Attribute) toWire(v tftypes.Value) (any, error) {
	if v.IsNull() {
		return nil, nil
	}
	switch a.Type {
	case TYPE_INT64:
		var f big.Float
		if err := v.As(&f); err != nil {
			return nil, err
		}
		n, acc := f.Int64()
		if acc != big.Exact {
			return nil, fmt.Errorf("%s: %s is not an integer", a.Name, f.String())
		}
		return n, nil
	case TYPE_BOOL:
		var b bool
		if err := v.As(&b); err != nil {
			return nil, err
		}
		return b, nil
	default:
		var s string
		if err := v.As(&s); err != nil {
			return nil, err
		}
		if a.Codec == CODEC_JSON {
			if !json.Valid([]byte(s)) {
				return nil, fmt.Errorf("%s: value is not valid JSON", a.Name)
			}
			return json.RawMessage(s), nil
		}
		return s, nil
	}
}

// filterLiteral encodes a known, non-null value as an OData literal.
func (a Attribute) filterLiteral(v tftypes.Value) (string, error) {
	w, err := a.toWire(v)
	if err != nil {
		return "", err
	}
	switch x := w.(type) {
	case int64:
		return strconv.FormatInt(x, 10), nil
	case bool:
		return strconv.FormatBool(x), nil
	case string:
		return "'" + strings.ReplaceAll(x, "'", "''") + "'", nil
	}
	return "", fmt.Errorf("%s: cannot filter on this attribute", a.Name)
}

// sameJSON reports whether two strings hold equal JSON documents.
func sameJSON(x, y string) bool {
	var a, b any
	if json.Unmarshal([]byte(x), &a) != nil || json.Unmarshal([]byte(y), &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

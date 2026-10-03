package application

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Values is the transport-neutral representation of callback values. JSON
// bytes preserve Feishu's string, number, boolean and list values without
// leaking an SDK map[string]any into application use cases.
type Values struct {
	raw map[string]json.RawMessage
}

// ValuesFromMap is the adapter boundary for SDK callback maps.
func ValuesFromMap(values map[string]any) Values {
	if len(values) == 0 {
		return Values{}
	}
	raw := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		encoded, err := json.Marshal(value)
		if err == nil {
			raw[key] = encoded
		}
	}
	return Values{raw: raw}
}

// Map converts values back at an external adapter boundary. Application code
// should use the typed accessors below instead.
func (v Values) Map() map[string]any {
	if len(v.raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(v.raw))
	for key, raw := range v.raw {
		var value any
		if err := json.Unmarshal(raw, &value); err == nil {
			out[key] = value
		}
	}
	return out
}

func (v Values) Empty() bool { return len(v.raw) == 0 }

func (v Values) String(key string) (string, bool) {
	var value string
	raw, ok := v.raw[key]
	if !ok || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func (v Values) Bool(key string) (bool, bool) {
	var value bool
	raw, ok := v.raw[key]
	if !ok || json.Unmarshal(raw, &value) != nil {
		return false, false
	}
	return value, true
}

func (v Values) Int(key string) (int, bool) {
	raw, ok := v.raw[key]
	if !ok {
		return 0, false
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		value, err := strconv.Atoi(number.String())
		return value, err == nil
	}
	var value float64
	if json.Unmarshal(raw, &value) != nil {
		return 0, false
	}
	return int(value), true
}

func (v Values) Strings(key string) ([]string, bool) {
	var values []string
	raw, ok := v.raw[key]
	if !ok || json.Unmarshal(raw, &values) != nil {
		return nil, false
	}
	return values, true
}

func (v *Values) SetString(key, value string) {
	if v.raw == nil {
		v.raw = make(map[string]json.RawMessage)
	}
	encoded, _ := json.Marshal(value)
	v.raw[key] = encoded
}

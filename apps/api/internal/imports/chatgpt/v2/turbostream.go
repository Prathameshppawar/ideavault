package chatgptv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// React Router's turbo-stream encodes a value graph as a flat JSON array:
// element 0 is the root; objects are {"_<keyIndex>": <valueIndex>}; arrays
// are lists of indices; arrays starting with a one-letter string are typed
// values (["D", ms] = Date, ["P", id] = promise, ...); negative indices are
// constants. Indices may be shared (memoized) and may form cycles.
const (
	tsHole        = -1
	tsNaN         = -2
	tsNegInf      = -3
	tsNegZero     = -4
	tsNull        = -5
	tsPosInf      = -6
	tsUndefined   = -7
	maxTurboDepth = 2000
)

// turboStream is a parsed turbo-stream payload that hydrates lazily.
type turboStream struct {
	values []any
	memo   map[int]any
}

// parseTurboStream parses the first line of a turbo-stream payload. Later
// lines (promise resolutions such as `P12:[...]`) are ignored.
func parseTurboStream(payload string) (*turboStream, error) {
	line, _, _ := strings.Cut(payload, "\n")
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") {
		return nil, errors.New("turbo-stream payload is not a JSON array")
	}
	var values []any
	if err := json.Unmarshal([]byte(line), &values); err != nil {
		return nil, fmt.Errorf("turbo-stream payload: %w", err)
	}
	if len(values) == 0 {
		return nil, errors.New("turbo-stream payload is empty")
	}
	return &turboStream{values: values, memo: make(map[int]any)}, nil
}

// root hydrates the root value.
func (t *turboStream) root() any { return t.hydrate(0, 0) }

// findObjectWithKey returns the first (lowest-index) encoded object that has
// key as a direct property, hydrated.
func (t *turboStream) findObjectWithKey(key string) (map[string]any, bool) {
	for i, v := range t.values {
		obj, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for k := range obj {
			ki, ok := encodedKeyIndex(k)
			if !ok || ki < 0 || ki >= len(t.values) {
				continue
			}
			if s, ok := t.values[ki].(string); ok && s == key {
				m, ok := t.hydrate(i, 0).(map[string]any)
				return m, ok
			}
		}
	}
	return nil, false
}

func (t *turboStream) hydrate(idx, depth int) any {
	switch idx {
	case tsHole, tsNaN, tsNegInf, tsPosInf, tsNull, tsUndefined:
		return nil
	case tsNegZero:
		return float64(0)
	}
	if idx < 0 || idx >= len(t.values) || depth > maxTurboDepth {
		return nil
	}
	if v, ok := t.memo[idx]; ok {
		return v
	}
	switch v := t.values[idx].(type) {
	case []any:
		if len(v) > 0 {
			if tag, ok := v[0].(string); ok {
				return t.typed(idx, tag, v, depth)
			}
		}
		arr := make([]any, len(v))
		t.memo[idx] = arr // before recursing: cycles resolve to this slice
		for i, e := range v {
			if n, ok := asIndex(e); ok {
				arr[i] = t.hydrate(n, depth+1)
			}
		}
		return arr
	case map[string]any:
		obj := make(map[string]any, len(v))
		t.memo[idx] = obj
		t.fillObject(obj, v, depth)
		return obj
	default:
		t.memo[idx] = v
		return v
	}
}

func (t *turboStream) fillObject(obj, encoded map[string]any, depth int) {
	for k, e := range encoded {
		ki, ok := encodedKeyIndex(k)
		if !ok {
			continue
		}
		vi, ok := asIndex(e)
		if !ok {
			continue
		}
		if key, ok := keyString(t.hydrate(ki, depth+1)); ok {
			obj[key] = t.hydrate(vi, depth+1)
		}
	}
}

// typed hydrates a tagged value. Only the tags that can carry data IdeaVault
// needs are decoded; promises, errors and plugin values become nil.
func (t *turboStream) typed(idx int, tag string, v []any, depth int) any {
	arg := func(i int) any {
		if i < len(v) {
			return v[i]
		}
		return nil
	}
	var out any
	switch tag {
	case "D": // Date (milliseconds)
		if ms, ok := arg(1).(float64); ok && !math.IsNaN(ms) && !math.IsInf(ms, 0) {
			out = time.UnixMilli(int64(ms)).UTC().Format(time.RFC3339Nano)
		}
	case "U", "B", "Y", "R": // URL, BigInt, Symbol, RegExp: keep their source text
		if s, ok := arg(1).(string); ok {
			out = s
		}
	case "S": // Set
		arr := make([]any, len(v)-1)
		t.memo[idx] = arr
		for i, e := range v[1:] {
			if n, ok := asIndex(e); ok {
				arr[i] = t.hydrate(n, depth+1)
			}
		}
		return arr
	case "M": // Map
		obj := map[string]any{}
		t.memo[idx] = obj
		for i := 1; i+1 < len(v); i += 2 {
			ki, ok1 := asIndex(v[i])
			vi, ok2 := asIndex(v[i+1])
			if !ok1 || !ok2 {
				continue
			}
			if key, ok := keyString(t.hydrate(ki, depth+1)); ok {
				obj[key] = t.hydrate(vi, depth+1)
			}
		}
		return obj
	case "N": // null-prototype object
		obj := map[string]any{}
		t.memo[idx] = obj
		if enc, ok := arg(1).(map[string]any); ok {
			t.fillObject(obj, enc, depth)
		}
		return obj
	case "Z": // previously resolved value
		if n, ok := asIndex(arg(1)); ok && n != idx {
			t.memo[idx] = nil // break self-reference loops
			out = t.hydrate(n, depth+1)
		}
	default: // "P" promise, "E" error, "W", plugins...
	}
	t.memo[idx] = out
	return out
}

// encodedKeyIndex parses an encoded object key "_<n>" (or "<n>").
func encodedKeyIndex(k string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(k, "_"))
	return n, err == nil
}

func asIndex(e any) (int, bool) {
	f, ok := e.(float64)
	if !ok || f != math.Trunc(f) || f < math.MinInt32 || f > math.MaxInt32 {
		return 0, false
	}
	return int(f), true
}

// keyString renders a hydrated object key; missing (nil) keys are dropped.
func keyString(v any) (string, bool) {
	switch k := v.(type) {
	case string:
		return k, true
	case float64:
		return strconv.FormatFloat(k, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(k), true
	case nil:
		return "", false
	default:
		return fmt.Sprint(k), true
	}
}

// Limits for re-encoding hydrated values (shared references can expand).
var (
	errMarshalTooLarge = errors.New("decoded share payload is too large")
	errMarshalTooDeep  = errors.New("decoded share payload is nested too deeply")
)

// marshalBounded encodes a hydrated value as JSON, failing when the output
// exceeds limit bytes or nesting exceeds maxTurboDepth. Shared references are
// expanded, so a hostile payload could otherwise blow up exponentially.
func marshalBounded(v any, limit int) ([]byte, error) {
	var buf bytes.Buffer
	var enc func(v any, depth int) error
	enc = func(v any, depth int) error {
		if depth > maxTurboDepth {
			return errMarshalTooDeep
		}
		if buf.Len() > limit {
			return errMarshalTooLarge
		}
		switch x := v.(type) {
		case nil:
			buf.WriteString("null")
		case bool:
			buf.WriteString(strconv.FormatBool(x))
		case float64:
			if math.IsNaN(x) || math.IsInf(x, 0) {
				buf.WriteString("null")
				return nil
			}
			b, _ := json.Marshal(x)
			buf.Write(b)
		case string:
			b, _ := json.Marshal(x)
			buf.Write(b)
		case []any:
			buf.WriteByte('[')
			for i, e := range x {
				if i > 0 {
					buf.WriteByte(',')
				}
				if err := enc(e, depth+1); err != nil {
					return err
				}
			}
			buf.WriteByte(']')
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			buf.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					buf.WriteByte(',')
				}
				kb, _ := json.Marshal(k)
				buf.Write(kb)
				buf.WriteByte(':')
				if err := enc(x[k], depth+1); err != nil {
					return err
				}
			}
			buf.WriteByte('}')
		default:
			b, err := json.Marshal(x)
			if err != nil {
				return err
			}
			buf.Write(b)
		}
		return nil
	}
	if err := enc(v, 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

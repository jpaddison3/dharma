package output

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// encodeTOON renders a JSON-normalized value (the map/[]interface{}/scalar tree
// you get from unmarshaling into interface{}) as TOON — a line-oriented format
// that drops repeated object keys and JSON punctuation. It is EXPERIMENTAL and
// behind --output toon.
//
// Uniform arrays of flat objects become a tabular block (header + CSV rows);
// arrays of scalars go inline; anything TOON can't express compactly (arrays of
// nested or ragged objects) falls back to inline compact JSON, so the result is
// always complete and readable even if not maximally compressed.
func encodeTOON(v interface{}) string {
	var b strings.Builder
	if obj, ok := v.(map[string]interface{}); ok {
		writeTOONObject(&b, obj, 0)
	} else {
		b.WriteString(toonScalar(v))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeTOONObject(b *strings.Builder, obj map[string]interface{}, indent int) {
	for _, k := range sortedKeys(obj) {
		writeTOONField(b, k, obj[k], indent)
	}
}

func writeTOONField(b *strings.Builder, key string, v interface{}, indent int) {
	pad := strings.Repeat("  ", indent)
	switch t := v.(type) {
	case []interface{}:
		writeTOONArray(b, key, t, indent)
	case map[string]interface{}:
		fmt.Fprintf(b, "%s%s:\n", pad, key)
		writeTOONObject(b, t, indent+1)
	default:
		fmt.Fprintf(b, "%s%s: %s\n", pad, key, toonScalar(v))
	}
}

func writeTOONArray(b *strings.Builder, key string, arr []interface{}, indent int) {
	pad := strings.Repeat("  ", indent)
	switch {
	case len(arr) == 0:
		fmt.Fprintf(b, "%s%s[0]:\n", pad, key)
	case allScalars(arr):
		cells := make([]string, len(arr))
		for i, e := range arr {
			cells[i] = toonScalar(e)
		}
		fmt.Fprintf(b, "%s%s[%d]: %s\n", pad, key, len(arr), strings.Join(cells, ","))
	default:
		if fields, ok := uniformFlatFields(arr); ok {
			fmt.Fprintf(b, "%s%s[%d]{%s}:\n", pad, key, len(arr), strings.Join(fields, ","))
			rowPad := strings.Repeat("  ", indent+1)
			for _, e := range arr {
				obj := e.(map[string]interface{})
				cells := make([]string, len(fields))
				for i, f := range fields {
					cells[i] = toonScalar(obj[f])
				}
				fmt.Fprintf(b, "%s%s\n", rowPad, strings.Join(cells, ","))
			}
			return
		}
		// Nested or ragged: keep it lossless with inline compact JSON.
		raw, _ := json.Marshal(arr)
		fmt.Fprintf(b, "%s%s: %s\n", pad, key, string(raw))
	}
}

// uniformFlatFields reports the shared field list of an array iff every element
// is an object with the same keys and only scalar values (the tabular case).
func uniformFlatFields(arr []interface{}) ([]string, bool) {
	var fields []string
	for i, e := range arr {
		obj, ok := e.(map[string]interface{})
		if !ok {
			return nil, false
		}
		keys := sortedKeys(obj)
		for _, k := range keys {
			if !isScalar(obj[k]) {
				return nil, false
			}
		}
		if i == 0 {
			fields = keys
		} else if !equalStrings(fields, keys) {
			return nil, false
		}
	}
	return fields, len(fields) > 0
}

func isScalar(v interface{}) bool {
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		return false
	}
	return true
}

func allScalars(arr []interface{}) bool {
	for _, e := range arr {
		if !isScalar(e) {
			return false
		}
	}
	return true
}

func toonScalar(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case string:
		return toonQuote(t)
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}

// toonQuote quotes a string only when leaving it bare would be ambiguous —
// it contains a delimiter, has edge whitespace, is empty, or would otherwise
// parse as a number/bool/null (notably Asana gids, which are numeric-looking
// strings that must stay strings).
func toonQuote(s string) string {
	needsQuote := s == "" ||
		strings.ContainsAny(s, ",:\n\"") ||
		s != strings.TrimSpace(s)
	if !needsQuote {
		switch s {
		case "true", "false", "null":
			needsQuote = true
		}
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			needsQuote = true
		}
	}
	if !needsQuote {
		return s
	}
	esc := strings.ReplaceAll(s, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	esc = strings.ReplaceAll(esc, "\n", `\n`)
	return `"` + esc + `"`
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

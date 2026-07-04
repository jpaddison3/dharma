package output

import (
	"encoding/json"
	"os"
	"reflect"
)

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// Print JSON-encodes v to w: indented when w is a TTY, compact when piped.
func Print(w *os.File, v interface{}) error {
	enc := json.NewEncoder(w)
	if isTerminal(w) {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

type listEnvelope struct {
	OK      bool        `json:"ok"`
	Count   int         `json:"count"`
	HasMore bool        `json:"has_more"`
	Hint    string      `json:"hint,omitempty"`
	Data    interface{} `json:"data"`
}

type objectEnvelope struct {
	OK   bool        `json:"ok"`
	Data interface{} `json:"data"`
}

// PrintList wraps a slice in the list envelope:
//
//	{"ok":true,"count":N,"has_more":bool,"hint":"...","data":[...]}
//
// count is derived from the slice length; hint is omitted when empty. A nil
// slice is normalized to [] so data is never JSON null.
func PrintList(w *os.File, items interface{}, hasMore bool, hint string) error {
	count := 0
	if items != nil {
		rv := reflect.ValueOf(items)
		if rv.Kind() == reflect.Slice {
			count = rv.Len()
			if rv.IsNil() {
				items = reflect.MakeSlice(rv.Type(), 0, 0).Interface()
			}
		}
	}
	return Print(w, listEnvelope{OK: true, Count: count, HasMore: hasMore, Hint: hint, Data: items})
}

// PrintObject wraps a single value in the object envelope: {"ok":true,"data":{...}}.
func PrintObject(w *os.File, obj interface{}) error {
	return Print(w, objectEnvelope{OK: true, Data: obj})
}

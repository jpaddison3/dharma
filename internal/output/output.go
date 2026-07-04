package output

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
)

// Format selects the output encoding for Print: "json" (default) or the
// experimental "toon". Set once from the --output flag before any command runs.
var Format = "json"

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// Print encodes v to w in the selected Format. JSON is indented on a TTY and
// compact when piped; TOON is always line-oriented.
func Print(w *os.File, v interface{}) error {
	if Format == "toon" {
		// Normalize through JSON so the TOON encoder sees a uniform
		// map/slice/scalar tree regardless of v's concrete Go type.
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var generic interface{}
		if err := json.Unmarshal(b, &generic); err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, encodeTOON(generic))
		return err
	}
	enc := json.NewEncoder(w)
	if isTerminal(w) {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

type listEnvelope struct {
	OK              bool        `json:"ok"`
	Count           int         `json:"count"`
	HasMore         bool        `json:"has_more"`
	Hint            string      `json:"hint,omitempty"`
	TruncatedFields []string    `json:"truncated_fields,omitempty"`
	Data            interface{} `json:"data"`
}

type objectEnvelope struct {
	OK   bool        `json:"ok"`
	Data interface{} `json:"data"`
}

type objectContextEnvelope struct {
	OK              bool        `json:"ok"`
	Data            interface{} `json:"data"`
	Context         interface{} `json:"context,omitempty"`
	TruncatedFields []string    `json:"truncated_fields,omitempty"`
}

// PrintList wraps a slice in the list envelope:
//
//	{"ok":true,"count":N,"has_more":bool,"hint":"...","data":[...]}
//
// count is derived from the slice length; hint is omitted when empty. A nil
// slice is normalized to [] so data is never JSON null.
func PrintList(w *os.File, items interface{}, hasMore bool, hint string) error {
	return PrintListFull(w, items, hasMore, hint, nil)
}

// PrintListFull is PrintList plus truncatedFields — names of fields shortened
// in one or more items (omitted when nil), so a consumer can detect truncation
// without scanning the in-string markers.
func PrintListFull(w *os.File, items interface{}, hasMore bool, hint string, truncatedFields []string) error {
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
	return Print(w, listEnvelope{OK: true, Count: count, HasMore: hasMore, Hint: hint, TruncatedFields: truncatedFields, Data: items})
}

// PrintObject wraps a single value in the object envelope: {"ok":true,"data":{...}}.
func PrintObject(w *os.File, obj interface{}) error {
	return Print(w, objectEnvelope{OK: true, Data: obj})
}

// PrintObjectFull is PrintObject plus an optional sibling `context` block
// (pre-computed hints that save a round trip) and `truncated_fields` (names of
// fields shortened in data). Both are omitted when nil/empty, degrading to a
// plain object envelope.
func PrintObjectFull(w *os.File, obj, context interface{}, truncatedFields []string) error {
	return Print(w, objectContextEnvelope{OK: true, Data: obj, Context: context, TruncatedFields: truncatedFields})
}

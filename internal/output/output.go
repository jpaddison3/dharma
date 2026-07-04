package output

import (
	"encoding/json"
	"fmt"
	"os"
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
	return PrintJSON(w, v)
}

// PrintJSON encodes v as JSON regardless of the global Format — indented on a
// TTY, compact when piped. Used by the raw `dharma api` passthrough, which must
// stay JSON (and jq-parseable) even under --output toon.
func PrintJSON(w *os.File, v interface{}) error {
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
func PrintList[T any](w *os.File, items []T, hasMore bool, hint string) error {
	return PrintListFull(w, items, hasMore, hint, nil)
}

// PrintListFull is PrintList plus truncatedFields — names of fields shortened
// in one or more items (omitted when nil), so a consumer can detect truncation
// without scanning the in-string markers.
func PrintListFull[T any](w *os.File, items []T, hasMore bool, hint string, truncatedFields []string) error {
	if items == nil {
		items = []T{} // never emit JSON null for an empty list
	}
	return Print(w, listEnvelope{OK: true, Count: len(items), HasMore: hasMore, Hint: hint, TruncatedFields: truncatedFields, Data: items})
}

// PrintObject wraps a single value in the object envelope: {"ok":true,"data":{...}}.
// It is PrintObjectFull with no context/truncated_fields — both omitempty, so the
// JSON is byte-identical to a bare {ok,data} envelope.
func PrintObject(w *os.File, obj interface{}) error {
	return PrintObjectFull(w, obj, nil, nil)
}

// PrintObjectFull is PrintObject plus an optional sibling `context` block
// (pre-computed hints that save a round trip) and `truncated_fields` (names of
// fields shortened in data). Both are omitted when nil/empty, degrading to a
// plain object envelope.
func PrintObjectFull(w *os.File, obj, context interface{}, truncatedFields []string) error {
	return Print(w, objectContextEnvelope{OK: true, Data: obj, Context: context, TruncatedFields: truncatedFields})
}

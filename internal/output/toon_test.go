package output

import (
	"strings"
	"testing"
)

func TestEncodeTOONTabular(t *testing.T) {
	v := map[string]interface{}{
		"ok":       true,
		"count":    float64(2),
		"has_more": false,
		"data": []interface{}{
			map[string]interface{}{"gid": "123", "name": "A"},
			map[string]interface{}{"gid": "456", "name": "B, sub"},
		},
	}
	out := encodeTOON(v)
	if !strings.Contains(out, "data[2]{gid,name}:") {
		t.Errorf("missing tabular header:\n%s", out)
	}
	// Numeric-looking gid must be quoted to stay a string; a value with a comma
	// must be quoted so it isn't read as two cells.
	if !strings.Contains(out, `"123",A`) {
		t.Errorf("row 1 wrong:\n%s", out)
	}
	if !strings.Contains(out, `"456","B, sub"`) {
		t.Errorf("row 2 wrong:\n%s", out)
	}
	if !strings.Contains(out, "count: 2") || !strings.Contains(out, "ok: true") {
		t.Errorf("scalar fields wrong:\n%s", out)
	}
}

func TestEncodeTOONNestedFallsBackToJSON(t *testing.T) {
	v := map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{
				"gid":      "1",
				"assignee": map[string]interface{}{"name": "X"},
			},
		},
	}
	out := encodeTOON(v)
	// A nested object per row can't be tabular; stay lossless with inline JSON.
	if !strings.Contains(out, `data: [{`) {
		t.Errorf("expected JSON fallback for nested array:\n%s", out)
	}
}

func TestEncodeTOONScalarsAndEmpty(t *testing.T) {
	v := map[string]interface{}{
		"n":    nil,
		"arr":  []interface{}{},
		"nums": []interface{}{float64(1), float64(2)},
	}
	out := encodeTOON(v)
	for _, want := range []string{"n: null", "arr[0]:", "nums[2]: 1,2"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestToonQuote(t *testing.T) {
	cases := map[string]string{
		"plain": "plain",
		"":      `""`,
		"true":  `"true"`, // would otherwise read as a bool
		"123":   `"123"`,  // would otherwise read as a number
		"a,b":   `"a,b"`,  // delimiter
		"a: b":  `"a: b"`, // delimiter
		" pad":  `" pad"`, // edge whitespace
	}
	for in, want := range cases {
		if got := toonQuote(in); got != want {
			t.Errorf("toonQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

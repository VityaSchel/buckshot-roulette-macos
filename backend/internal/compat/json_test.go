package compat

import (
	"slices"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	cases := map[string]bool{
		`{"a":1}`:                         true,
		` {"a":1} ` + "\n":                true,
		"\xEF\xBB\xBF" + `{"a":1}`:        true,
		"\xEF\xBB\xBF\xEF\xBB\xBF" + `{}`: false,
		`{"a":1}` + "\xEF\xBB\xBF":        false,
		"{\"a\":\"\xEF\xBB\xBF\"}":        true,
		`{"a":1e400}`:                     true,
		`{"a":1} trailing`:                false,
		`{"a":1}{}`:                       false,
		`{"a":`:                           false,
		"":                                false,
	}
	for text, ok := range cases {
		if _, err := DecodeJSON([]byte(text)); (err == nil) != ok {
			t.Errorf("DecodeJSON(%q): %v, want ok %v", text, err, ok)
		}
	}
}

func TestFieldAndEach(t *testing.T) {
	document, err := DecodeJSON([]byte(`{"a":{"b":{"c":7}},"list":[{"n":1},{"n":2.5}],"mixed":[{"n":1},null],"one":{"n":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := NonNegativeSafeInteger(Field(document, "a", "b", "c")); !ok || got != 7 {
		t.Errorf("a.b.c = %v, %v", got, ok)
	}
	for _, path := range [][]string{{"missing"}, {"a", "missing"}, {"a", "b", "c", "d"}, {"list", "n"}} {
		if got := Field(document, path...); got != nil {
			t.Errorf("Field(%q) = %v, want nil", path, got)
		}
	}
	if got := Field("not an object", "a"); got != nil {
		t.Errorf("Field of a string = %v", got)
	}
	if got, ok := Each(Field(document, "list"), "n", FiniteNumber); !ok || !slices.Equal(got, []float64{1, 2.5}) {
		t.Errorf("Each(list) = %v, %v", got, ok)
	}
	for _, name := range []string{"mixed", "one", "missing"} {
		if got, ok := Each(Field(document, name), "n", FiniteNumber); ok || got != nil {
			t.Errorf("Each(%s) = %v, %v", name, got, ok)
		}
	}
	if got, ok := Each(Field(document, "list"), "n", NonNegativeSafeInteger); ok || got != nil {
		t.Errorf("one bad element must fail the list, got %v, %v", got, ok)
	}
}

func TestNumbers(t *testing.T) {
	cases := []struct {
		text      string
		finite    bool
		integer   int64
		isInteger bool
	}{
		{`5`, true, 5, true},
		{`5.0`, true, 5, true},
		{`-0`, true, 0, true},
		{`9007199254740991`, true, 9007199254740991, true},
		{`9007199254740992`, true, 0, false},
		{`1e300`, true, 0, false},
		{`1e400`, false, 0, false},
		{`1.5`, true, 0, false},
		{`-5`, true, 0, false},
		{`"5"`, false, 0, false},
		{`null`, false, 0, false},
	}
	for _, c := range cases {
		value, err := DecodeJSON([]byte(c.text))
		if err != nil {
			t.Fatal(err)
		}
		if _, finite := FiniteNumber(value); finite != c.finite {
			t.Errorf("FiniteNumber(%s): %v, want %v", c.text, finite, c.finite)
		}
		if integer, ok := NonNegativeSafeInteger(value); ok != c.isInteger || integer != c.integer {
			t.Errorf("NonNegativeSafeInteger(%s) = %d, %v; want %d, %v", c.text, integer, ok, c.integer, c.isInteger)
		}
	}
}

func TestWithoutBOM(t *testing.T) {
	cases := map[string]string{
		"\xEF\xBB\xBFa":             "a",
		"\xEF\xBB\xBF\xEF\xBB\xBFa": "\xEF\xBB\xBFa",
		"a\xEF\xBB\xBF":             "a\xEF\xBB\xBF",
		"\xEF\xBB":                  "\xEF\xBB",
		"":                          "",
	}
	for text, want := range cases {
		if got := string(WithoutBOM([]byte(text))); got != want {
			t.Errorf("WithoutBOM(%q) = %q, want %q", text, got, want)
		}
	}
}

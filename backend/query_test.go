package main

import (
	"slices"
	"testing"
)

func TestParseQuery(t *testing.T) {
	cases := []struct {
		query string
		want  []queryPair
	}{
		{"", nil},
		{"&", nil},
		{"a=1&a=2", []queryPair{{"a", "1"}, {"a", "2"}}},
		{"a=b+c", []queryPair{{"a", "b c"}}},
		{"a=b%2Bc", []queryPair{{"a", "b+c"}}},
		{"openid.sig=ab+c%2Bd/e=", []queryPair{{"openid.sig", "ab c+d/e="}}},
		{"a=%E0%A4%A", []queryPair{{"a", "%E0%A4%A"}}},
		{"a=x+%ZZ", []queryPair{{"a", "x %ZZ"}}},
		{"a;b=1", []queryPair{{"a;b", "1"}}},
		{"a&&b", []queryPair{{"a", ""}, {"b", ""}}},
		{"=v", []queryPair{{"", "v"}}},
		{"a=b=c", []queryPair{{"a", "b=c"}}},
		{"k%65y=v", []queryPair{{"key", "v"}}},
		{"a+b=c", []queryPair{{"a b", "c"}}},
		{"a=%2541", []queryPair{{"a", "%41"}}},
	}
	for _, c := range cases {
		if got := parseQuery(c.query); !slices.Equal(got, c.want) {
			t.Errorf("parseQuery(%q) = %q, want %q", c.query, got, c.want)
		}
	}
}

func TestLastValuesKeepsTheLastValue(t *testing.T) {
	values := lastValues(parseQuery("openid.sig=first&file=a&openid.sig=second"))
	if values["openid.sig"] != "second" || values["file"] != "a" {
		t.Fatalf("got %q", values)
	}
}

func TestDecodeURIComponent(t *testing.T) {
	valid := map[string]string{
		"abc":           "abc",
		"%41":           "A",
		"%c3%a9":        "é",
		"é%20":          "é ",
		"%2541":         "%41",
		"a%F0%9F%98%80": "a😀",
	}
	for encoded, want := range valid {
		if got, ok := decodeURIComponent(encoded); !ok || got != want {
			t.Errorf("decodeURIComponent(%q) = %q, %v; want %q", encoded, got, ok, want)
		}
	}
	malformed := []string{"%", "%4", "%G1", "%C3", "%C3x", "%C3%41", "%E0%A4%A", "%ED%A0%80", "%C0%80", "%F4%90%80%80", "%FF"}
	for _, encoded := range malformed {
		if got, ok := decodeURIComponent(encoded); ok {
			t.Errorf("decodeURIComponent(%q) accepted as %q", encoded, got)
		}
	}
}

func TestEncodersMatchJavaScript(t *testing.T) {
	const sample = "!'()*~ é/:?#[]@$&+,;="
	if got, want := encodeURIComponent(sample), "!'()*~%20%C3%A9%2F%3A%3F%23%5B%5D%40%24%26%2B%2C%3B%3D"; got != want {
		t.Errorf("encodeURIComponent = %q, want %q", got, want)
	}
	if got, want := encodeForm(queryPair{"a", sample + "\n"}), "a=%21%27%28%29*%7E+%C3%A9%2F%3A%3F%23%5B%5D%40%24%26%2B%2C%3B%3D%0A"; got != want {
		t.Errorf("encodeForm = %q, want %q", got, want)
	}
	if got, want := encodeForm(queryPair{"b", "1"}, queryPair{"a", "2"}), "b=1&a=2"; got != want {
		t.Errorf("encodeForm keeps order: got %q", got)
	}
}

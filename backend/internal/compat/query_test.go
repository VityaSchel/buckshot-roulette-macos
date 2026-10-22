package compat

import (
	"slices"
	"testing"
)

func TestParseQuery(t *testing.T) {
	cases := []struct {
		query string
		want  []Pair
	}{
		{"", nil},
		{"&", nil},
		{"a=1&a=2", []Pair{{"a", "1"}, {"a", "2"}}},
		{"a=b+c", []Pair{{"a", "b c"}}},
		{"a=b%2Bc", []Pair{{"a", "b+c"}}},
		{"openid.sig=ab+c%2Bd/e=", []Pair{{"openid.sig", "ab c+d/e="}}},
		{"a=%E0%A4%A", []Pair{{"a", "%E0%A4%A"}}},
		{"a=x+%ZZ", []Pair{{"a", "x %ZZ"}}},
		{"a;b=1", []Pair{{"a;b", "1"}}},
		{"a&&b", []Pair{{"a", ""}, {"b", ""}}},
		{"=v", []Pair{{"", "v"}}},
		{"a=b=c", []Pair{{"a", "b=c"}}},
		{"k%65y=v", []Pair{{"key", "v"}}},
		{"a+b=c", []Pair{{"a b", "c"}}},
		{"a=%2541", []Pair{{"a", "%41"}}},
	}
	for _, c := range cases {
		if got := ParseQuery(c.query); !slices.Equal(got, c.want) {
			t.Errorf("ParseQuery(%q) = %q, want %q", c.query, got, c.want)
		}
	}
}

func TestLastValuesKeepsTheLastValue(t *testing.T) {
	values := LastValues(ParseQuery("openid.sig=first&file=a&openid.sig=second"))
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
		if got, ok := DecodeURIComponent(encoded); !ok || got != want {
			t.Errorf("DecodeURIComponent(%q) = %q, %v; want %q", encoded, got, ok, want)
		}
	}
	malformed := []string{"%", "%4", "%G1", "%C3", "%C3x", "%C3%41", "%E0%A4%A", "%ED%A0%80", "%C0%80", "%F4%90%80%80", "%FF"}
	for _, encoded := range malformed {
		if got, ok := DecodeURIComponent(encoded); ok {
			t.Errorf("DecodeURIComponent(%q) accepted as %q", encoded, got)
		}
	}
}

func TestEncodersMatchJavaScript(t *testing.T) {
	const sample = "!'()*~ é/:?#[]@$&+,;="
	if got, want := EncodeURIComponent(sample), "!'()*~%20%C3%A9%2F%3A%3F%23%5B%5D%40%24%26%2B%2C%3B%3D"; got != want {
		t.Errorf("EncodeURIComponent = %q, want %q", got, want)
	}
	if got, want := EncodeForm("a", sample+"\n"), "a=%21%27%28%29*%7E+%C3%A9%2F%3A%3F%23%5B%5D%40%24%26%2B%2C%3B%3D%0A"; got != want {
		t.Errorf("EncodeForm = %q, want %q", got, want)
	}
	if got, want := EncodeForm("b", "1", "a", "2", "c", ""), "b=1&a=2&c="; got != want {
		t.Errorf("EncodeForm keeps order: got %q", got)
	}
}

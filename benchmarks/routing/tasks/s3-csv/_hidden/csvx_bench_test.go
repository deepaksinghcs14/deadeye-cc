package csvx

import (
	"reflect"
	"testing"
)

func TestBenchParseLine(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a,,c", []string{"a", "", "c"}},                        // empty middle field
		{"a,", []string{"a", ""}},                               // trailing empty field
		{`"x,y",z`, []string{"x,y", "z"}},                       // comma inside quotes
		{`"he said ""hi""",ok`, []string{`he said "hi"`, "ok"}}, // doubled-quote escape
		{" a , b ", []string{" a ", " b "}},                     // unquoted spaces preserved
	}
	for _, c := range cases {
		if got := ParseLine(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseLine(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

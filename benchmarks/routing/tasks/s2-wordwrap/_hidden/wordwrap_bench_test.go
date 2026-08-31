package wordwrap

import (
	"reflect"
	"testing"
)

func TestBenchWordWrap(t *testing.T) {
	cases := []struct {
		text  string
		width int
		want  []string
	}{
		{"the quick brown fox", 10, []string{"the quick", "brown fox"}},
		{"a bb ccc", 3, []string{"a", "bb", "ccc"}},
		{"hello world", 5, []string{"hello", "world"}},
		{"supercalifragilistic hi", 5, []string{"supercalifragilistic", "hi"}}, // long word unsplit
		{"  multiple   spaces  here ", 100, []string{"multiple spaces here"}},  // collapse + trim
		{"héllo wörld", 5, []string{"héllo", "wörld"}},                         // rune-count, not byte
	}
	for _, c := range cases {
		if got := WordWrap(c.text, c.width); !reflect.DeepEqual(got, c.want) {
			t.Errorf("WordWrap(%q,%d)=%q want %q", c.text, c.width, got, c.want)
		}
	}
	for _, empty := range []string{"", "   ", "\t \n"} {
		if got := WordWrap(empty, 10); len(got) != 0 {
			t.Errorf("WordWrap(%q)=%q want empty", empty, got)
		}
	}
}

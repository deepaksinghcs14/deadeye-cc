package expr

import "testing"

func TestBenchEvalValid(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"2+3*4", 14},        // precedence
		{"(2+3)*4", 20},      // parens
		{"10-3-2", 5},        // left-assoc subtraction
		{"100/5/2", 10},      // left-assoc division
		{"2*(3+4)-1", 13},    // mixed
		{"-5+3", -2},         // unary minus
		{"  7  ", 7},         // whitespace
		{"2*-3", -6},         // unary after operator
	}
	for _, c := range cases {
		got, err := Eval(c.in)
		if err != nil {
			t.Errorf("Eval(%q) unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Eval(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestBenchEvalErrors(t *testing.T) {
	for _, bad := range []string{"10/0", "2+", "(1+2", "1++2", "", "abc"} {
		if _, err := Eval(bad); err == nil {
			t.Errorf("Eval(%q) expected error, got nil", bad)
		}
	}
}

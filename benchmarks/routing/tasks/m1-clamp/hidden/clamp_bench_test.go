package mathutil

import "testing"

func TestBenchClamp(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{{5, 0, 10, 5}, {-1, 0, 10, 0}, {11, 0, 10, 10}, {3, 3, 3, 3}, {0, -5, 5, 0}}
	for _, c := range cases {
		if got := Clamp(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("Clamp(%d,%d,%d)=%d want %d", c.v, c.lo, c.hi, got, c.want)
		}
	}
}

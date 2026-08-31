package semver

import "testing"

func sgn(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
}

func TestBenchSemverCompare(t *testing.T) {
	// The canonical SemVer 2.0.0 precedence chain (each strictly < the next).
	chain := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
	}
	for i := 0; i+1 < len(chain); i++ {
		a, b := chain[i], chain[i+1]
		if sgn(Compare(a, b)) != -1 {
			t.Errorf("Compare(%q,%q) sign=%d want -1", a, b, sgn(Compare(a, b)))
		}
		if sgn(Compare(b, a)) != 1 {
			t.Errorf("Compare(%q,%q) sign=%d want 1", b, a, sgn(Compare(b, a)))
		}
	}
	// numeric-vs-lexical trap: alpha.2 < alpha.11 numerically (lexically "2">"11")
	if sgn(Compare("1.0.0-alpha.2", "1.0.0-alpha.11")) != -1 {
		t.Error("alpha.2 should precede alpha.11 (numeric identifier comparison)")
	}
	// core numeric ordering
	if sgn(Compare("1.0.0", "2.0.0")) != -1 || sgn(Compare("2.1.1", "2.1.0")) != 1 {
		t.Error("core major/patch ordering wrong")
	}
	// build metadata ignored; equality; leading v tolerated
	for _, p := range [][2]string{{"1.0.0+build.9", "1.0.0"}, {"v1.2.3", "1.2.3"}, {"1.0.0-alpha+x", "1.0.0-alpha"}} {
		if sgn(Compare(p[0], p[1])) != 0 {
			t.Errorf("Compare(%q,%q) should be equal", p[0], p[1])
		}
	}
}

package main

import "testing"

func TestNormalizeBashRetryKey(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		wantKey string
		wantOK  bool
	}{
		// Same key across flag escalation -- the loops this exists to catch.
		{"pytest bare", "pytest tests/test_auth.py", "pytest tests/test_auth.py", true},
		{"pytest -x", "pytest tests/test_auth.py -x", "pytest tests/test_auth.py", true},
		{"pytest -x -vv", "pytest tests/test_auth.py -x -vv", "pytest tests/test_auth.py", true},
		{"go test bare", "go test ./internal/auth", "go test ./internal/auth", true},
		{"go test -race -v", "go test -race -v ./internal/auth", "go test ./internal/auth", true},

		// Different keys -- legitimately different runs must never collide.
		{"run=TestFoo keeps value", "go test -run=TestFoo ./pkg", "go test TestFoo ./pkg", true},
		{"run=TestBar keeps value", "go test -run=TestBar ./pkg", "go test TestBar ./pkg", true},
		{"count=1 keeps value", "go test -count=1 ./pkg", "go test 1 ./pkg", true},
		{"spaced option value kept", "git log -n 5 -- fileA.go", "git log 5 fileA.go", true},

		// Out of scope: refused outright.
		{"pipe refused", "grep -r handleAuth src | head -50", "", false},
		{"chain refused", "go build && go test", "", false},
		{"redirect refused", "go test > out.log", "", false},
		{"substitution refused", "echo $(date)", "", false},
		{"single token refused", "make", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, ok := normalizeBashRetryKey(c.cmd)
			if key != c.wantKey || ok != c.wantOK {
				t.Errorf("normalizeBashRetryKey(%q) = (%q, %v), want (%q, %v)", c.cmd, key, ok, c.wantKey, c.wantOK)
			}
		})
	}
}

package laya

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

// serveJSON stands in for laya-serve, capturing the request body so the
// wire format can be asserted against the upstream documented schema.
func serveJSON(t *testing.T, body string, captured *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			raw, _ := io.ReadAll(r.Body)
			m := map[string]any{}
			_ = json.Unmarshal(raw, &m)
			*captured = m
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The request must match laya-serve's documented shape: a `state` object and
// a `questions` map whose entries carry type/instructions/criteria. Getting
// this wrong fails silently as "no answer", so it's pinned.
func TestChoiceSendsDocumentedSchema(t *testing.T) {
	var got map[string]any
	srv := serveJSON(t, `{"answers":{"q":{"choice":"1","confidence":0.7,"answer_confidence":0.81}}}`, &got)

	c := New(srv.URL, "", "typed-decisions", time.Second)
	a, _, ok := c.Choice(ctx(t), "refactor the parser", "Pick a tier", map[string]string{"0": "easy", "1": "medium"})
	if !ok {
		t.Fatal("Choice returned not-ok against a well-formed server")
	}
	if a.Choice != "1" || a.Certainty() != 0.81 {
		t.Errorf("answer = %+v; want choice 1, certainty 0.81", a)
	}

	state, _ := got["state"].(map[string]any)
	if state["task"] != "refactor the parser" {
		t.Errorf("state = %v; want the task text under \"task\"", got["state"])
	}
	qs, _ := got["questions"].(map[string]any)
	q, _ := qs["q"].(map[string]any)
	if q["type"] != "choice" || q["instructions"] != "Pick a tier" {
		t.Errorf("question = %v; want type=choice with instructions", q)
	}
	crit, _ := q["criteria"].(map[string]any)
	if crit["0"] != "easy" || crit["1"] != "medium" {
		t.Errorf("criteria = %v; want label->description map", q["criteria"])
	}
}

// Certainty prefers the calibrated value and falls back to the
// entropy-derived one, so a server that reports only `confidence` still
// yields a usable number instead of zero.
func TestCertaintyPrefersCalibratedThenFallsBack(t *testing.T) {
	cases := []struct {
		name string
		a    Answer
		want float64
	}{
		{"calibrated present", Answer{Confidence: 0.4, AnswerConfidence: 0.9}, 0.9},
		{"only entropy confidence", Answer{Confidence: 0.55}, 0.55},
		{"neither", Answer{}, 0},
	}
	for _, c := range cases {
		if got := c.a.Certainty(); got != c.want {
			t.Errorf("%s: Certainty() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestYesNoAndScore(t *testing.T) {
	var got map[string]any
	srv := serveJSON(t, `{"answers":{"q":{"noul":0.73,"score":2.4,"confidence":0.8}}}`, &got)
	c := New(srv.URL, "", "typed-decisions", time.Second)

	a, _, ok := c.YesNo(ctx(t), "fix: nil deref in parser", "Is this a bug fix?")
	if !ok || a.Noul != 0.73 {
		t.Errorf("YesNo = %+v, ok=%v; want noul 0.73", a, ok)
	}
	qs, _ := got["questions"].(map[string]any)
	q, _ := qs["q"].(map[string]any)
	if q["type"] != "noul" {
		t.Errorf("YesNo sent type %v, want noul", q["type"])
	}
	// A noul question carries no criteria -- the instruction IS the
	// proposition, so an empty criteria key would be wrong on the wire.
	if _, present := q["criteria"]; present {
		t.Errorf("noul question should omit criteria, got %v", q)
	}

	if a, _, ok := c.Score(ctx(t), "rewrite the kernel", "How hard?", []string{"easy", "mid", "hard"}); !ok || a.Score != 2.4 {
		t.Errorf("Score = %+v, ok=%v; want score 2.4", a, ok)
	}
	qs, _ = got["questions"].(map[string]any)
	q, _ = qs["q"].(map[string]any)
	if _, isList := q["criteria"].([]any); !isList {
		t.Errorf("score criteria should be an ordered list, got %T", q["criteria"])
	}
}

// Every failure mode must fail OPEN -- ok=false, never a panic and never a
// fabricated answer. A classifier that is down cannot be allowed to change
// a routing decision.
func TestFailsOpenOnEveryFailureMode(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		var c *Client
		if _, _, ok := c.Choice(ctx(t), "x", "i", map[string]string{"a": "b"}); ok {
			t.Error("nil client answered")
		}
		if c.Endpoint() != "" {
			t.Error("nil client reported an endpoint")
		}
		if err := c.Health(ctx(t)); err == nil {
			t.Error("nil client reported healthy")
		}
	})
	t.Run("unset endpoint yields nil client", func(t *testing.T) {
		if New("", "", "typed-decisions", time.Second) != nil || New("   ", "", "typed-decisions", time.Second) != nil {
			t.Error("empty endpoint should produce a nil client")
		}
	})
	t.Run("connection refused", func(t *testing.T) {
		c := New("http://127.0.0.1:1", "", "typed-decisions", 200*time.Millisecond)
		if _, _, ok := c.YesNo(ctx(t), "x", "i"); ok {
			t.Error("unreachable endpoint answered")
		}
	})
	t.Run("non-2xx", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()
		if _, _, ok := New(srv.URL, "", "typed-decisions", time.Second).YesNo(ctx(t), "x", "i"); ok {
			t.Error("500 response answered")
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		srv := serveJSON(t, `{not json`, nil)
		if _, _, ok := New(srv.URL, "", "typed-decisions", time.Second).YesNo(ctx(t), "x", "i"); ok {
			t.Error("malformed body answered")
		}
	})
	t.Run("empty answers", func(t *testing.T) {
		srv := serveJSON(t, `{"answers":{}}`, nil)
		if _, _, ok := New(srv.URL, "", "typed-decisions", time.Second).YesNo(ctx(t), "x", "i"); ok {
			t.Error("empty answers map answered")
		}
	})
	t.Run("answer under a different key", func(t *testing.T) {
		srv := serveJSON(t, `{"answers":{"other":{"noul":1}}}`, nil)
		if _, _, ok := New(srv.URL, "", "typed-decisions", time.Second).YesNo(ctx(t), "x", "i"); ok {
			t.Error("mismatched answer key answered")
		}
	})
	t.Run("empty text or no questions", func(t *testing.T) {
		srv := serveJSON(t, `{"answers":{"q":{"noul":1}}}`, nil)
		c := New(srv.URL, "", "typed-decisions", time.Second)
		if _, _, ok := c.YesNo(ctx(t), "   ", "i"); ok {
			t.Error("blank text answered")
		}
		if _, ok := c.Ask(ctx(t), "text", nil); ok {
			t.Error("no questions answered")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond)
			fmt.Fprint(w, `{"answers":{"q":{"noul":1}}}`)
		}))
		defer srv.Close()
		if _, _, ok := New(srv.URL, "", "typed-decisions", 30*time.Millisecond).YesNo(ctx(t), "x", "i"); ok {
			t.Error("a call that outran its deadline answered")
		}
	})
}

// An unparseable `probabilities` field must not sink the whole response:
// its JSON shape differs between choice and score, which is exactly why the
// client doesn't model it.
func TestIgnoresUnmodelledFields(t *testing.T) {
	srv := serveJSON(t, `{"answers":{"q":{"choice":"2","probabilities":[0.1,0.2,0.7],"confidence":0.9}},
	 "routing":{"model":"english","reason":"ascii"},"usage":{"input_tokens":12}}`, nil)
	a, _, ok := New(srv.URL, "", "typed-decisions", time.Second).Choice(ctx(t), "x", "i", map[string]string{"2": "hard"})
	if !ok || a.Choice != "2" {
		t.Errorf("answer = %+v, ok=%v; want choice 2 despite array probabilities", a, ok)
	}
}

func TestBearerTokenSentOnlyWhenPresent(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"answers":{"q":{"noul":0.5}}}`)
	}))
	defer srv.Close()

	New(srv.URL, "tok-123", "typed-decisions", time.Second).YesNo(ctx(t), "x", "i")
	if auth != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want bearer token", auth)
	}
	auth = ""
	New(srv.URL, "", "typed-decisions", time.Second).YesNo(ctx(t), "x", "i")
	if auth != "" {
		t.Errorf("Authorization = %q, want no header when no key is set", auth)
	}
}

func TestHealthReachabilityOnly(t *testing.T) {
	// The health-response body is undocumented upstream, so any 2xx counts
	// and nothing is parsed out of it.
	srv := serveJSON(t, `not even json`, nil)
	if err := New(srv.URL, "", "typed-decisions", time.Second).Health(ctx(t)); err != nil {
		t.Errorf("2xx with an unparseable body should still be healthy: %v", err)
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	err := New(down.URL, "", "typed-decisions", time.Second).Health(ctx(t))
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("503 should surface as an error naming the status, got %v", err)
	}
}

func TestEndpointTrailingSlashNormalized(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		fmt.Fprint(w, `{"answers":{"q":{"noul":1}}}`)
	}))
	defer srv.Close()
	New(srv.URL+"/", "", "typed-decisions", time.Second).YesNo(ctx(t), "x", "i")
	if path != "/predict" {
		t.Errorf("path = %q, want /predict (no doubled slash)", path)
	}
}

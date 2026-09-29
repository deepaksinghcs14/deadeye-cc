// Package laya is a bounded HTTP client for a locally-run Laya decision
// model (github.com/NandhaKishorM/laya, Apache 2.0) -- a non-autoregressive
// classifier that answers typed choice/score/yes-no questions about a piece
// of text in one forward pass, with no free-text generation to parse.
//
// deadeye does NOT ship, install, or supervise Laya. Laya is Python; this
// plugin is a single static Go binary with no runtime dependencies, and
// bundling a Python interpreter plus ~843MB of weights into six release
// binaries would trade that away for one optional feature. (Apache 2.0
// would permit redistribution -- the objection is practical, not legal.)
// So the contract is an endpoint: the user runs `laya-serve` and points
// deadeye at it. Nothing here runs unless mode.laya is set and an endpoint
// answers.
//
// Fail-open is absolute (INV-5). Every method returns ok=false on an unset
// endpoint, a connection error, a timeout, a non-2xx status, malformed
// JSON, or a missing answer -- and every call site must then behave exactly
// as it did before Laya existed. A classifier that is down must never be
// able to change a routing decision, block a hook, or fail a review.
package laya

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultEndpoint is where `laya-serve` binds out of the box (it listens on
// 0.0.0.0:8000; deadeye talks to it over loopback).
const DefaultEndpoint = "http://127.0.0.1:8000"

// CheckpointTypedDecisions is the fine-tuned checkpoint every question
// deadeye asks belongs to. The server's own router only chooses by script
// and language -- english vs multilingual -- so without asking for this one
// by name deadeye gets base weights that upstream benchmarks at 0.362 on
// typed decisions, against 0.766 here.
const CheckpointTypedDecisions = "typed-decisions"

// DefaultTimeout bounds one /predict call. Laya's own published latency is
// 32.8ms on a T4 GPU but 193-464ms on CPU with the model already resident,
// and a cold checkpoint load costs seconds. Four of deadeye's call sites
// sit on the PreToolUse path that gates a real tool call (INV-8), so the
// budget is generous enough for resident CPU inference and far too short
// for a cold load: a server that has to load a checkpoint loses the race
// and the call site falls back, rather than stalling the user's tool call.
const DefaultTimeout = 1500 * time.Millisecond

// maxResponseBytes caps what a single answer can cost us in memory. A
// typed decision's response is a few hundred bytes; anything approaching
// this is a misconfigured endpoint (a proxy error page, the wrong service),
// not Laya.
const maxResponseBytes = 1 << 20

// Question is one typed question about the supplied state.
//
// Criteria's shape depends on Type, which is why it's `any`:
//   - TypeChoice: map[string]string -- label -> what that label means
//   - TypeScore:  []string         -- ordinal levels, lowest first
//   - TypeNoul:   omitted          -- the instruction IS the proposition
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// The three decision primitives Laya exposes. "noul" is its name for a
// calibrated P(true) over a yes/no proposition.
const (
	TypeChoice = "choice"
	TypeScore  = "score"
	TypeNoul   = "noul"
)

// Answer is one question's result. Which value field is populated depends
// on the question's Type.
//
// Confidence is 1 - normalized entropy over the distribution;
// AnswerConfidence is Laya's calibrated probability for the answer it
// actually reported. Callers that gate on certainty should read
// AnswerConfidence and treat a missing value (0) as "unknown", never as
// "certainly wrong" -- the `probabilities` field is deliberately not
// modeled here, since its JSON shape differs between choice and score and
// nothing in deadeye needs it.
type Answer struct {
	Choice           string  `json:"choice"`
	Score            float64 `json:"score"`
	Noul             float64 `json:"noul"`
	Confidence       float64 `json:"confidence"`
	AnswerConfidence float64 `json:"answer_confidence"`
}

// Certainty is the value call sites should gate on: the calibrated
// probability when the server reported one, else the entropy-derived
// confidence, else 0 for "unknown".
func (a Answer) Certainty() float64 {
	if a.AnswerConfidence > 0 {
		return a.AnswerConfidence
	}
	return a.Confidence
}

type request struct {
	State     map[string]string   `json:"state"`
	Questions map[string]Question `json:"questions"`
	// Model names a checkpoint by its short name. laya-serve honours a
	// client's model field when it names one ("english", "multilingual",
	// "typed-decisions"); omitted, its router picks by SCRIPT AND LANGUAGE
	// only -- which means it never reaches the typed-decisions checkpoint on
	// its own unless the server was started with LAYA_AUTO_TASK=1.
	//
	// That default matters more than it sounds: every question deadeye asks
	// is a typed decision, and upstream's own benchmark puts the base
	// checkpoint at 0.362 against 0.766 for typed-decisions, with "all of
	// the capability on this benchmark comes from fine-tuning". Asking the
	// base weights a typed question is asking the wrong model.
	Model string `json:"model,omitempty"`
}

type response struct {
	Answers map[string]Answer `json:"answers"`
	Routing struct {
		Model  string `json:"model"`
		Reason string `json:"reason"`
	} `json:"routing"`
}

// Result is one /predict response: the answers, plus which checkpoint
// actually produced them.
//
// Checkpoint is load-bearing, not decoration. An agreement rate that mixes
// two checkpoints is not a number about either of them, and a user who
// changes LAYA_MODELS mid-window would otherwise have no way to know the
// figure stopped meaning what it meant yesterday.
type Result struct {
	Answers    map[string]Answer
	Checkpoint string
}

// Client talks to one laya-serve endpoint. A nil *Client is valid and
// answers nothing -- call sites hold one unconditionally and don't branch
// on configuration themselves.
type Client struct {
	endpoint   string
	apiKey     string
	checkpoint string
	hc         *http.Client
}

// New returns nil when endpoint is empty, so "not configured" and "not
// reachable" collapse into the same fail-open path at every call site.
//
// apiKey is read from the environment by the caller, never from
// config.json: LAYA_API_KEY is a bearer token, and a token in a config
// file is a secret at rest that `deadeye config` would happily print.
// checkpoint is the short name to request per call ("typed-decisions" for
// everything deadeye asks). Empty leaves the choice to the server's router.
func New(endpoint, apiKey, checkpoint string, timeout time.Duration) *Client {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		endpoint:   endpoint,
		apiKey:     apiKey,
		checkpoint: strings.TrimSpace(checkpoint),
		hc:         &http.Client{Timeout: timeout},
	}
}

// Checkpoint reports the checkpoint this client asks for, or "" when it
// leaves the choice to the server's router.
func (c *Client) Checkpoint() string {
	if c == nil {
		return ""
	}
	return c.checkpoint
}

// Endpoint reports where this client points, for status/doctor output.
func (c *Client) Endpoint() string {
	if c == nil {
		return ""
	}
	return c.endpoint
}

// Ask is the one real entry point: several questions about one piece of
// text in a single round trip, since Laya answers them in one forward pass
// and a hook has budget for one call, not four.
func (c *Client) Ask(ctx context.Context, text string, questions map[string]Question) (Result, bool) {
	if c == nil || strings.TrimSpace(text) == "" || len(questions) == 0 {
		return Result{}, false
	}
	body, err := json.Marshal(request{
		State:     map[string]string{"task": text},
		Questions: questions,
		Model:     c.checkpoint,
	})
	if err != nil {
		return Result{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/predict", bytes.NewReader(body))
	if err != nil {
		return Result{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return Result{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Drain before closing so the pooled connection is reusable -- the
		// whole point of holding one client per configuration.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return Result{}, false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Result{}, false
	}
	var out response
	if json.Unmarshal(raw, &out) != nil || len(out.Answers) == 0 {
		return Result{}, false
	}
	return Result{Answers: out.Answers, Checkpoint: out.Routing.Model}, true
}

// Choice asks one labelled choice question. criteria maps each label to a
// short description of what it means.
func (c *Client) Choice(ctx context.Context, text, instructions string, criteria map[string]string) (Answer, string, bool) {
	return c.one(ctx, text, Question{Type: TypeChoice, Instructions: instructions, Criteria: criteria})
}

// YesNo asks one proposition and returns Answer.Noul as P(true).
func (c *Client) YesNo(ctx context.Context, text, instructions string) (Answer, string, bool) {
	return c.one(ctx, text, Question{Type: TypeNoul, Instructions: instructions})
}

// Score asks one ordinal-rubric question; levels are lowest-first.
func (c *Client) Score(ctx context.Context, text, instructions string, levels []string) (Answer, string, bool) {
	return c.one(ctx, text, Question{Type: TypeScore, Instructions: instructions, Criteria: levels})
}

const soleQuestion = "q"

func (c *Client) one(ctx context.Context, text string, q Question) (Answer, string, bool) {
	res, ok := c.Ask(ctx, text, map[string]Question{soleQuestion: q})
	if !ok {
		return Answer{}, "", false
	}
	a, ok := res.Answers[soleQuestion]
	return a, res.Checkpoint, ok
}

// Health probes the endpoint's /health. Reachability only: the endpoint's
// health-response body is not documented upstream, so any 2xx counts as up
// and nothing is parsed out of it.
func (c *Client) Health(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("no laya endpoint configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/health", nil)
	if err != nil {
		return err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("laya endpoint %s returned %s", c.endpoint, resp.Status)
	}
	return nil
}

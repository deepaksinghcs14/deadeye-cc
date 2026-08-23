package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/secscan"
)

func nowUnix() int64 { return time.Now().Unix() }

type editToolInput struct {
	FilePath  string `json:"file_path"`
	NewString string `json:"new_string"`
	Content   string `json:"content"`
}

type applyPatchInput struct {
	Command string `json:"command"`
}

// editTarget is one (path, added-text) pair pulled from a PreToolUse
// Edit/Write/apply_patch call -- apply_patch can touch several files in
// one call, Edit/Write always exactly one.
type editTarget struct {
	Path  string
	Added string
}

// extractEditTargets never reads the target file -- only what the tool
// call itself is about to write. Unrecognized shapes (a malformed
// tool_input, an unknown tool name) return nil, which decideVulnAdvice
// treats as "nothing to scan," not an error.
func extractEditTargets(toolName string, raw json.RawMessage) []editTarget {
	switch toolName {
	case "Edit":
		var in editToolInput
		if json.Unmarshal(raw, &in) != nil || in.FilePath == "" {
			return nil
		}
		return []editTarget{{Path: in.FilePath, Added: in.NewString}}
	case "Write":
		var in editToolInput
		if json.Unmarshal(raw, &in) != nil || in.FilePath == "" {
			return nil
		}
		return []editTarget{{Path: in.FilePath, Added: in.Content}}
	case "apply_patch":
		var in applyPatchInput
		if json.Unmarshal(raw, &in) != nil || in.Command == "" {
			return nil
		}
		return parseApplyPatch(in.Command)
	}
	return nil
}

// patchFileHeaderRe matches Codex apply_patch's file-section headers.
var patchFileHeaderRe = regexp.MustCompile(`^\*\*\* (?:Update|Add) File: (.+)$`)

// parseApplyPatch pulls (path, added-text) pairs out of Codex's
// apply_patch format (docs/verified.md §12): "*** Begin Patch" / "***
// Update File: x" or "*** Add File: x" / +added / -removed lines / "***
// End Patch". A deleted file carries nothing to scan.
func parseApplyPatch(cmd string) []editTarget {
	var out []editTarget
	var path string
	var added []string
	flush := func() {
		if path != "" && len(added) > 0 {
			out = append(out, editTarget{Path: path, Added: strings.Join(added, "\n")})
		}
		added = nil
	}
	for _, line := range strings.Split(cmd, "\n") {
		if m := patchFileHeaderRe.FindStringSubmatch(line); m != nil {
			flush()
			path = strings.TrimSpace(m[1])
			continue
		}
		if strings.HasPrefix(line, "*** Delete File:") || strings.HasPrefix(line, "*** End Patch") {
			flush()
			path = ""
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added = append(added, strings.TrimPrefix(line, "+"))
		}
	}
	flush()
	return out
}

// vulnFindingCap bounds how many advisories one edit can surface -- a
// reminder, not a report; /deadeye-guard is where the full pass lives.
const vulnFindingCap = 2

// decideVulnAdvice is coder mode's "check your backstop" section made
// deterministic: a regex/manifest pass over an Edit/Write/apply_patch's
// ADDED text only (never the target file, never the whole repo). Since
// 0.17.0 it is INDEPENDENT of the coder persona LEVEL -- turning the
// persona off ("stop coder", /deadeye-coder off, coder.default_level:
// "off") no longer silences the security advisory, because disliking the
// persona's prose is not a reason to stop checking what's being written.
// Still disabled by coder.security: "off" and the env kill switches
// (DEADEYE=off / DEADEYE_CODER=off, via Coder.Disabled). /deadeye-mute
// suppresses the advisory nags but NOT an ask-mode vulnerable-dependency
// prompt -- that's a human-facing security stop, like the hard plan gate
// mute also leaves on. Fails open on any parse miss (INV-5).
func decideVulnAdvice(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	if cfg.Coder.Disabled || cfg.Coder.Security == "off" {
		return hookio.Empty()
	}
	targets := extractEditTargets(in.ToolName, in.ToolInput)
	if len(targets) == 0 {
		return hookio.Empty()
	}

	// Mute suppresses the advisory nags, but NOT the ask: a confirmed
	// vulnerable-dependency add in ask mode is a human-facing security
	// stop, like the hard plan gate, which /deadeye-mute also leaves on.
	muted := state.isMuted(in.SessionID)
	askMode := cfg.Coder.Security == "ask"

	disabled := cfg.DisabledRuleSet()
	osvOn := cfg.Coder.SecurityOSVEnabled()
	var cache secscan.OSVCache
	if osvOn {
		cache = secscan.LoadOSVCache(meta.OSVCachePath(), nowUnix())
	}

	var lines []string   // advise-mode findings (deduped, mute-suppressed)
	var askDeps []string // confirmed-vulnerable adds to escalate (ask mode)
	var missingOSV []secscan.Dep
	for _, tgt := range targets {
		var findings []secscan.Finding
		if secscan.IsManifest(tgt.Path) {
			findings = secscan.ScanDeps(tgt.Path, tgt.Added, cache, disabled)
			if osvOn {
				for _, d := range secscan.ExtractDeps(tgt.Path, tgt.Added) {
					if !cache.Known(d.Ecosystem, d.Name, d.Version) {
						missingOSV = append(missingOSV, d)
					}
				}
			}
		} else {
			findings = secscan.Scan(tgt.Path, tgt.Added, disabled)
		}
		for _, f := range findings {
			// A confirmed-vulnerable dependency add, in ask mode, escalates
			// to a permission prompt instead of an advisory line -- no
			// dedupe (each add is a fresh human decision) and survives mute.
			if f.Vuln && askMode {
				askDeps = append(askDeps, f.Advice)
				state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/" + in.ToolName, Action: "vuln-ask", Reason: f.Rule})
				continue
			}
			if muted || len(lines) >= vulnFindingCap {
				continue
			}
			if !state.markSuggestedIfFirst(in.SessionID, "vuln:"+f.Rule+":"+tgt.Path) {
				continue
			}
			lines = append(lines, "deadeye: "+f.Advice)
			state.log(logstore.Record{TS: nowRFC3339(), SessionID: in.SessionID, Surface: "PreToolUse/" + in.ToolName, Action: "advise", Reason: f.Rule})
		}
	}

	if len(missingOSV) > 0 {
		triggerOSVRefresh(missingOSV)
	}
	if len(askDeps) == 0 && len(lines) == 0 {
		return hookio.Empty()
	}
	out := hookio.ForEvent("PreToolUse")
	if len(askDeps) > 0 {
		out.HookSpecificOutput.PermissionDecision = hookio.PermissionAsk
		// On a deny-or-pass host (Gemini), downgrade to a nudge -- there's
		// no "I accept the advisory" approve path there, so a hard deny
		// would strand a legitimate add.
		out.HookSpecificOutput.AskFallback = hookio.AskFallbackAdvise
		out.HookSpecificOutput.PermissionDecisionReason = "deadeye: this manifest edit adds a dependency with a known vulnerability (" + strings.Join(askDeps, "; ") + "). Approve only if you accept the advisory or have no safer version."
	}
	if len(lines) > 0 {
		out.HookSpecificOutput.AdditionalContext = strings.Join(lines, " ")
	}
	return out
}

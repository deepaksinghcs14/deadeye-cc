package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/coder"
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
// ADDED text only (never the target file, never the whole repo), gated on
// the coder persona being active -- security rides the coder axis rather
// than being a separate feature. Fails open on any parse miss or unmuted
// disable, same as every other advisory surface (INV-5).
func decideVulnAdvice(in hookio.Input, cfg config.Config, state *daemonState) hookio.Output {
	level := effectiveCoderLevel(in.SessionID, cfg, state)
	if !coder.Active(level) || cfg.Coder.Security == "off" || state.isMuted(in.SessionID) {
		return hookio.Empty()
	}
	targets := extractEditTargets(in.ToolName, in.ToolInput)
	if len(targets) == 0 {
		return hookio.Empty()
	}

	disabled := cfg.DisabledRuleSet()
	osvOn := cfg.Coder.SecurityOSVEnabled()
	var cache secscan.OSVCache
	if osvOn {
		cache = secscan.LoadOSVCache(meta.OSVCachePath(), nowUnix())
	}

	var lines []string
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
		if len(lines) >= vulnFindingCap {
			continue // still needed the loop body above for missingOSV
		}
		for _, f := range findings {
			if len(lines) >= vulnFindingCap {
				break
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
	if len(lines) == 0 {
		return hookio.Empty()
	}
	out := hookio.ForEvent("PreToolUse")
	out.HookSpecificOutput.AdditionalContext = strings.Join(lines, " ")
	return out
}

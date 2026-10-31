import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Hud } from '../types'
import { THEMES, THEME_SLUGS, autoTheme, claudeThemeFile, themeBySlug, type Theme } from './themes'

// deadeye's Claude Code mod. Off until `deadeye config set mode.mod on`
// (a plugin.json userConfig switch would make Claude Code < 2.1.75 drop
// the whole plugin). On: a themed HUD band, spinner, finish line, tool
// rows, the scope pane (/deadeye) and theme picker (/deadeye theme), a
// level badge, a toast on every deadeye block, and -- with mod.repaint --
// Claude's own colours through a custom theme file. Display only: routing,
// the gates and the coder persona stay in the Go binary.

const SCOPE = 'deadeye-scope'
const PICKER = 'deadeye-theme'
const CONFIG = 'deadeye-config'
const STATS = 'deadeye-stats'
const HELP = 'deadeye-help'
// The stats views, each one of the binary's own reports (the skill's table).
const STATS_VIEWS: Record<string, string[]> = { impact: ['gain'], savings: ['audit'], context: ['context'], accuracy: ['misses'], disagreement: ['disagreement'], adherence: ['adherence'] }
const COMMANDS: [string, string][] = [
  ['/deadeye', 'this scope pane (again closes it)'], ['/deadeye config', 'every setting; enter cycles a value'],
  ['/deadeye theme [name]', 'pick a theme, or set one by name'], ['/deadeye stats [view]', 'impact · savings · context · accuracy · disagreement · adherence'],
  ['/deadeye help', 'the full command card'], ['/deadeye repaint', 'retry recolouring Claude and the terminal'],
  ['/deadeye-status', 'modes, level, kill switches, catalog, daemon'], ['/deadeye-coder [level]', 'spotter · marksman · sniper · off'],
  ['/deadeye-route [task]', 'dry-run the routing decision'], ['/deadeye-mute [off]', 'mute advisories this session'],
  ['/deadeye-review [--repo]', 'four-lens self-review of the diff'], ['/deadeye-pr [<PR>] [--post]', 'the same lenses on a GitHub PR'],
  ['/deadeye-guard', 'OWASP-mapped security review of the diff'], ['/deadeye-vapt', 'whole-service pen-test pass'],
  ['/deadeye-sweep [--repo|--pr]', 'apply the findings, verify, re-scan'], ['/deadeye-debt', 'ledger of deadeye: shortcut markers'],
]
// The last stats report, drawn by the stats pane.
let statsTitle = ''
let statsLines: string[] = []
// The /deadeye skill ships the command; the mod answers it when on. A
// plugin skill's name may reach command.run plain or prefixed.
const isOurCommand = (name: string) => name === 'deadeye' || name === 'deadeye:deadeye'
const TIERS: [RegExp, string][] = [[/haiku/, 'tier 0'], [/sonnet/, 'tier 1'], [/opus/, 'tier 2'], [/fable/, 'tier 3']]

type ModConfig = { enabled: boolean; theme: string; repaint: boolean; background: boolean; sounds: boolean; notify: boolean; hud: 'auto' | 'full' | 'deadeye' | 'off'; tips: boolean }

const hud = atom({ plugin: 'deadeye', key: 'hud' } as const, {
  level: 'marksman', theme: 'marksman', route: '—', saved: 0, savedAll: 0, gate: 'clear', accuracy: null, context: null, daemon: false, repaint: 'off',
  model: '', branch: '', usage: null, resetAt: null, rev: 0,
} as Hud, { shape: 'hud-4' })

async function home($: EngineInterface) { return (await $.env.get('HOME')) ?? '' }

async function readJSON($: EngineInterface, path: string): Promise<any> {
  try { return JSON.parse(await $.fs.read(path)) } catch { return {} }
}

async function modConfig($: EngineInterface): Promise<ModConfig> {
  const c = await readJSON($, `${await home($)}/.deadeye/config.json`)
  const m = c?.mod ?? {}
  return {
    enabled: c?.mode?.mod === 'on',
    theme: typeof m.theme === 'string' ? m.theme : 'marksman',
    repaint: m.repaint !== 'off',
    background: m.background !== 'off',
    hud: ['full', 'deadeye', 'off'].includes(m.hud) ? m.hud : 'auto',
    tips: m.tips !== 'off',
    sounds: m.sounds === 'on',
    notify: m.notify === 'on',
  }
}

// Mirrors hooks/deadeye-statusline.sh: this session's mode file, then the global one.
async function coderLevel($: EngineInterface): Promise<string> {
  const h = await home($)
  const sid = (await $.session.id()).replace(/[^A-Za-z0-9-]/g, '_')
  for (const file of [`${h}/.deadeye/coder-mode.${sid}`, `${h}/.deadeye/coder-mode`]) {
    const level = await $.fs.read(file).then(t => t.trim(), () => undefined)
    if (level) return level
  }
  return 'off'
}

// deadeye is not always on PATH; fall back to its self-bootstrap install.
async function deadeye($: EngineInterface, args: readonly string[]) {
  const h = await home($)
  for (const bin of ['deadeye', `${h}/.deadeye/bin/deadeye`]) {
    const ran = await $.process.run([bin, ...args], { timeoutMs: 10_000 }).catch(() => undefined)
    if (ran) return ran
  }
  return undefined
}

// The tail of ~/.deadeye/decisions.jsonl, this session's records first:
// last route (PreToolUse/Agent advise), tokens saved (rewrite records),
// and how often a routing advisory was followed (PostToolUse/Agent resolved).
async function decisions($: EngineInterface, sid: string) {
  const path = `${await home($)}/.deadeye/decisions.jsonl`
  const ran = await $.process.run(['tail', '-c', '600000', path], { timeoutMs: 5_000 }).catch(() => undefined)
  const lines = (ran?.stdout ?? '').split('\n').slice(1)
  const rows = lines.flatMap(l => { try { return [JSON.parse(l)] } catch { return [] } })
  const mine = rows.filter(r => r.session_id === sid)
  const pick = (rs: any[], f: (r: any) => boolean) => rs.filter(f)
  const advised = pick(mine, r => r.surface === 'PreToolUse/Agent' && r.model).at(-1)
  const route = advised ? `${advised.model.replace(/^claude-/, '').replace(/-\d.*$/, '')} · ${TIERS.find(([re]) => re.test(advised.model))?.[1] ?? '?'}` : '—'
  const savedIn = (rs: any[]) => Math.round(rs.filter(r => r.action === 'rewrite').reduce((n, r) => n + Math.max(0, (r.bytes_before_est ?? 0) - (r.bytes_after ?? 0)), 0) / 4)
  // "Followed" compares tiers, not model ids: the harness may run
  // claude-opus-4-8 for a recommended claude-opus-5-5, and that is a follow.
  const tier = (m: string) => TIERS.findIndex(([re]) => re.test(m))
  let resolved = pick(mine, r => r.surface === 'PostToolUse/Agent' && r.action === 'resolved')
  if (resolved.length < 3) resolved = pick(rows, r => r.surface === 'PostToolUse/Agent' && r.action === 'resolved')
  const followed = resolved.filter(r => typeof r.reason === 'string' && typeof r.model === 'string' && tier(r.model) >= 0 && tier(r.model) === tier(r.reason.replace(/^recommended /, ''))).length
  const accuracy = resolved.length ? Math.round((100 * followed) / resolved.length) : null
  return { route, saved: savedIn(mine), savedAll: savedIn(rows), accuracy }
}

// The HUD's level chip: the coder level, or which kill switch silenced it
// (silence is indistinguishable from "forgot I turned it off").
async function levelChip($: EngineInterface): Promise<string> {
  if ((await $.env.get('DEADEYE')) === 'off') return 'deadeye off'
  if ((await $.env.get('DEADEYE_CODER')) === 'off') return 'coder off'
  return coderLevel($)
}

// The rate-limit window to show: the session (5-hour) one when named, else the first.
function pickLimit(limits: readonly { kind: string; percentUsed: number; resetsAt?: string }[] | undefined) {
  if (!limits?.length) return undefined
  return limits.find(l => /five|5h|session/i.test(l.kind)) ?? limits[0]
}

function clock(iso: string | undefined): string | null {
  if (!iso) return null
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  try { return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }) } catch { return `${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}` }
}

async function refresh($: EngineInterface, cfg: ModConfig, patch: Partial<Hud> = {}) {
  const sid = await $.session.id()
  const level = await levelChip($)
  const h = await home($)
  const [d, usage, daemon, model, branch] = await Promise.all([
    decisions($, sid),
    $.session.usage().catch(() => undefined),
    $.fs.stat(`${h}/.deadeye/deadeye.sock`).then(() => true, () => false),
    $.session.model().catch(() => ''),
    $.process.run(['git', 'rev-parse', '--abbrev-ref', 'HEAD'], { timeoutMs: 3_000 }).then(r => (r.exitCode === 0 ? r.stdout.trim() : ''), () => ''),
  ])
  const limit = pickLimit(usage?.rateLimits)
  const theme = cfg.theme === 'auto' ? autoTheme(level).slug : (themeBySlug(cfg.theme) ?? autoTheme('marksman')).slug
  const context = usage?.context?.percent ?? (usage?.context?.tokens && usage.context.window ? Math.round((100 * usage.context.tokens) / usage.context.window) : null)
  return update($, hud, v => ({ ...v, level, theme, ...d, context, daemon, model, branch, usage: limit ? Math.round(limit.percentUsed) : null, resetAt: clock(limit?.resetsAt), ...patch }))
}

// With mod.repaint on, Claude's own colours follow the theme: a custom
// theme file under ~/.claude/themes, then the theme row in /config. The
// theme the person had is kept in $.store and put back when the mod stops.
// `deadeye config list` rows: "  key   value   a · b · c" or "free text".
type ConfigRow = { key: string; value: string; allowed: string[] | null }
let configRows: ConfigRow[] = []
let configWhy = ''

async function loadConfigRows($: EngineInterface) {
  const ran = await deadeye($, ['config', 'list'])
  if (!ran || ran.exitCode !== 0) {
    configWhy = (ran?.stderr || 'deadeye binary not found').trim().slice(0, 160)
    configRows = []
    return
  }
  configWhy = ''
  configRows = ran.stdout.split('\n').flatMap(line => {
    const m = /^\s{2}(\S+)\s{2,}(\S+)\s{2,}(.+?)\s*$/.exec(line)
    if (!m) return []
    const [, key, value, rest] = m as unknown as [string, string, string, string]
    return [{ key, value, allowed: rest === 'free text' ? null : rest.split(' · ') }]
  })
}

// Why the last repaint was refused, for the pane and `/deadeye repaint`.
let repaintWhy = ''
let restartHinted = false

async function themesDir($: EngineInterface) {
  return `${(await $.env.get('CLAUDE_CONFIG_DIR')) ?? `${await home($)}/.claude`}/themes`
}

// Every theme's file, written once (rewritten only when its content
// changed): Claude Code reads the folder at startup, so after one restart
// all sixteen are loaded and `/deadeye theme <name>` repaints by itself.
async function installThemes($: EngineInterface) {
  const dir = await themesDir($)
  for (const t of THEMES) {
    const path = `${dir}/deadeye-${t.slug}.json`
    const want = claudeThemeFile(t)
    const have = await $.fs.read(path).catch(() => '')
    if (have !== want) await $.fs.write(path, want).catch(() => undefined)
  }
}

async function repaint($: EngineInterface, t: Theme): Promise<Hud['repaint']> {
  const slug = `deadeye-${t.slug}`
  repaintWhy = ''
  try {
    await $.fs.write(`${await themesDir($)}/${slug}.json`, claudeThemeFile(t))
    const rows = await $.config.list()
    const row = rows.find(r => r.key === 'theme')
    if (!row) {
      repaintWhy = `no "theme" row in /config (${rows.length} rows: ${rows.slice(0, 12).map(r => r.key).join(', ')})`
      return 'refused'
    }
    // Custom themes are stored as `custom:<slug>` (seen in settings.json).
    const isOurs = (v: unknown) => typeof v === 'string' && v.replace(/^custom:/, '').startsWith('deadeye-')
    if (typeof row.value === 'string' && !isOurs(row.value)) await $.store.set('prevTheme', row.value)
    if (row.value === slug || row.value === `custom:${slug}`) return 'on'
    let set = await $.config.set({ key: 'theme', value: `custom:${slug}` })
    if (set.deny) set = await $.config.set({ key: 'theme', value: slug })
    if (!set.deny) return 'on'
    // The /config row takes only the built-in names; a custom theme is what
    // /theme writes to settings.json as "custom:<slug>". Do the same.
    const wrote = await writeSettingsTheme($, `custom:${slug}`)
    if (wrote === 'on') {
      if (!restartHinted) {
        restartHinted = true
        $.ui.toast(`deadeye: Claude theme set to ${t.name} — restart Claude Code if the colours don't change`)
      }
      return 'on'
    }
    repaintWhy = `${set.deny}; settings.json: ${wrote}`
    return 'refused'
  } catch (err) {
    repaintWhy = err instanceof Error ? err.message : String(err)
    return 'refused'
  }
}

// mod.background: the terminal's own background and foreground, through
// OSC 11 / OSC 10 written to the tty that hosts Claude (found by walking up
// from the child: the hooks' children have no controlling tty of their
// own). iTerm2, Ghostty, Kitty, WezTerm, Alacritty and VS Code honour it;
// JetBrains terminals and Terminal.app ignore it. Reset with OSC 111 / 110.
let terminalPainted = false
// What the last paint did, for `/deadeye repaint`: the tty it wrote to, or why it couldn't.
let terminalWhy = ''

async function paintTerminal($: EngineInterface, t: Theme | null) {
  // Inside tmux the pane swallows OSC 11; tmux's own pane style colours
  // Claude's pane instead (and only that pane). `bg=default` resets.
  const tmux = await $.env.get('TMUX')
  const pane = await $.env.get('TMUX_PANE')
  if (tmux && pane) {
    const style = t ? `bg=${t.c.bg},fg=${t.c.fg}` : 'bg=default,fg=default'
    const ran = await $.process.run(['tmux', 'select-pane', '-t', pane, '-P', style], { env: { TMUX: tmux, TMUX_PANE: pane }, timeoutMs: 3_000 })
      .catch(err => ({ exitCode: -1, stdout: '', stderr: err instanceof Error ? err.message : String(err) }))
    terminalWhy = ran.exitCode === 0 ? `tmux pane ${pane} style ${style}` : `tmux: ${(ran.stderr || ran.stdout).trim() || `exit ${ran.exitCode}`}`
    if (ran.exitCode === 0) terminalPainted = t !== null
    return
  }
  const seq = t ? `\\033]11;${t.c.bg}\\007\\033]10;${t.c.fg}\\007` : '\\033]111\\007\\033]110\\007'
  const script = 'p=$PPID; for i in 1 2 3 4 5 6; do t=$(/bin/ps -o tty= -p "$p" | tr -d " "); case "$t" in ""|"?"|"??") p=$(/bin/ps -o ppid= -p "$p" | tr -d " "); [ -z "$p" ] && { echo "no tty up the process tree"; exit 1; };; *) printf "$SEQ" > "/dev/$t" && echo "/dev/$t"; exit $?;; esac; done; echo "no tty within 6 parents"; exit 1'
  const ran = await $.process.run(['/bin/sh', '-c', script], { env: { SEQ: seq }, timeoutMs: 3_000 }).catch(err => ({ exitCode: -1, stdout: '', stderr: err instanceof Error ? err.message : String(err) }))
  terminalWhy = ran.exitCode === 0 ? `wrote OSC ${t ? '11/10' : '111/110'} to ${ran.stdout.trim()}` : `${(ran.stdout + ran.stderr).trim() || `exit ${ran.exitCode}`}`
  if (ran.exitCode === 0) terminalPainted = t !== null
}

// Sets "theme" in Claude's settings.json the way /theme does, touching no
// other key. Resolves 'on', or why not.
async function writeSettingsTheme($: EngineInterface, value: string): Promise<string> {
  const dir = (await $.env.get('CLAUDE_CONFIG_DIR')) ?? `${await home($)}/.claude`
  const path = `${dir}/settings.json`
  try {
    const raw = await $.fs.read(path).catch(() => '{}')
    const settings = JSON.parse(raw || '{}')
    if (typeof settings !== 'object' || settings === null || Array.isArray(settings)) return 'settings.json is not an object'
    if (settings.theme === value) return 'on'
    if (typeof settings.theme === 'string' && !settings.theme.replace(/^custom:/, '').startsWith('deadeye-')) await $.store.set('prevTheme', settings.theme)
    settings.theme = value
    await $.fs.write(path, JSON.stringify(settings, null, 2) + '\n')
    return 'on'
  } catch (err) {
    return err instanceof Error ? err.message : String(err)
  }
}

async function unpaint($: EngineInterface) {
  if (terminalPainted) await paintTerminal($, null)
  const prev = await $.store.get('prevTheme')
  const row = (await $.config.list().catch(() => []))?.find(r => r.key === 'theme')
  if (typeof prev === 'string' && typeof row?.value === 'string' && row.value.replace(/^custom:/, '').startsWith('deadeye-')) {
    const set = await $.config.set({ key: 'theme', value: prev }).catch(() => ({ deny: 'threw' }))
    if (set.deny) await writeSettingsTheme($, prev)
  }
}

function bar(pct: number | null, width: number) {
  if (pct === null) return '░'.repeat(width)
  const full = Math.round((Math.max(0, Math.min(100, pct)) / 100) * width)
  return '█'.repeat(full) + '░'.repeat(width - full)
}
const pct = (p: number | null) => (p === null ? '—' : `${p}%`)
const k = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}k` : `${n}`)

// Module state: reset by a reload, refilled by session.start (which a reload runs again).
let cfg: ModConfig = { enabled: false, theme: 'marksman', repaint: false, background: false, sounds: false, notify: false, hud: 'auto', tips: true }

// The /deadeye skill ships the command for every host; with the mod on the
// mod registers it too, so /deadeye resolves even where the skill's name
// folds differently. A refusal (already taken) is fine.
async function registerCommand($: EngineInterface) {
  await $.command.register({ name: 'deadeye', description: 'deadeye: scope pane; config · theme [name] · stats [view] · help · repaint · close' }).catch(() => undefined)
}

// Tips about deadeye's commands, the way Claude shows its own: one every
// fifth turn, in the spinner while it runs and at the hint line's tail while
// you type. A tip retires once its command has been used ($.store, across
// sessions), so the deck shrinks to what you haven't tried.
const TIPS: [string, string][] = [
  ['deadeye config', '/deadeye config — every setting in a pane, enter cycles a value'],
  ['deadeye theme', '/deadeye theme — sixteen looks for Claude, pick one from a list'],
  ['deadeye stats', '/deadeye stats — what deadeye measured: savings, routing, context'],
  ['deadeye help', '/deadeye help — the card of every deadeye command'],
  ['deadeye-coder', '/deadeye-coder sniper — one shot only: the one-liner, and the rest challenged'],
  ['deadeye-review', '/deadeye-review — four-lens self-review of your diff before a PR'],
  ['deadeye-pr', '/deadeye-pr 123 --post — review a GitHub PR, post the findings'],
  ['deadeye-guard', '/deadeye-guard — OWASP-mapped security review of the current diff'],
  ['deadeye-sweep', '/deadeye-sweep — apply the review findings, verify the build, re-scan'],
  ['deadeye-vapt', '/deadeye-vapt — whole-service pen-test pass, ranked worst-first'],
  ['deadeye-mute', '/deadeye-mute — quiet the advisories for this session; trimming stays on'],
  ['deadeye-debt', '/deadeye-debt — the ledger of every deadeye: shortcut marker in the repo'],
  ['deadeye-route', '/deadeye-route <task> — what deadeye would route a task to, and why'],
  ['deadeye-status', '/deadeye-status — modes, level, kill switches, daemon in one screen'],
  ['kill', 'DEADEYE=off in your shell turns everything off; DEADEYE_CODER=off just the persona'],
]
let turns = 0
let tip: string | null = null
let lastTip = ''
let tipsUsed: string[] = []

async function loadTipsUsed($: EngineInterface) {
  const v = await $.store.get('tipsUsed').catch(() => undefined)
  tipsUsed = Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
}

async function markUsed($: EngineInterface, command: string, sub: string) {
  const key = command.replace(/^deadeye:/, '') === 'deadeye' ? `deadeye ${sub}` : command.replace(/^deadeye:/, '')
  if (!TIPS.some(([k]) => k === key) || tipsUsed.includes(key)) return
  tipsUsed = [...tipsUsed, key]
  await $.store.set('tipsUsed', tipsUsed).catch(() => undefined)
}

function pickTip(): string | null {
  const deck = TIPS.filter(([k, text]) => !tipsUsed.includes(k) && text !== lastTip)
  if (!deck.length) return null
  const [, text] = deck[turns % deck.length]!
  lastTip = text
  return text
}
// Whether Claude's own statusLine is configured (then the HUD row shows only deadeye's figures).
let hasStatusLine = false

async function detectStatusLine($: EngineInterface) {
  const dir = (await $.env.get('CLAUDE_CONFIG_DIR')) ?? `${await home($)}/.claude`
  const s = await readJSON($, `${dir}/settings.json`)
  hasStatusLine = Boolean(s?.statusLine)
}
let theme: Theme = THEMES[0]!

const themeFor = (h: Hud) => themeBySlug(h.theme) ?? THEMES[0]!

async function applyTheme($: EngineInterface, slug: string, persist: boolean) {
  cfg = { ...cfg, theme: slug }
  const h = await refresh($, cfg)
  theme = themeFor(h)
  const rp = cfg.repaint ? await repaint($, theme) : 'off'
  if (cfg.background) await paintTerminal($, theme)
  else if (terminalPainted) await paintTerminal($, null)
  await update($, hud, v => ({ ...v, repaint: rp }))
  if (persist) {
    const ran = await deadeye($, ['config', 'set', 'mod.theme', slug])
    if (!ran || ran.exitCode !== 0) $.ui.toast(`deadeye: theme set for this session only — ${(ran?.stderr || 'deadeye binary not found').trim().slice(0, 120)}`)
  }
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    cfg = await modConfig($)
    await detectStatusLine($)
    if (!cfg.enabled) {
      await unpaint($)
      return next(e)
    }
    if (cfg.repaint) await installThemes($)
    await loadTipsUsed($)
    await registerCommand($)
    await applyTheme($, cfg.theme, false)
    return next(e)
  })

  // The coder level can change mid-turn (/deadeye-coder); refresh at turn end.
  on('turn.complete', async ($, e, next) => {
    // Re-read the config each turn: `deadeye config set mode.mod on` (or a
    // theme change from the CLI) then takes effect on the next turn, no
    // restart or reload needed.
    const fresh = await modConfig($)
    const toggled = fresh.enabled !== cfg.enabled
    const restyled = fresh.enabled && (fresh.theme !== cfg.theme || fresh.repaint !== cfg.repaint || fresh.background !== cfg.background)
    cfg = { ...fresh, theme: fresh.enabled ? fresh.theme : cfg.theme }
    if (toggled && !cfg.enabled) {
      await unpaint($)
      return next(e)
    }
    if (!cfg.enabled) return next(e)
    if (toggled) await registerCommand($)
    if (toggled || restyled) await applyTheme($, cfg.theme, false)
    turns++
    tip = cfg.tips && turns % 5 === 0 ? pickTip() : null
    const h = await refresh($, cfg)
    if (cfg.theme === 'auto' && h.theme !== theme.slug) await applyTheme($, 'auto', false)
    const took = e.durationMs
    if (cfg.sounds && e.reason === 'answer') void $.audio.play({ asset: 'hooks/sounds/target.wav' }).catch(() => undefined)
    if (cfg.notify && took > 60_000) void $.ui.notify(`${theme.done} in ${Math.round(took / 1000)}s`, { title: 'deadeye' }).catch(() => undefined)
    return next(e)
  })

  on('session.end', async ($, e, next) => {
    if (terminalPainted) await paintTerminal($, null)
    return next(e)
  })

  // deadeye's own PreToolUse hook denies with a "deadeye:" reason: toast it,
  // mark the gate as holding until an edit lands.
  on('tool.call', async ($, e, next) => {
    const ran = await next(e)
    if (!cfg.enabled) return ran
    const reason = ran.deny ?? (ran.isError ? ran.text : undefined)
    const at = reason?.indexOf('deadeye:') ?? -1
    if (at >= 0) {
      const text = reason!.slice(at + 'deadeye:'.length, at + 200).trim()
      $.ui.toast(`${theme.tag} · ${text}`)
      if (cfg.sounds) void $.audio.play({ asset: 'hooks/sounds/hold.wav' }).catch(() => undefined)
      if (/plan gate/i.test(text)) await update($, hud, v => ({ ...v, gate: 'holding' as const }))
    } else if ((e.tool === 'Edit' || e.tool === 'Write') && !ran.isError) {
      await update($, hud, v => (v.gate === 'clear' ? v : { ...v, gate: 'clear' as const }))
    }
    return ran
  }).catch(($, e, next) => next(e)) // a UI nicety never blocks a tool call

  on('command.run', async ($, e, next) => {
    const [sub, name] = e.args.trim().split(/\s+/)
    if (/^(deadeye:)?deadeye(-|$)/.test(e.command)) await markUsed($, e.command, sub ?? '')
    // Mod off: the /deadeye skill runs as it always has (status, config, theme from chat).
    if (!isOurCommand(e.command) || !cfg.enabled) return next(e)
    if (sub === 'stats') {
      const view = name && STATS_VIEWS[name] ? name : 'impact'
      const args = [...STATS_VIEWS[view]!, ...(view === 'context' && name === 'context' ? e.args.trim().split(/\s+/).slice(2) : [])]
      const ran = await deadeye($, args)
      statsTitle = `deadeye · stats · ${view}`
      statsLines = ran ? (ran.stdout + (ran.exitCode === 0 ? '' : '\n' + ran.stderr)).replace(/\x1b\[[0-9;]*m/g, '').split('\n') : ['deadeye binary not found (PATH or ~/.deadeye/bin).']
      await update($, hud, v => ({ ...v, rev: v.rev + 1 }))
      await $.ui.open({ id: STATS, title: statsTitle, rows: Math.min(40, statsLines.length + 3) })
      return { text: `${statsTitle} opened (${Object.keys(STATS_VIEWS).join(' · ')}). Figures are the binary's own; estimates and measurements are labelled as such.` }
    }
    if (sub === 'help') {
      await $.ui.open({ id: HELP, title: 'deadeye · help', rows: Math.min(40, COMMANDS.length + 6) })
      return { text: 'Command card opened.' }
    }
    if (sub === 'config') {
      await loadConfigRows($)
      await update($, hud, v => ({ ...v, rev: v.rev + 1 }))
      await $.ui.open({ id: CONFIG, title: 'deadeye · config', focus: true, closeOnEscape: true, rows: Math.min(32, configRows.length + 4) })
      return { text: configWhy ? `deadeye config: ${configWhy}` : 'Settings pane opened: ↑↓ move, enter cycles a value, esc closes. Free-text keys: `deadeye config set <key> <value>`.' }
    }
    if (sub === 'repaint') {
      const rp = await repaint($, theme)
      await update($, hud, v => ({ ...v, repaint: rp }))
      if (cfg.background) await paintTerminal($, theme)
      const bg = cfg.background ? ` Terminal: ${terminalWhy} (tmux panes, iTerm2, Ghostty, Kitty, WezTerm, Alacritty and VS Code; JetBrains terminals and Terminal.app ignore it).` : ' Terminal: mod.background is off.'
      return { text: (rp === 'on' ? `deadeye: Claude repainted to ${theme.name}.` : `deadeye: repaint refused — ${repaintWhy}. Pick "deadeye ${theme.name}" under /theme by hand.`) + bg }
    }
    if (sub === 'theme' && name) {
      if (name !== 'auto' && !themeBySlug(name)) return { text: `deadeye: no theme "${name}". Themes: auto, ${THEME_SLUGS.join(', ')}.` }
      await applyTheme($, name, true)
      return { text: `deadeye theme: ${name === 'auto' ? `auto (${theme.name})` : theme.name}.` }
    }
    if (sub === 'theme') {
      await $.ui.open({ id: PICKER, title: 'deadeye · theme', focus: true, closeOnEscape: true, rows: THEMES.length + 4 })
      return { text: 'Theme picker opened: ↑↓ move, enter keeps, esc cancels.' }
    }
    const shown = (await $.ui.panes().catch(() => [])).some(p => p.id === SCOPE && p.isShown)
    if (sub === 'close' || (shown && !sub)) {
      await $.ui.close({ id: SCOPE })
      return { text: `${theme.pane} pane closed.` }
    }
    await $.ui.open({ id: SCOPE, title: `deadeye · ${theme.pane}` })
    return { text: `${theme.pane} pane opened. /deadeye again closes it; /deadeye config, /deadeye theme.` }
  }).catch(($, e, next) => next(e))

  on('ui.render', { component: 'Pane', requestId: STATS }, async ($, e) => {
    await read($, hud)
    const { Box, Text } = $.ui.resolve(e)
    return Box({ flexDirection: 'column', children: [
      ...statsLines.map(l => Text({ dimColor: !l.trim() || /^\s/.test(l) && !/\d/.test(l), children: l || ' ' })),
      Text({ dimColor: true, children: `/deadeye stats ${Object.keys(STATS_VIEWS).join('|')}` }),
    ] })
  })

  on('ui.render', { component: 'Pane', requestId: HELP }, ($, e) => {
    const { Box, Text } = $.ui.resolve(e)
    const t = theme
    return Box({ flexDirection: 'column', children: [
      ...COMMANDS.map(([c, d]) => Text({ children: [Text({ color: t.c.acc, children: c.padEnd(30) }), Text({ dimColor: true, children: d })] })),
      Text({ children: ' ' }),
      Text({ dimColor: true, children: 'kill switches: DEADEYE=off · DEADEYE_PREPROCESS=off · DEADEYE_GATE=off · DEADEYE_CODER=off' }),
      Text({ dimColor: true, children: 'coder level: /deadeye-coder spotter|marksman|sniper|off · "normal mode" turns it off mid-session' }),
    ] })
  })

  // Settings pane: every key the CLI knows, enter cycles an enum value
  // through `deadeye config set`, so validation stays the binary's.
  on('ui.render', { component: 'Pane', requestId: CONFIG }, async ($, e) => {
    await read($, hud)
    const t = $.ui.resolve(e)
    const { Box, Text } = t
    if (!configRows.length) return Text({ color: theme.c.warn, children: configWhy || 'no settings read; run /deadeye config again' })
    const width = Math.max(...configRows.map(r => r.key.length)) + 2
    const options = configRows.map(r => ({ key: r.key, value: r.key, label: `${r.key.padEnd(width)} ${r.value.padEnd(11)} ${r.allowed ? r.allowed.join(' · ') : 'free text'}` }))
    if (!('Select' in t)) return Box({ flexDirection: 'column', children: options.map(o => Text({ children: o.label })) })
    const { Select } = t
    return Box({ flexDirection: 'column', gap: 1, children: [
      Select({ key: 'config', options, autoFocus: true, onSelect: async (key: string) => {
        const row = configRows.find(r => r.key === key)
        if (!row) return
        if (key === 'mod.theme') {
          await $.ui.open({ id: PICKER, title: 'deadeye · theme', focus: true, closeOnEscape: true, rows: THEMES.length + 4 })
          return
        }
        if (!row.allowed) {
          $.ui.toast(`${key} is free text: deadeye config set ${key} <value>`)
          return
        }
        const next = row.allowed[(row.allowed.indexOf(row.value) + 1) % row.allowed.length] ?? row.allowed[0]!
        const ran = await deadeye($, ['config', 'set', key, next])
        if (!ran || ran.exitCode !== 0) {
          $.ui.toast(`deadeye: ${(ran?.stderr || 'config set failed').trim().slice(0, 140)}`)
          return
        }
        await loadConfigRows($)
        await update($, hud, v => ({ ...v, rev: v.rev + 1 }))
        $.ui.toast(`${key} → ${next}${key.startsWith('mod.') ? ' (applies on the next turn)' : ''}`)
      } }),
      Text({ dimColor: true, children: '↑↓ move · enter cycles the value · esc close · mod.theme opens the picker' }),
    ] })
  })

  // Spinner: the theme's verbs in place of the engine's ("Sighting… (4s)").
  on('ui.render', { component: 'Spinner' }, ($, e, next) => {
    if (!cfg.enabled || e.props.message) return next(e)
    const verb = theme.verbs[Math.floor(Date.now() / 2500) % theme.verbs.length] ?? e.props.word
    const suffix = tip ? ` · tip: ${tip}${e.props.suffix}` : e.props.suffix
    return next({ ...e, props: { ...e.props, word: verb, suffix } })
  })

  // Elements are built by calling the surface's constructors, not JSX: the
  // installed-plugin runtime has no `h` global (dev loaders do).

  // Finish line: "✓ On target in 14s" instead of "Baked for 14s".
  on('ui.render', { component: 'TurnDuration' }, ($, e, next) => {
    if (!cfg.enabled || e.props.waitingFor || e.props.isExpanded) return next(e)
    const { Text } = $.ui.resolve(e)
    const secs = Math.round(e.props.durationMs / 1000)
    const dur = secs >= 60 ? `${Math.floor(secs / 60)}m ${secs % 60}s` : `${secs}s`
    return Text({ dimColor: true, children: [Text({ color: theme.c.acc, children: theme.glyph.done }), ` ${theme.done} in ${dur}`] })
  })

  // Tool rows: a themed glyph and a one-line target for the read-only tools.
  // Edit/Write/Bash keep the engine's row (diffs, output) untouched.
  on('ui.render', { component: 'ToolUse' }, ($, e, next) => {
    const p = e.props
    if (!cfg.enabled || p.isExpanded || !['Read', 'Grep', 'Glob'].includes(p.tool)) return next(e)
    const { Text } = $.ui.resolve(e)
    const input = (p.input ?? {}) as Record<string, unknown>
    const target = String(input.file_path ?? input.pattern ?? '').slice(0, 80)
    return Text({ children: [
      Text({ color: p.isErrored ? theme.c.bad : theme.c.acc, children: theme.glyph.tool }), ' ',
      Text({ weight: 'bold', children: p.tool }), ' ', target,
      p.isRunning ? Text({ dimColor: true, children: ' …' }) : '',
    ] })
  })

  // A deadeye block as one short tag line instead of an error wall.
  on('ui.render', { component: 'ToolResult' }, ($, e, next) => {
    if (!cfg.enabled || e.props.isExpanded || !e.props.isErrored) return next(e)
    const text = typeof e.props.output === 'string' ? e.props.output : JSON.stringify(e.props.output ?? '')
    const at = text.indexOf('deadeye:')
    if (at < 0) return next(e)
    const { Text } = $.ui.resolve(e)
    const reason = text.slice(at + 'deadeye:'.length).replace(/\s+/g, ' ').trim().slice(0, 120)
    return Text({ children: [
      '  ', Text({ color: theme.c.bg, backgroundColor: theme.c.warn, weight: 'bold', children: ` ${theme.tag} ` }), ' ',
      Text({ color: theme.c.bad, children: reason }),
    ] })
  })

  // Above the prompt: nothing, except one row while the plan gate holds --
  // the toast fades and the HOLD tag scrolls away, this stays until an edit lands.
  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    if (!cfg.enabled) return next(e)
    const h = await read($, hud)
    if (h.gate !== 'holding') return next(e)
    const t = themeFor(h)
    const { Box, Text } = $.ui.resolve(e)
    return Box({ flexDirection: 'row', gap: 1, children: [
      Text({ color: t.c.bg, backgroundColor: t.c.warn, weight: 'bold', children: ` ${t.tag} ` }),
      Text({ color: t.c.warn, children: 'plan gate' }),
      Text({ dimColor: true, children: '· present a short plan and wait for the go-ahead · /deadeye for the scope' }),
    ] })
  })

  // Under the prompt: the idle hint line becomes the HUD --
  // LEVEL ⎇ branch │ model │ Ctx: 44% │ Usage: 59% ▓▓▓▓▓▓░░░░ → reset 11:40 AM.
  // While a turn runs the engine's own line stays (esc to interrupt).
  on('ui.render', { component: 'PromptHint' }, async ($, e, next) => {
    if (!cfg.enabled || cfg.hud === 'off' || e.props.isWorking || e.props.isDraft) return next(e)
    const h = await read($, hud)
    const t = themeFor(h)
    const { Text } = $.ui.resolve(e)
    const sep = Text({ dimColor: true, children: ' │ ' })
    const usageBar = h.usage === null ? '' : bar(h.usage, 10)
    const chip = Text({ color: t.c.bg, backgroundColor: t.c.acc, weight: 'bold', children: ` ${h.level.toUpperCase()} ` })
    // With Claude's own statusLine configured (branch, model, context, usage
    // already on screen) the row carries only deadeye's figures.
    if (cfg.hud === 'deadeye' || (cfg.hud === 'auto' && hasStatusLine)) {
      const meter = (label: string, value: number | null, color: string) =>
        [Text({ dimColor: true, children: `${label} ` }), Text({ color, children: bar(value, 10) }), Text({ color: t.c.fg, children: ` ${pct(value)}` })]
      return Text({ children: [
        chip, ' ', Text({ color: t.c.fg, children: t.name }), sep,
        ...meter('routed right', h.accuracy, t.c.acc), sep,
        ...meter('context', h.context, h.context !== null && h.context >= 80 ? t.c.warn : t.c.acc2), sep,
        Text({ dimColor: true, children: 'gate ' }), Text({ color: h.gate === 'holding' ? t.c.warn : t.c.good, children: h.gate }),
        Text({ dimColor: true, children: tip ? `  · tip: ${tip}` : '  · /deadeye' }),
      ] })
    }
    return Text({ children: [
      chip, ' ',
      Text({ color: t.c.acc, children: '⎇ ' }), Text({ color: t.c.fg, children: h.branch || '—' }), sep,
      Text({ color: t.c.fg, children: h.model || '—' }), sep,
      Text({ dimColor: true, children: 'Ctx: ' }), Text({ color: h.context !== null && h.context >= 80 ? t.c.warn : t.c.fg, children: pct(h.context) }),
      ...(h.usage === null ? [] : [sep, Text({ dimColor: true, children: 'Usage: ' }), Text({ color: h.usage >= 80 ? t.c.warn : t.c.fg, children: `${h.usage}% ` }),
        Text({ color: h.usage >= 80 ? t.c.warn : t.c.acc, children: usageBar }),
        ...(h.resetAt ? [Text({ dimColor: true, children: ` → reset ${h.resetAt}` })] : [])]),
      Text({ dimColor: true, children: tip ? `  · tip: ${tip}` : '  · /deadeye' }),
    ] })
  })

  on('ui.render', { component: 'InfoNotice' }, async ($, e, next) => {
    if (!cfg.enabled) return next(e)
    const h = await read($, hud)
    const first = cfg.tips ? TIPS.find(([k]) => !tipsUsed.includes(k))?.[1] : undefined
    return next({ ...e, props: { ...e.props, text: `${e.props.text} · deadeye ${themeFor(h).name} · ${h.level}${first ? ` · ${first}` : ''}` } })
  })

  // Scope pane: docked beside the transcript, or a two-row strip inline.
  on('ui.render', { component: 'Pane', requestId: SCOPE }, async ($, e) => {
    const h = await read($, hud)
    const t = themeFor(h)
    const { Box, Text } = $.ui.resolve(e)
    const good = (ok: boolean) => (ok ? t.c.good : t.c.warn)
    const dim = (label: string, value: string, color: string) => Text({ dimColor: true, children: [`${label} `, Text({ color, children: value })] })
    const meter = (label: string, value: number | null, color: string, width: number) =>
      Text({ dimColor: true, children: [`${label} `, Text({ color, children: bar(value, width) }), ' ', Text({ color: t.c.fg, children: pct(value) })] })
    if (e.props.placement === 'inline') {
      return Box({ flexDirection: 'column', children: [
        Box({ flexDirection: 'row', gap: 2, flexWrap: 'wrap', children: [
          Text({ color: t.c.acc, weight: 'bold', children: `${t.glyph.tool} ${h.level.toUpperCase()}` }),
          meter('routed right', h.accuracy, t.c.acc, 10),
          meter('context', h.context, t.c.acc2, 10),
        ] }),
        Box({ flexDirection: 'row', gap: 2, flexWrap: 'wrap', children: [
          dim('route', h.route, t.c.fg), dim('saved', h.saved ? `${k(h.saved)} tok` : `${k(h.savedAll)} all-time`, t.c.good),
          dim('gate', h.gate, good(h.gate === 'clear')), dim('daemon', h.daemon ? 'up' : 'down', good(h.daemon)),
        ] }),
        Text({ dimColor: true, children: '/deadeye config · theme · stats · help · /deadeye again closes' }),
      ] })
    }
    const w = Math.max(10, Math.min(28, e.props.bodyColumns - 2))
    const reticle = ['      │      ', '   ╭──┼──╮   ', '  ╱   │   ╲  ', ' ─┼───◎───┼─ ', '  ╲   │   ╱  ', '   ╰──┼──╯   ', '      │      ']
    const kv: [string, string, string][] = [
      ['level', h.level, t.c.fg], ['theme', t.name, t.c.fg], ['model', h.model || '—', t.c.fg], ['branch', h.branch || '—', t.c.fg],
      ['usage', h.usage === null ? '—' : `${h.usage}%${h.resetAt ? ` → reset ${h.resetAt}` : ''}`, h.usage !== null && h.usage >= 80 ? t.c.warn : t.c.fg],
      ['last route', h.route, t.c.fg],
      ['tokens saved', h.saved ? k(h.saved) : `${k(h.savedAll)} all-time`, t.c.good], ['plan gate', h.gate, good(h.gate === 'clear')],
      ['daemon', h.daemon ? 'up' : 'down', good(h.daemon)], ['repaint', h.repaint, h.repaint === 'refused' ? t.c.warn : t.c.dim],
    ]
    const why = h.repaint === 'refused' && repaintWhy ? [Text({ color: t.c.warn, wrap: 'wrap', children: `repaint: ${repaintWhy.slice(0, 200)}` }), Text({ dimColor: true, children: '/deadeye repaint retries · /theme picks it by hand' })] : []
    const row = (a: string, b: string, c: string) =>
      Box({ flexDirection: 'row', justifyContent: 'space-between', gap: 2, children: [Text({ dimColor: true, children: a }), Text({ color: c, children: b })] })
    return Box({ flexDirection: 'column', gap: 1, children: [
      Box({ flexDirection: 'column', alignItems: 'center', children: reticle.map(line => Text({ color: t.c.acc, children: line })) }),
      Box({ flexDirection: 'column', children: [
        row('routed right first time', pct(h.accuracy), t.c.fg),
        Text({ color: t.c.acc, children: bar(h.accuracy, w) }),
        row('context used', pct(h.context), t.c.fg),
        Text({ color: t.c.acc2, children: bar(h.context, w) }),
      ] }),
      Box({ flexDirection: 'column', children: kv.map(([a, b, c]) => row(a, b, c)) }),
      ...why,
      Box({ flexDirection: 'column', children: [
        Text({ color: t.c.dim, weight: 'bold', children: 'commands' }),
        ...COMMANDS.slice(0, 6).map(([c, d]) => Text({ children: [Text({ color: t.c.acc, children: c.padEnd(22) }), Text({ dimColor: true, children: d })] })),
        Text({ dimColor: true, children: '/deadeye help for the rest' }),
      ] }),
    ] })
  })

  // Theme picker: a list; enter keeps, esc leaves the theme as it was.
  on('ui.render', { component: 'Pane', requestId: PICKER }, async ($, e) => {
    const h = await read($, hud)
    const t = $.ui.resolve(e)
    const { Box, Text } = t
    const options = [
      ...THEMES.map(x => ({ key: x.slug, value: x.slug, label: `${x.glyph.tool} ${x.name.padEnd(18)} ${x.blurb}` })),
      { key: 'auto', value: 'auto', label: `↻ ${'Auto'.padEnd(18)} follows the coder level` },
    ]
    if (!('Select' in t)) {
      return Box({ flexDirection: 'column', children: options.map(o => Text({ color: o.value === h.theme ? theme.c.acc : undefined, children: o.label })) })
    }
    const { Select } = t
    return Box({ flexDirection: 'column', gap: 1, children: [
      Select({ key: 'theme', options, value: cfg.theme, autoFocus: true, onSelect: async (value: string) => {
        await applyTheme($, value, true)
        await $.ui.close({ id: PICKER })
        $.ui.toast(`deadeye theme: ${value === 'auto' ? `auto (${theme.name})` : theme.name}`)
      } }),
      Text({ dimColor: true, children: '↑↓ move · enter keep · esc cancel' }),
    ] })
  })
}

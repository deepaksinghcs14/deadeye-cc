import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register, RenderElement } from 'claude-code'

import type { Hud } from '../types'
import { THEMES, THEME_SLUGS, autoTheme, claudeThemeFile, themeBySlug, type Theme } from './themes'

// deadeye's Claude Code mod. Off until `deadeye config set mode.mod on`
// (a plugin.json userConfig switch would make Claude Code < 2.1.75 drop
// the whole plugin). On: a themed HUD band, spinner, finish line, tool
// rows, the scope pane (/deadeye) and theme picker (/deadeye theme), a
// level badge, a toast on every deadeye block, and -- with mod.repaint --
// Claude's own colours through a custom theme file. Display only: routing,
// the gates and the coder persona stay in the Go binary.

const PANE = 'deadeye'
type Tab = 'scope' | 'config' | 'themes' | 'stats' | 'help'
const TABS: [Tab, string][] = [['scope', '1'], ['config', '2'], ['themes', '3'], ['stats', '4'], ['help', '5']]
// The stats views, each one of the binary's own reports (the skill's table).
const STATS_VIEWS: [string, string, string[]][] = [
  ['impact', 'i', ['gain']], ['savings', 's', ['audit']], ['context', 'c', ['context']],
  ['accuracy', 'a', ['misses']], ['disagreement', 'd', ['disagreement']], ['adherence', 'h', ['adherence']],
]
const CONFIG_GROUPS: [string, string][] = [['mode', 'm'], ['mod', 'o'], ['coder', 'c'], ['security', 's'], ['tuning', 't']]
const COMMANDS: [string, string, string][] = [
  ['deadeye', '/deadeye', 'this pane (again closes it); 1-5 switch tabs'],
  ['deadeye', '/deadeye config', 'every setting; enter cycles a value'],
  ['deadeye', '/deadeye theme [name]', 'pick a theme, or set one by name'],
  ['deadeye', '/deadeye stats [view]', 'impact · savings · context · accuracy · disagreement · adherence'],
  ['deadeye', '/deadeye help', 'this card'],
  ['deadeye', '/deadeye repaint', 'retry recolouring Claude and the terminal'],
  ['review', '/deadeye-review [--repo]', 'four-lens self-review of the diff'],
  ['review', '/deadeye-pr [<PR>] [--post]', 'the same lenses on a GitHub PR'],
  ['review', '/deadeye-guard', 'OWASP-mapped security review of the diff'],
  ['review', '/deadeye-vapt', 'whole-service pen-test pass'],
  ['review', '/deadeye-sweep [--repo|--pr]', 'apply the findings, verify, re-scan'],
  ['review', '/deadeye-debt', 'ledger of deadeye: shortcut markers'],
  ['session', '/deadeye-status', 'modes, level, kill switches, catalog, daemon'],
  ['session', '/deadeye-coder [level]', 'spotter · marksman · sniper · off'],
  ['session', '/deadeye-route [task]', 'dry-run the routing decision'],
  ['session', '/deadeye-mute [off]', 'mute advisories this session'],
]
// Pane state: which tab, which stats view, which config group, the free-text
// key being edited, the last changed setting (shown with a tick), the report.
let tab: Tab = 'scope'
let statsView = 'impact'
let configGroup = 'mode'
let editingKey: string | null = null
let flash: { key: string; value: string } | null = null
let statsLines: string[] = []
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

function groupOf(key: string): string {
  const head = key.split('.')[0] ?? ''
  return key.includes('.') && CONFIG_GROUPS.some(([g]) => g === head) ? head : 'tuning'
}

async function redraw($: EngineInterface) {
  await update($, hud, v => ({ ...v, rev: v.rev + 1 }))
}

async function loadStats($: EngineInterface, view: string, extra: readonly string[] = []) {
  const entry = STATS_VIEWS.find(([v]) => v === view) ?? STATS_VIEWS[0]!
  statsView = entry[0]
  const ran = await deadeye($, [...entry[2], ...extra])
  statsLines = ran
    ? (ran.stdout + (ran.exitCode === 0 ? '' : '\n' + ran.stderr)).replace(/\x1b\[[0-9;]*m/g, '').replace(/\s+$/, '').split('\n')
    : ['deadeye binary not found (PATH or ~/.deadeye/bin).']
}

async function switchTab($: EngineInterface, to: Tab) {
  tab = to
  editingKey = null
  if (to === 'config') await loadConfigRows($)
  if (to === 'stats' && !statsLines.length) await loadStats($, statsView)
  await redraw($)
}

async function openPane($: EngineInterface, to: Tab) {
  await switchTab($, to)
  await $.ui.open({ id: PANE, title: 'deadeye', focus: true, closeOnEscape: true, rows: 22 })
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


// Pane actions, called from the pane's handlers.
async function changeSetting($: EngineInterface, key: string) {
  const row = configRows.find(r => r.key === key)
  if (!row) return
  if (key === 'mod.theme') { await switchTab($, 'themes'); return }
  if (!row.allowed) { editingKey = key; await redraw($); return }
  const next = row.allowed[(row.allowed.indexOf(row.value) + 1) % row.allowed.length] ?? row.allowed[0]!
  await submitSetting($, key, next)
}

async function submitSetting($: EngineInterface, key: string, value: string) {
  editingKey = null
  const ran = await deadeye($, ['config', 'set', key, value])
  if (!ran || ran.exitCode !== 0) {
    $.ui.toast(`deadeye: ${(ran?.stderr || ran?.stdout || 'config set failed').trim().slice(0, 140)}`)
    await redraw($)
    return
  }
  await loadConfigRows($)
  flash = { key, value }
  await redraw($)
  $.ui.toast(`${key} → ${value}${key.startsWith('mod.') ? ' (applies on the next turn)' : ''}`)
}

async function chooseTheme($: EngineInterface, value: string) {
  await applyTheme($, value, true)
  await redraw($)
  $.ui.toast(`deadeye theme: ${value === 'auto' ? `auto (${theme.name})` : theme.name}`)
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
    const words = e.args.trim().split(/\s+/).filter(Boolean)
    const [sub, name] = words
    if (/^(deadeye:)?deadeye(-|$)/.test(e.command)) await markUsed($, e.command, sub ?? '')
    // Mod off: the /deadeye skill runs as it always has (status, config, theme from chat).
    if (!isOurCommand(e.command) || !cfg.enabled) return next(e)
    if (sub === 'stats') {
      const view = name && STATS_VIEWS.some(([v]) => v === name) ? name : 'impact'
      await loadStats($, view, view === 'context' ? words.slice(2) : [])
      await openPane($, 'stats')
      return { text: `deadeye · stats · ${view} opened; i/s/c/a/d/h switch views, 1-5 tabs, esc closes. Figures are the binary's own; estimates and measurements are labelled as such.` }
    }
    if (sub === 'help') {
      await openPane($, 'help')
      return { text: 'deadeye · help opened; 1-5 switch tabs, esc closes.' }
    }
    if (sub === 'config') {
      await openPane($, 'config')
      return { text: configWhy ? `deadeye config: ${configWhy}` : 'deadeye · config opened: m/o/c/s/t pick a group, ↑↓ move, enter cycles a value (or edits a free-text one), esc closes.' }
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
      await openPane($, 'themes')
      return { text: 'deadeye · themes opened: ↑↓ move, enter applies (and stays), esc closes.' }
    }
    const shown = (await $.ui.panes().catch(() => [])).some(p => p.id === PANE && p.isShown)
    if (sub === 'close' || (shown && !sub)) {
      await $.ui.close({ id: PANE })
      return { text: 'deadeye pane closed.' }
    }
    await openPane($, 'scope')
    return { text: 'deadeye pane opened: 1 scope · 2 config · 3 themes · 4 stats · 5 help · esc closes. /deadeye again closes it.' }
  }).catch(($, e, next) => next(e))

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

  // The pane: one pane, five tabs, keys to roam. Elements are built by
  // calling the surface's constructors (no JSX in the installed runtime).
  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const h = await read($, hud)
    const t = themeFor(h)
    const el = $.ui.resolve(e)
    const { Box, Text } = el
    const inline = e.props.placement === 'inline'
    const cols = Math.max(40, e.props.bodyColumns)
    const dim = (text: string) => Text({ dimColor: true, children: text })
    const good = (ok: boolean) => (ok ? t.c.good : t.c.warn)
    const hasButtons = 'Button' in el
    const keyed = (hotkey: string, label: string, selected: boolean, onPress: () => void) =>
      hasButtons
        ? el.Button({ key: `k-${hotkey}-${label}`, hotkey, plain: true, label, dimColor: !selected, onPress })
        : Text({ color: selected ? t.c.acc : undefined, dimColor: !selected, children: `${hotkey}: ${label}` })

    const tabBar = Box({ flexDirection: 'row', gap: 3, flexWrap: 'wrap', children: [
      ...TABS.map(([id, key]) => keyed(key, id, id === tab, () => { void switchTab($, id) })),
      dim('esc close'),
    ] })
    const rule = Text({ color: t.c.line, children: '─'.repeat(Math.min(cols, 60)) })

    let body: RenderElement[] = []
    let footer = ''

    if (tab === 'scope') {
      const w = inline ? 10 : Math.min(24, cols - 30)
      const meter = (label: string, value: number | null, color: string) =>
        Box({ flexDirection: 'row', gap: 1, children: [dim(label.padEnd(14)), Text({ color, children: bar(value, w) }), Text({ color: t.c.fg, children: pct(value) })] })
      const row = (a: string, b: string, c: string) => Box({ flexDirection: 'row', gap: 1, children: [dim(a.padEnd(14)), Text({ color: c, children: b })] })
      body = [
        Text({ children: [Text({ color: t.c.acc, weight: 'bold', children: `${t.glyph.tool} ${h.level.toUpperCase()}` }), dim(` · ${t.name} · ${h.model || '—'} · ⎇ ${h.branch || '—'}`)] }),
        meter('routed right', h.accuracy, t.c.acc),
        meter('context', h.context, h.context !== null && h.context >= 80 ? t.c.warn : t.c.acc2),
        ...(h.usage === null ? [] : [meter('usage', h.usage, h.usage >= 80 ? t.c.warn : t.c.acc2)]),
        Box({ flexDirection: 'row', gap: 3, flexWrap: 'wrap', children: [
          Box({ flexDirection: 'column', children: [
            row('last route', h.route, t.c.fg),
            row('tokens saved', h.saved ? k(h.saved) : `${k(h.savedAll)} all-time`, t.c.good),
            row('plan gate', h.gate, good(h.gate === 'clear')),
          ] }),
          Box({ flexDirection: 'column', children: [
            row('daemon', h.daemon ? 'up' : 'down', good(h.daemon)),
            row('reset', h.resetAt ?? '—', t.c.fg),
            row('repaint', h.repaint, h.repaint === 'refused' ? t.c.warn : t.c.dim),
            row('layout', `${e.viewport?.isFullscreen ? 'fullscreen' : 'main screen'} · ${e.viewport?.columns ?? '?'} cols · ${inline ? 'inline' : 'docked'}`, inline ? t.c.warn : t.c.dim),
          ] }),
        ] }),
        ...(inline ? [dim('docks beside the transcript at 110+ cols in fullscreen mode: start Claude with CLAUDE_CODE_NO_FLICKER=1; tmux stays inline')] : []),
        ...(h.repaint === 'refused' && repaintWhy ? [Text({ color: t.c.warn, wrap: 'wrap', children: `repaint: ${repaintWhy.slice(0, 160)} · /deadeye repaint retries` })] : []),
      ]
      footer = '2 config · 3 themes · 4 stats · 5 help'
    }

    if (tab === 'config') {
      const groupBar = Box({ flexDirection: 'row', gap: 3, flexWrap: 'wrap', children: CONFIG_GROUPS.map(([g, key]) => keyed(key, g, g === configGroup, () => { configGroup = g; editingKey = null; void redraw($) })) })
      const rows = configRows.filter(r => groupOf(r.key) === configGroup)
      if (!configRows.length) {
        body = [groupBar, Text({ color: t.c.warn, children: configWhy || 'no settings read; press 2 again' })]
      } else {
        const width = Math.max(...rows.map(r => r.key.length)) + 2
        const label = (r: ConfigRow) => {
          const nextValue = r.allowed ? r.allowed[(r.allowed.indexOf(r.value) + 1) % r.allowed.length] : null
          const mark = flash?.key === r.key ? ' ✓' : ''
          return `${r.key.padEnd(width)} ${r.value.padEnd(11)} ${r.allowed ? `▸ ${nextValue}` : '✎ free text'}${mark}`
        }
        const list = 'Select' in el
          ? el.Select({ key: `cfg-${configGroup}`, options: rows.map(r => ({ key: r.key, value: r.key, label: label(r) })), autoFocus: editingKey ? undefined : true, onSelect: (key: string) => { void changeSetting($, key) } })
          : Box({ flexDirection: 'column', children: rows.map(r => Text({ children: label(r) })) })
        const editor = editingKey && 'Input' in el
          ? el.Input({ key: `edit-${editingKey}`, label: `${editingKey} =`, value: configRows.find(r => r.key === editingKey)?.value.replace('(unset)', '') ?? '', autoFocus: true, onSubmit: (value: string) => { void submitSetting($, editingKey!, value) } })
          : null
        body = [groupBar, list, ...(editor ? [editor, dim('enter writes · esc cancels')] : [])]
      }
      footer = 'm/o/c/s/t group · ↑↓ move · enter cycles the value, or edits a free-text one'
    }

    if (tab === 'themes') {
      const swatch = Box({ flexDirection: 'row', gap: 1, children: [
        ...['bg', 'acc', 'acc2', 'warn', 'bad', 'good'].map(key => Text({ backgroundColor: t.c[key as keyof typeof t.c], children: '   ' })),
        dim(`${t.name} · spinner: ${t.verbs.join(' → ')} · "${t.done}"`),
      ] })
      const options = [
        ...THEMES.map(x => ({ key: x.slug, value: x.slug, label: `${x.glyph.tool} ${x.name.padEnd(18)} ${x.blurb}` })),
        { key: 'auto', value: 'auto', label: `↻ ${'Auto'.padEnd(18)} follows the coder level` },
      ]
      const list = 'Select' in el
        ? el.Select({ key: 'themes', options, value: cfg.theme, autoFocus: true, onSelect: (value: string) => { void chooseTheme($, value) } })
        : Box({ flexDirection: 'column', children: options.map(o => Text({ color: o.value === cfg.theme ? t.c.acc : undefined, children: o.label })) })
      body = [swatch, list]
      footer = '↑↓ move · enter applies and stays here · themes.html shows them all'
    }

    if (tab === 'stats') {
      const viewBar = Box({ flexDirection: 'row', gap: 3, flexWrap: 'wrap', children: STATS_VIEWS.map(([v, key]) => keyed(key, v, v === statsView, () => { void loadStats($, v).then(() => redraw($)) })) })
      const lines = statsLines.length ? statsLines : ['loading…']
      const styled = lines.map(line => {
        if (!line.trim()) return Text({ children: ' ' })
        const indent = line.length - line.trimStart().length
        const text = line.trim()
        if (text.includes('file://')) return Text({ color: t.c.acc, children: text })
        if (indent === 0) return Text({ color: t.c.acc, weight: 'bold', children: text })
        const m = /^(\S+)\s{2,}(.*)$/.exec(text)
        if (indent <= 2 && m && /^[A-Z]/.test(m[1]!)) return Text({ children: [Text({ weight: 'bold', children: m[1]!.padEnd(16) }), Text({ color: t.c.fg, children: m[2]! })] })
        if (m && /^(estimated|measured)$/.test(m[1]!)) return Text({ children: ['    ', Text({ color: m[1] === 'measured' ? t.c.good : t.c.warn, children: m[1]!.padEnd(12) }), dim(m[2]!)] })
        if (m) return Text({ children: ['  '.repeat(Math.max(1, Math.floor(indent / 2))), dim(m[1]!.padEnd(24)), Text({ color: t.c.fg, children: m[2]! })] })
        return Text({ dimColor: true, wrap: 'wrap', children: '  '.repeat(Math.max(1, Math.floor(indent / 2))) + text })
      })
      body = [viewBar, ...styled]
      footer = "i/s/c/a/d/h views · figures are the binary's own; estimated and measured are labelled"
    }

    if (tab === 'help') {
      const group = (name: string) => [
        Text({ color: t.c.acc, weight: 'bold', children: name }),
        ...COMMANDS.filter(([g]) => g === name).map(([, c, d]) => Text({ children: [Text({ color: t.c.fg, children: c.padEnd(30) }), dim(d)] })),
      ]
      body = [
        ...group('deadeye'), Text({ children: ' ' }), ...group('review'), Text({ children: ' ' }), ...group('session'), Text({ children: ' ' }),
        Text({ color: t.c.acc, weight: 'bold', children: 'kill switches' }),
        dim('DEADEYE=off everything · DEADEYE_PREPROCESS=off context hygiene · DEADEYE_GATE=off plan gate · DEADEYE_CODER=off persona'),
        Text({ color: t.c.acc, weight: 'bold', children: 'coder levels' }),
        dim('spotter names the leaner way · marksman enforces the ladder (default) · sniper ships the one-liner · "normal mode" turns it off'),
      ]
      footer = 'settings: /deadeye config · looks: /deadeye theme'
    }

    return Box({ flexDirection: 'column', gap: inline ? 0 : 1, children: [tabBar, rule, ...body, dim(footer)] })
  })
}

import { expect, mock, test } from 'claude-code/testing'

const DENY = 'deadeye: plan gate triggered -- multi-file change'

type On = Parameters<Parameters<typeof test>[1]>[1]

// Stands in for the host: ~/.deadeye holds config.json (mode.mod, mod.*)
// and this session's coder-mode file; status, toasts, theme writes and
// config changes are recorded.
function host(on: On, mod: 'on' | 'off' | undefined, extra: Record<string, string> = {}) {
  const seen = { toasts: [] as string[], statuses: [] as (string | undefined)[], writes: {} as Record<string, string>, config: [] as [string, unknown][], argv: [] as string[][] }
  const files: Record<string, string> = { '/home/u/.deadeye/coder-mode.s-1': 'sniper\n' }
  if (mod) files['/home/u/.deadeye/config.json'] = JSON.stringify({ mode: { mod }, mod: extra })

  mock.env(on, { HOME: '/home/u' })
  on('command.register', () => ({ value: { command: 'deadeye' } }))
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('session.id', () => ({ value: 's-1' }))
  on('session.usage', () => ({ value: { startedAt: 0, context: { window: 200000, tokens: 76000, percent: 38 }, rateLimits: [] } }))
  on('fs.read', (_, e) => (e.path in files ? { value: files[e.path] } : { deny: 'ENOENT' }))
  on('fs.stat', (_, e) => (e.path.endsWith('deadeye.sock') ? { value: { kind: 'other', size: 0 } } : { deny: 'ENOENT' }))
  on('fs.write', (_, e) => ((seen.writes[e.path] = e.text), { value: undefined }))
  on('process.run', (_, e) => (seen.argv.push([...e.argv]), { value: { exitCode: 0, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }))
  on('config.list', () => ({ value: [{ key: 'theme', label: 'Theme', kind: 'choice', value: 'dark', provider: { kind: 'user' }, isLocked: false }] }))
  on('config.set', (_, e) => (seen.config.push([e.key, e.value]), { value: e.value }))
  on('store.get', () => ({ value: undefined }))
  on('store.set', () => ({ value: undefined }))
  on('ui.status', (_, e) => (seen.statuses.push(e.text), { value: undefined }))
  on('ui.open', () => ({ value: { isPlaced: false, reason: 'narrow' } }))
  on('ui.toast', (_, e) => (seen.toasts.push(e.text), { value: undefined }))
  return seen
}

const start = { cwd: '/', surface: 'terminal', isInteractive: true } as const

for (const mod of [undefined, 'off'] as const) {
  test(`mode.mod ${mod ?? 'unset'}: no badge, no toast, no repaint`, async ($, on) => {
    const seen = host(on, mod)
    on('classic.PreToolUse', () => ({ deny: DENY }))
    on('tool.call', () => ({ result: {} }))

    await $.session.start(start)
    await $.tool.call({ tool: 'Edit', file_path: '/a', old_string: 'a', new_string: 'b' })

    expect(seen.statuses.filter(Boolean)).toEqual([])
    expect(seen.toasts).toEqual([])
    expect(seen.config).toEqual([])
    expect(Object.keys(seen.writes)).toEqual([])
  })
}

test('mode.mod on: no status-line notice, Claude repainted to the configured theme', async ($, on) => {
  const seen = host(on, 'on', { theme: 'tokyonight' })

  await $.session.start(start)

  expect(seen.statuses).toEqual([])
  expect(seen.config).toEqual([['theme', 'custom:deadeye-tokyonight']])
  const file = seen.writes['/home/u/.claude/themes/deadeye-tokyonight.json']
  expect(JSON.parse(file).overrides.claude).toBe('#7aa2f7')
})

test('mode.mod on, repaint off: nothing written to Claude', async ($, on) => {
  const seen = host(on, 'on', { theme: 'ghost', repaint: 'off' })

  await $.session.start(start)

  expect(seen.config).toEqual([])
  expect(Object.keys(seen.writes)).toEqual([])
})

test("mode.mod on: themed toast on deadeye's shell PreToolUse deny", async ($, on) => {
  const seen = host(on, 'on', { theme: 'nightvision', repaint: 'off' })
  on('classic.PreToolUse', () => ({ deny: DENY }))
  on('tool.call', () => ({ result: {} }))

  await $.session.start(start)
  await $.tool.call({ tool: 'Edit', file_path: '/a', old_string: 'a', new_string: 'b' })

  expect(seen.toasts.length).toBe(1)
  expect(seen.toasts[0]).toStartWith('NO-FIRE · plan gate')
})

test('/deadeye theme <name> switches and persists through the deadeye binary', async ($, on) => {
  const seen = host(on, 'on', { theme: 'marksman', repaint: 'off' })

  await $.session.start(start)
  const ran = await $.command.run({ command: 'deadeye', args: 'theme dracula' })

  expect(ran.text).toBe('deadeye theme: Dracula.')
  expect(seen.argv.some(a => a.slice(-4).join(' ') === 'config set mod.theme dracula')).toBe(true)
  expect(seen.toasts).toEqual([])
})

test('/deadeye theme <unknown> lists the themes', async ($, on) => {
  host(on, 'on', { repaint: 'off' })
  await $.session.start(start)
  const ran = await $.command.run({ command: 'deadeye', args: 'theme neon' })
  expect(ran.text).toContain('no theme "neon"')
  expect(ran.text).toContain('tokyonight')
})

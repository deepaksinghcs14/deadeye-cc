import { expect, mock, test } from 'claude-code/testing'

// mode.mod flipped on in a running session: the next turn.complete picks it
// up (badge, command, repaint); flipped off, the next turn clears it.
test('mode.mod toggled mid-session applies on the next turn', async ($, on) => {
  let modOn = false
  const statuses: (string | undefined)[] = []
  const config: [string, unknown][] = []
  const registered: string[] = []
  mock.env(on, { HOME: '/home/u' })
  on('command.register', (_, e) => (registered.push(e.name), { value: { command: e.name } }))
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('session.id', () => ({ value: 's-1' }))
  on('session.usage', () => ({ value: { startedAt: 0, context: { window: 1000, tokens: 100, percent: 10 }, rateLimits: [] } }))
  on('fs.read', (_, e) => {
    if (e.path.endsWith('config.json')) return { value: JSON.stringify({ mode: { mod: modOn ? 'on' : 'off' }, mod: { theme: 'nord' } }) }
    if (e.path.endsWith('coder-mode.s-1')) return { value: 'marksman' }
    return { deny: 'ENOENT' }
  })
  on('fs.stat', () => ({ deny: 'ENOENT' }))
  on('fs.write', () => ({ value: undefined }))
  on('process.run', () => ({ value: { exitCode: 0, stdout: '', stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }))
  on('config.list', () => ({ value: [{ key: 'theme', label: 'Theme', kind: 'choice', value: config.at(-1)?.[1] ?? 'dark', provider: { kind: 'user' }, isLocked: false }] }))
  on('config.set', (_, e) => (config.push([e.key, e.value]), { value: e.value }))
  let prev: unknown
  on('store.get', () => ({ value: prev }))
  on('store.set', (_, e) => ((prev = e.value), { value: undefined }))
  on('ui.status', (_, e) => (statuses.push(e.text), { value: undefined }))
  on('ui.open', () => ({ value: { isPlaced: false, reason: 'narrow' } }))
  on('ui.toast', () => ({ value: undefined }))
  on('turn.complete', () => ({ text: 'ok' }))

  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })

  modOn = true
  await $.turn.complete({ reason: 'answer', answer: 'ok', durationMs: 1000, isAborted: false, turnId: 't1' })
  expect(config.at(-1)).toEqual(['theme', 'custom:deadeye-nord'])

  modOn = false
  await $.turn.complete({ reason: 'answer', answer: 'ok', durationMs: 1000, isAborted: false, turnId: 't1' })
  expect(statuses).toEqual([])
  expect(config.at(-1)).toEqual(['theme', 'dark'])
})

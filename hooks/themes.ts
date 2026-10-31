// The deadeye theme table. Each theme styles what the mod draws (HUD band,
// spinner, finish line, tool rows, scope pane) and, with mod.repaint on,
// Claude Code's own colours through a custom theme file. Community palettes
// are ports of their published colours (all MIT-licensed).

export type Palette = {
  bg: string; fg: string; dim: string; acc: string; acc2: string
  warn: string; bad: string; good: string; line: string; band: string
}

export type Theme = {
  slug: string
  name: string
  blurb: string
  family: 'deadeye' | 'community'
  light?: true
  c: Palette
  glyph: { tool: string; done: string }
  spin: readonly string[]
  verbs: readonly string[]
  done: string
  tag: string
  pane: string
}

const DEF = {
  glyph: { tool: '◎', done: '✓' },
  spin: ['◜', '◝', '◞', '◟'],
  verbs: ['Sighting', 'Steadying', 'Breathing out', 'Squeezing'],
  done: 'On target',
  tag: 'HOLD',
  pane: 'Scope',
} as const

type Spec = Omit<Theme, keyof typeof DEF | 'slug'> & Partial<Pick<Theme, keyof typeof DEF>>

const T = (slug: string, s: Spec): Theme => ({ slug, ...DEF, ...s })

export const THEMES: readonly Theme[] = [
  T('marksman', { name: 'Marksman', blurb: 'Calm sage. The default.', family: 'deadeye',
    c: { bg: '#101512', fg: '#d3dbd5', dim: '#6f7d74', acc: '#87af87', acc2: '#87afaf', warn: '#e0a95a', bad: '#e07a6a', good: '#87af87', line: '#26332b', band: '#16201a' } }),
  T('nightvision', { name: 'Night Vision', blurb: 'Phosphor and scanlines.', family: 'deadeye',
    c: { bg: '#020a04', fg: '#9dffad', dim: '#3c8a4b', acc: '#3dff6e', acc2: '#b6ff3d', warn: '#d6ff4a', bad: '#ff6b5a', good: '#3dff6e', line: '#0f3a1a', band: '#06160a' },
    glyph: { tool: '⌖', done: '◉' }, spin: ['░', '▒', '▓', '▒'], verbs: ['Scanning', 'Acquiring', 'Tracking', 'Locking'], done: 'Target down', tag: 'NO-FIRE', pane: 'Thermal' }),
  T('highnoon', { name: 'High Noon', blurb: 'Dust, brass, quick draw.', family: 'deadeye',
    c: { bg: '#1c140c', fg: '#efdcbc', dim: '#8f7658', acc: '#e3953f', acc2: '#c9a86a', warn: '#ffcf6b', bad: '#e0664f', good: '#b5c46a', line: '#3a2a1a', band: '#251a10' },
    glyph: { tool: '✶', done: '✦' }, spin: ['·', '✶', '✷', '✶'], verbs: ['Drawing', 'Cocking', 'Aiming', 'Firing'], done: 'Fastest draw', tag: 'HOLSTERED', pane: 'Wanted' }),
  T('blueprint', { name: 'Blueprint', blurb: 'Drafting grid for reviews.', family: 'deadeye',
    c: { bg: '#0b1a2e', fg: '#d4e4f7', dim: '#5f7fa8', acc: '#73b6ff', acc2: '#9fd0ff', warn: '#ffd36b', bad: '#ff8a7a', good: '#7fe0b0', line: '#16304f', band: '#0f2340' },
    glyph: { tool: '▸', done: '✓' }, spin: ['┤', '┘', '┴', '└', '├', '┌', '┬', '┐'], verbs: ['Measuring', 'Drafting', 'Checking tolerances', 'Inking'], done: 'Drafted', tag: 'REVISE', pane: 'Spec sheet' }),
  T('ghost', { name: 'Ghost', blurb: 'Mono and quiet.', family: 'deadeye',
    c: { bg: '#0f0f0f', fg: '#d0d0d0', dim: '#666666', acc: '#f2f2f2', acc2: '#a8a8a8', warn: '#bdbdbd', bad: '#ff8f8f', good: '#e0e0e0', line: '#262626', band: '#151515' },
    glyph: { tool: '·', done: '·' }, spin: ['.  ', '.. ', '...', ' ..', '  .', '   '], verbs: ['Thinking'], done: 'Done', tag: 'BLOCKED', pane: 'Status' }),

  T('tokyonight', { name: 'Tokyo Night', blurb: 'Neon city after dark.', family: 'community',
    c: { bg: '#1a1b26', fg: '#c0caf5', dim: '#565f89', acc: '#7aa2f7', acc2: '#bb9af7', warn: '#ff9e64', bad: '#f7768e', good: '#9ece6a', line: '#292e42', band: '#1f2335' },
    glyph: { tool: '◆', done: '◆' }, spin: ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'], verbs: ['Tracing neon', 'Sighting', 'Locking'] }),
  T('catppuccin', { name: 'Catppuccin Mocha', blurb: 'Soft pastels. A hunting cat.', family: 'community',
    c: { bg: '#1e1e2e', fg: '#cdd6f4', dim: '#6c7086', acc: '#cba6f7', acc2: '#f5c2e7', warn: '#fab387', bad: '#f38ba8', good: '#a6e3a1', line: '#313244', band: '#181825' },
    glyph: { tool: '◉', done: '❀' }, spin: ['◐', '◓', '◑', '◒'], verbs: ['Stalking', 'Crouching', 'Wiggling', 'Pouncing'], done: 'Pounced', pane: 'Whiskers' }),
  T('rosepine', { name: 'Rosé Pine', blurb: 'Muted rose, low contrast.', family: 'community',
    c: { bg: '#191724', fg: '#e0def4', dim: '#6e6a86', acc: '#ebbcba', acc2: '#c4a7e7', warn: '#f6c177', bad: '#eb6f92', good: '#9ccfd8', line: '#26233a', band: '#1f1d2e' },
    glyph: { tool: '✿', done: '✿' }, spin: ['✿', '❀', '✾', '❁'], verbs: ['Sighting', 'Steadying', 'Blooming'] }),
  T('gruvbox', { name: 'Gruvbox', blurb: 'Warm retro groove.', family: 'community',
    c: { bg: '#282828', fg: '#ebdbb2', dim: '#928374', acc: '#fabd2f', acc2: '#8ec07c', warn: '#fe8019', bad: '#fb4934', good: '#b8bb26', line: '#3c3836', band: '#1d2021' },
    glyph: { tool: '■', done: '■' }, spin: ['▖', '▘', '▝', '▗'] }),
  T('dracula', { name: 'Dracula', blurb: 'Purple night, bright ink.', family: 'community',
    c: { bg: '#282a36', fg: '#f8f8f2', dim: '#6272a4', acc: '#bd93f9', acc2: '#ff79c6', warn: '#ffb86c', bad: '#ff5555', good: '#50fa7b', line: '#44475a', band: '#21222c' },
    glyph: { tool: '▲', done: '▲' }, spin: ['◢', '◣', '◤', '◥'], verbs: ['Stalking', 'Hunting', 'Sinking teeth'], done: 'Fed', tag: 'SUNLIGHT', pane: 'Crypt' }),
  T('nord', { name: 'Nord', blurb: 'Arctic frost, calm blues.', family: 'community',
    c: { bg: '#2e3440', fg: '#d8dee9', dim: '#616e88', acc: '#88c0d0', acc2: '#81a1c1', warn: '#ebcb8b', bad: '#bf616a', good: '#a3be8c', line: '#3b4252', band: '#272c36' },
    glyph: { tool: '❄', done: '❄' }, spin: ['·', '∙', '•', '∙'], verbs: ['Sighting', 'Holding still', 'Squeezing'], done: 'Clean shot' }),
  T('kanagawa', { name: 'Kanagawa', blurb: 'The Great Wave, in ink.', family: 'community',
    c: { bg: '#1f1f28', fg: '#dcd7ba', dim: '#727169', acc: '#7e9cd8', acc2: '#d27e99', warn: '#e6c384', bad: '#e46876', good: '#98bb6c', line: '#2a2a37', band: '#16161d' },
    glyph: { tool: '≈', done: '➶' }, spin: ['∼', '≈', '≋', '≈'], verbs: ['Drawing the bow', 'Aiming', 'Releasing'], done: 'Arrow home', pane: 'Kyūdō' }),
  T('everforest', { name: 'Everforest', blurb: 'Green, easy on the eyes.', family: 'community',
    c: { bg: '#2d353b', fg: '#d3c6aa', dim: '#859289', acc: '#a7c080', acc2: '#83c092', warn: '#dbbc7f', bad: '#e67e80', good: '#a7c080', line: '#3d484d', band: '#272e33' },
    glyph: { tool: '▲', done: '✓' }, spin: ['⡀', '⠄', '⠂', '⠁', '⠂', '⠄'], verbs: ['Tracking', 'Waiting downwind', 'Taking the shot'] }),
  T('synthwave', { name: "Synthwave '84", blurb: 'Retro neon with glow.', family: 'community',
    c: { bg: '#262335', fg: '#ffffff', dim: '#848bbd', acc: '#ff7edb', acc2: '#36f9f6', warn: '#fede5d', bad: '#fe4450', good: '#72f1b8', line: '#34294f', band: '#1e1a2b' },
    glyph: { tool: '▶', done: '★' }, spin: ['▁', '▃', '▅', '▇', '▅', '▃'], verbs: ['Charging', 'Locking on', 'Firing'], done: 'Hit' }),
  T('solarized', { name: 'Solarized Dark', blurb: 'The classic, precise.', family: 'community',
    c: { bg: '#002b36', fg: '#93a1a1', dim: '#586e75', acc: '#268bd2', acc2: '#2aa198', warn: '#b58900', bad: '#dc322f', good: '#859900', line: '#073642', band: '#00222b' },
    glyph: { tool: '☉', done: '☉' }, spin: ['◴', '◷', '◶', '◵'] }),
  T('dawn', { name: 'Rosé Pine Dawn', blurb: 'Light theme for daytime.', family: 'community', light: true,
    c: { bg: '#faf4ed', fg: '#575279', dim: '#9893a5', acc: '#d7827e', acc2: '#907aa9', warn: '#ea9d34', bad: '#b4637a', good: '#56949f', line: '#f2e9e1', band: '#fffaf3' },
    glyph: { tool: '✿', done: '✿' }, spin: ['✿', '❀', '✾', '❁'], verbs: ['Sighting', 'Steadying', 'Blooming'] }),
]

export const THEME_SLUGS = THEMES.map(t => t.slug)

export function themeBySlug(slug: string | undefined): Theme | undefined {
  return THEMES.find(t => t.slug === slug)
}

// `auto` follows the coder level, so the look says what mode you're in.
export function autoTheme(level: string | undefined): Theme {
  const map: Record<string, string> = { review: 'blueprint', sniper: 'nightvision', off: 'ghost' }
  const key = level?.endsWith('off') ? 'off' : level ?? ''
  return themeBySlug(map[key] ?? 'marksman')!
}

// Claude Code's custom theme file (~/.claude/themes/<slug>.json): a base
// theme plus overrides keyed as the custom theme reference lists them.
export function claudeThemeFile(t: Theme): string {
  return JSON.stringify({
    name: `deadeye ${t.name}`,
    base: t.light ? 'light' : 'dark',
    overrides: {
      claude: t.c.acc,
      promptBorder: t.c.acc2,
      bashBorder: t.c.warn,
      permission: t.c.acc2,
      planMode: t.c.good,
      autoAccept: t.c.warn,
      success: t.c.good,
      error: t.c.bad,
      warning: t.c.warn,
      suggestion: t.c.acc2,
      inactive: t.c.dim,
      subtle: t.c.line,
      diffAdded: t.c.good,
      diffRemoved: t.c.bad,
      userMessageBackground: t.c.band,
    },
  }, null, 2) + '\n'
}

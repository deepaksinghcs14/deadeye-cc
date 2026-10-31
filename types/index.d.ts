// deadeye's `$.state` contract: what the mod keeps for the session so the
// HUD band, scope pane and picker redraw when a value changes.
export type Hud = {
  level: string
  theme: string
  route: string
  saved: number
  savedAll: number
  gate: 'clear' | 'holding'
  accuracy: number | null
  context: number | null
  daemon: boolean
  repaint: 'on' | 'off' | 'refused'
  model: string
  branch: string
  usage: number | null
  resetAt: string | null
}

declare module 'claude-code' {
  interface PluginState {
    deadeye: { hud: Shaped<Hud> }
  }
}

# Contributing to deadeye

Thanks for taking a shot. Ground rules first, then the mechanics.

## The one rule that matters

deadeye's whole design is **fail open, never block** (INV-5 in the code
comments): if anything inside deadeye errors, the user's tool call must
pass through untouched. Any change that can make a hook hang, crash
loudly, or block a tool call will be rejected regardless of what else it
does. When in doubt, return `{}`.

Second rule, same weight: **when it doesn't know, it goes big** (INV-1).
Missing or shaky evidence must never buy a cheaper model or lower
effort. If your change touches `internal/kernel` or `internal/signals`,
the conservative direction is the correct direction.

## Building and testing

```bash
make check   # vet, gofmt, tests -- exactly what CI runs
make build   # ./bin/deadeye
```

Go version comes from `go.mod`. No other toolchain needed; CI also runs
`bash -n hooks/*.sh`, so keep the hook scripts POSIX-bash clean (macOS
ships bash 3.2 -- no associative arrays, no `declare -A`).

## What a good change looks like

- **Bugs**: a fix goes to the root cause every caller routes through,
  not the one path the report named. Include a regression test that
  FAILS against the old code -- verify that yourself by reverting your
  fix locally and watching the test fail. Several past fixes here
  introduced adjacent bugs of their own; the test suite is the only
  thing that catches that pattern.
- **Filter/regex rules** (`internal/preprocess`, `internal/secscan`):
  verify against REAL captured tool output, not hand-written
  approximations -- hand-written fixtures are exactly how two broken
  filters shipped historically. Real captures live in
  `internal/preprocess/testdata/`; add yours there.
- **New rules or advisories**: deadeye advises far more often than it
  acts. A new advisory must be high-precision -- a rule that false-fires
  on ordinary code is worse than no rule. Bring counter-examples you
  tested, not just examples.
- **Tests**: every non-trivial change carries one. `testdata/payloads/`
  holds real captured hook payloads for the JSON contract.

## What gets declined

- New dependencies for what a few lines of stdlib can do (the module
  currently has zero -- keep it that way unless something truly earns
  its place).
- Anything that phones home, adds telemetry, or makes a network call on
  a hook's hot path. The OSV lookup runs off-path in the daemon by
  design; that's the ceiling.
- Speculative abstractions -- interfaces with one implementation,
  config for values that never change.
- Changes that touch the user's Claude Code `settings.json`. deadeye
  keeps its own state under `~/.deadeye/` only.

## Commit messages

Subject says why, imperative mood, no filler -- the diff already shows
what. Body explains the reasoning and names what was verified. Look at
`git log` for the house style.

## Deliberate shortcuts

A knowingly-cut corner gets a marker in this exact grammar so
`/deadeye-debt` can track it:

```
deadeye: <shortcut>. ceiling: <limit>. upgrade: <trigger>.
```

`TODO` is for work not done yet. Never use one for the other.

## Model catalogs

`scripts/gen-catalog.go` regenerates the compiled-in model/pricing table
after editing its seed prices — a release-time step, since there's no
reachable pricing API at runtime. It also writes `docs/site/catalog.json`,
a hosted copy every install refreshes in the background (`mode.catalog_check`,
~24h, read-only, toggle with `deadeye config set mode.catalog_check off`)
and prefers over the compiled-in table
when it's present and well-formed. That file can also be hand-edited
directly and pushed — no release needed to roll out a new model or price,
though the seed table should be brought back in sync before the next
`go run` overwrites it.

Each host has its own catalog, because a model id only means something to
the host that runs it:

| Host | Hosted file | Local override | Used for |
|---|---|---|---|
| Claude Code | `catalog.json` | `~/.deadeye/catalog.json` | Routing, advice, stats |
| Codex CLI | `catalog-codex.json` | `~/.deadeye/catalog-codex.json` | Display and stats |
| Gemini CLI | `catalog-gemini.json` | `~/.deadeye/catalog-gemini.json` | Display and stats |
| Cursor | `catalog-cursor.json` | `~/.deadeye/catalog-cursor.json` | Display and stats |

The Cursor table lists Cursor's own models only; it also resells the other
vendors' models. `deadeye status` prints all four.

## Releases (maintainers)

1. Move the `## Unreleased` notes in `CHANGELOG.md` under a new version
   heading.
2. Bump `version` in `.claude-plugin/plugin.json` AND `Version` in
   `internal/meta/meta.go` (the `-dev` suffix stays; goreleaser
   overrides it at build time).
3. Commit, then tag `vX.Y.Z` matching plugin.json exactly -- the
   release workflow refuses a mismatched tag -- and push the tag.
   goreleaser builds and publishes the binaries.

## Questions

Open an issue. Attach `/deadeye-route` or `/deadeye-status` output when
it's about a routing or configuration decision -- every decision deadeye
makes is printable, so show the print.

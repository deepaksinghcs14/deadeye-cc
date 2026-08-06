## What and why

<!-- One or two sentences. The diff shows what; say why. -->

## Verification

- [ ] `make check` passes locally
- [ ] Bug fix: the included regression test FAILS against the old code (I reverted and watched it fail)
- [ ] Filter/regex change: verified against real captured tool output, not a hand-written approximation
- [ ] No new dependencies, no network calls on a hook's hot path, fails open on error

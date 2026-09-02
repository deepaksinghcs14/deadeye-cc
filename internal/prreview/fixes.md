## Suggested fixes

For each finding whose fix is concrete and mechanical — not a judgment call
("which auth policy is correct," "what should this business rule be") —
add the replacement as a fenced code block right after the finding line:
minimal, just the changed lines plus a line or two of context, language-tagged.
Skip the snippet and keep the prose `Fix:` alone when the right fix genuinely
needs a human decision. Same proof discipline as everywhere else in this
rubric: never fabricate a plausible-looking snippet for a fix you're not
sure of.

## Copy for AI

After the tally, print one more block: every finding that survived,
worst-severity first, as a self-contained task list a coding agent could
run directly from — no PR context needed, just this block pasted into a
prompt. One entry per finding: `path:line — <tag>: <what>. Fix: <the
snippet if you have one, else the prose fix>.` Wrap the whole list in a
single fenced block so it copies in one motion. Skip this section entirely
when nothing survived verification — an empty task list helps no one.

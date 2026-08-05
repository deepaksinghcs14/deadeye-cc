package main

import (
	"fmt"
	"io"
	"os"

	"github.com/deepaksinghcs14/deadeye-cc/internal/codemap"
)

// notesBodyCap bounds what one invocation will read from stdin -- the note
// is a cache entry, not a transcript dump.
const notesBodyCap = 4 << 10

// runNotesAppend backs `deadeye notes-append <kind>` with the note body on
// stdin: the Layer-B2 capture path for the explore skill. The path
// derivation happens HERE, in real Go (gitutil.ProjectKey via
// codemap.NotesPath), never replicated in a skill prompt -- a prompt's
// re-implementation that drifted from the real logic would orphan every
// write into a file nothing reads or prunes, with no visible failure.
// Best-effort by contract: any failure is a quiet exit 1 the calling
// skill is told to ignore.
func runNotesAppend(kind string) {
	if kind == "" {
		fmt.Fprintln(os.Stderr, "usage: deadeye notes-append <kind>  (note body on stdin)")
		os.Exit(2)
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(1)
	}
	body, err := io.ReadAll(io.LimitReader(os.Stdin, notesBodyCap))
	if err != nil {
		os.Exit(1)
	}
	if err := codemap.AppendNote(cwd, kind, string(body)); err != nil {
		os.Exit(1)
	}
}

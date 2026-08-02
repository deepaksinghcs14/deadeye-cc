// Command versionprobe exists only for meta_test.go's
// TestVersionIsLdflagsOverridable -- it builds this with the same -ldflags
// goreleaser uses and checks the override actually printed.
package main

import (
	"fmt"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

func main() {
	fmt.Println(meta.Version)
}

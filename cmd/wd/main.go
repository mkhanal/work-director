// Command wd is the director's CLI: a thin adapter over the core and the
// ledger. It parses the command line, calls the library and prints the
// result — human text, or the --json rail the whole tool is scripted against.
package main

import (
	"fmt"
	"os"

	"wd/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

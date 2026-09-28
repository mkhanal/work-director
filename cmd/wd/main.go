// Command wd is the director's CLI. It is a thin adapter over the core and the
// ledger: every command parses its input, calls the library and prints the
// result. Commands are ported from the TypeScript CLI one by one; until then
// only --help exists.
package main

import (
	"fmt"
	"os"
)

const usage = `usage: wd <command> [flags]

commands:
  --help            show this help

the work director: roadmap, taste and work status for many repos.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "wd: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("wd: no command; wd --help for usage")
	}
	switch args[0] {
	case "--help", "-h":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q; wd --help for usage", args[0])
	}
}

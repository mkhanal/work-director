package cli

import (
	"fmt"
	"slices"
	"strings"
)

// completion is registered here rather than in the commands table, which
// it reads: a table entry that reads the table is an initialization cycle.
func init() { commands["completion"] = (*Cli).completion }

// completion prints a script that completes wd's command names in a shell.
func (c *Cli) completion(rest []string) error {
	if len(rest) != 1 {
		return fail("usage: wd completion <bash|zsh|fish|powershell>")
	}
	names := []string{"help", "man", "version"}
	for name := range commands {
		names = append(names, name)
	}
	slices.Sort(names)
	words := strings.Join(names, " ")
	var script string
	switch rest[0] {
	case "bash":
		script = fmt.Sprintf(`_wd() {
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W %q -- "${COMP_WORDS[1]}"))
  fi
}
complete -F _wd -o default wd
`, words)
	case "zsh":
		script = fmt.Sprintf(`#compdef wd
_wd() {
  if (( CURRENT == 2 )); then
    compadd -- %s
  else
    _files
  fi
}
compdef _wd wd
`, words)
	case "fish":
		var b strings.Builder
		for _, n := range names {
			fmt.Fprintf(&b, "complete -c wd -n __fish_use_subcommand -a %s\n", n)
		}
		script = b.String()
	case "powershell":
		script = fmt.Sprintf(`Register-ArgumentCompleter -Native -CommandName wd -ScriptBlock {
  param($word, $ast, $cursor)
  if ($ast.CommandElements.Count -le 2) {
    '%s'.Split(' ') | Where-Object { $_ -like "$word*" } |
      ForEach-Object { [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) }
  }
}
`, words)
	default:
		return fail("wd completion knows bash, zsh, fish or powershell, not %q", rest[0])
	}
	if c.JSON {
		return c.out(map[string]any{"shell": rest[0], "script": script}, script)
	}
	_, err := fmt.Fprint(c.Stdout, script)
	return err
}

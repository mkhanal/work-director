package cli

import (
	"fmt"
	"slices"
	"strings"
)

// commandHelp is what one command is for and how it is called.
type commandHelp struct {
	usage   string
	summary string
}

// help is every command's help. A command without an entry fails the
// lazyspec test that walks the command table.
var help = map[string]commandHelp{
	"projects":   {"wd projects (list | add <name> <path> [--runner r] [--mode ask|auto] [--verify cmd] | policy <name> | archive <name> [\"<why>\"] | unarchive <name>)", "Register, list and archive the repositories the director works in."},
	"workspace":  {"wd workspace (add <path> [--name label] [--creates] | create <parent> <name> | list | show <id-or-path>)", "Register parent directories new projects may be created under."},
	"models":     {"wd models <runner>", "List the models a runner offers, from the provider's own CLI."},
	"model":      {"wd model (for <role> | set <role> [--prefer m]… [--runners r]… [--model m] | reset <role>)", "Show or set which model each judgement role uses."},
	"runner":     {"wd runner (list | add <name> <file.toml> | init <name>)", "List runners, or define a new one as a file of commands."},
	"add":        {"wd add <project> <title> [--detail text] [--kind task|goal|…] [--goal <id>] [--heading h]", "File a work item."},
	"tasks":      {"wd tasks <goal> [--all]", "List a goal's tasks."},
	"taste":      {"wd taste", "Show the rule cards every brief carries, and what feedback has gathered toward new ones."},
	"brief":      {"wd brief <id>", "Print the brief an executor would be given for work."},
	"spawn":      {"wd spawn <id> [--runner r] [--model m] [--worktree]", "Start an executor session on work, with its brief."},
	"goal":       {"wd goal (start <project> \"<text>\" [--detach] | begin <id> | find <project> \"<text>\" | supervise <id> | add <project> <title> | plan <id> | spawn <id> | run <id> | review <id> | status <id> | continue <id> \"<text>\" | reopen | release | abandon | classify)", "Start, plan, run and look after goals: work broken into tasks."},
	"epic":       {"wd epic (plan | spawn | run | review | status) <id>", "The older spelling of wd goal."},
	"roadmap":    {"wd roadmap (add <project> <title> | item <roadmap> <title> | plan <roadmap> | show <id> | status)", "Keep a project's roadmap and turn its items into goals."},
	"drive":      {"wd drive <goal> [--budget …]", "Run a goal's loop until it lands or needs a person."},
	"send":       {"wd send <id> \"<text>\"", "Tell running work something; a goal keeps it as a decision and its supervisor routes it."},
	"answer":     {"wd answer <id> <answer>...", "Answer the question a session is waiting on, one answer per item."},
	"attach":     {"wd attach <id> <session> [--runner r] [--ref ref] [--cwd dir]", "Bind a conversation started elsewhere to work."},
	"report":     {"wd report <id> [\"<text>\"]", "File work's STATUS report, read from its session or given."},
	"verify":     {"wd verify <id>", "Run the project's checks in the work's own directory."},
	"decide":     {"wd decide <id> \"<decision>\"", "Record a decision every later brief reads."},
	"pr":         {"wd pr <id> [<url>]", "Record where work landed: a pull request link, or the pushed commit."},
	"soft-done":  {"wd soft-done <id>", "Pass the readiness gate: a done report, a passing verify, a landing."},
	"set":        {"wd set <id> <state>", "Move work to a state the ledger allows."},
	"done":       {"wd done <id> [--cancelled <why>]", "Close work that passed its gates."},
	"status":     {"wd status [--all | --archived]", "List work and where it stands."},
	"context":    {"wd context [<project> | <id>]", "Show the directory, branch and worktree work runs in."},
	"open":       {"wd open [<id>] <path>[:<line>]", "Open a file, or a work's directory, in your editor."},
	"claim":      {"wd claim <id> [<who>] [--drop]", "Claim work for a session, so others see who has it."},
	"impact":     {"wd impact <id> <+path|-path> [--clear]", "Record the paths work touches."},
	"conflict":   {"wd conflict <goal>", "Show tasks under a goal whose claimed paths overlap."},
	"worktree":   {"wd worktree (attach <id> <path> | list <id> | remove <id>)", "Manage the worktrees work runs in."},
	"merge":      {"wd merge <id>", "Fold a task's worktree back into its goal's branch."},
	"concern":    {"wd concern (add <id> \"<text>\" | resolve <n> \"<decision>\" | list [<id>])", "Raise and settle concerns: the director's decision queue."},
	"scan":       {"wd scan [--adopt <card>]", "List project rules that could become global ones."},
	"events":     {"wd events <id>", "List everything recorded on work."},
	"review":     {"wd review --project <p> [--ack] | review reverse <event> \"<why>\"", "See what the loop decided since you last looked."},
	"abandon":    {"wd abandon <id> [no-pr|unmerged] [--reason \"<detail>\"]", "Record that work stopped without shipping."},
	"release":    {"wd release <id> [\"<what was true instead>\"]", "Correct an abandoned row to a choice: dropped."},
	"reopen":     {"wd reopen <id> \"<what is being worked on>\"", "Work on finished work again."},
	"feedback":   {"wd feedback (list | add \"<text>\" [--card c])", "Record a correction or a confirmed approach."},
	"distill":    {"wd distill", "Turn recorded feedback into candidate rule cards."},
	"archive":    {"wd archive <id> [\"<why>\"]", "Put work out of sight, dropping it first if it is open."},
	"unarchive":  {"wd unarchive <id>", "Bring archived work back into sight."},
	"stop":       {"wd stop <id>", "Stop what work's sessions are doing and take back what they had not read."},
	"pause":      {"wd pause <id>", "Stop work's sessions and hold it paused."},
	"resume":     {"wd resume <id> [\"<note>\"]", "Carry on paused or cancelled work where it left off."},
	"cancel":     {"wd cancel <id> [\"<why>\"]", "Stop and drop work; wd resume brings it back."},
	"tui":        {"wd tui", "A terminal board of everything in flight."},
	"serve":      {"wd serve [--port n | --stdio]", "Serve the ledger as JSON, over loopback HTTP or standard streams."},
	"doctor":     {"wd doctor", "Show which runner CLIs are on PATH and whether this is a repository."},
	"completion": {"wd completion <bash|zsh|fish|powershell>", "Print a shell completion script."},
}

// overview lists every command with what it does.
func overview() string {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	slices.Sort(names)
	var b strings.Builder
	b.WriteString("wd — a head-of-engineering agent for many repos\n\nCommands:\n")
	for _, n := range names {
		fmt.Fprintf(&b, "  %-11s %s\n", n, help[n].summary)
	}
	b.WriteString("\nwd <command> --help   how one command is used\nwd --version          which release this is\nwd man                this help as a man page\nAdd --json to any command for one JSON document on stdout.\n")
	return b.String()
}

// helpFor is one command's help, or the overview when name is empty.
func helpFor(name string) (string, error) {
	if name == "" {
		return overview(), nil
	}
	h, ok := help[name]
	if !ok {
		return "", fail("wd has no command %q: wd help lists them", name)
	}
	return fmt.Sprintf("%s\n\n%s\n", h.usage, h.summary), nil
}

// manPage is wd(1) in roff, from the same help.
func manPage() string {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	slices.Sort(names)
	var b strings.Builder
	b.WriteString(".TH WD 1\n.SH NAME\nwd \\- a head-of-engineering agent for many repos\n.SH SYNOPSIS\n.B wd\n\\fIcommand\\fR [\\fIargs\\fR] [\\-\\-json]\n")
	b.WriteString(".SH DESCRIPTION\nwd keeps the roadmap, taste and work status for many repositories, hands whole tasks to executor sessions (Claude Code, opencode, codex), verifies their work and gates where it lands. Every command reads and writes one ledger in ~/.work-director (WD_HOME).\n.SH COMMANDS\n")
	for _, n := range names {
		h := help[n]
		fmt.Fprintf(&b, ".TP\n.B %s\n%s\n.br\n%s\n", n, roff(h.usage), roff(h.summary))
	}
	b.WriteString(".SH FILES\n.TP\n~/.work-director\nThe ledger, projects, runners and logs.\n.SH SEE ALSO\nThe quickstart: docs/quickstart.md in the work-director repository.\n")
	return b.String()
}

func roff(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "-", `\-`)
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "'") {
		s = `\&` + s
	}
	return s
}

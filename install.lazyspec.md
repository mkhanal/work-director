# Install (Go)

The install package probes and installs the director's runtime dependencies:
the external commands the static wd binary shells out to. The binary itself
has no runtime dependencies — it is built with CGO disabled — so probing
these is the whole of what setup means.

## Setup Probes The Runtime Dependencies
`Probe` reports git and the four runner CLIs (claude, opencode, codex, ao):
each is present with its resolved path, or missing with the command that
installs it.

## Install Runs Each Missing Dependency's Command
`Install` runs the install command for every missing dependency and
re-probes: a dependency with no known command, or whose command failed,
stays missing.

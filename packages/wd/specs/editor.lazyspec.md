# The host editor

## An Open Target Resolves Against The Cwd
`parseTarget` turns `<path>[:<line>]` into an absolute path and optional line, resolving a relative
path against the caller's working directory.

## The Host Editor Is Detected From What Is Installed
`detectEditor` prefers the VS Code CLI when `code` is on PATH (reusing the open window and jumping
to the target line), then `$VISUAL`/`$EDITOR` as a plain open command, and reports no editor when
one host has none — never fabricating one.

## The File Link Is A Clickable OsC8 Url
`openLink` renders an iTerm2-style clickable file hyperlink (OSC-8) carrying the file's absolute
path and line, so a path printed by wd opens on click even without an editor spawn.
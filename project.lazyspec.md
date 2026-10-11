> **lazyspec.** Humans edit freely. Agents change this only through
> `/lazyspec`, with its tests, in one edit.
>
> Each `##` heading is one requirement. Its test repeats that heading
> as its own name — to find it, search the tests for that text.

# Project (Go)

The project package owns the project file's fields and defaults, the template
`wd projects add` writes, the runner-list flag, and loading every project in
the director's home.

## Parse Project Reads The Frontmatter Fields
`ParseProject` takes the name from the file's base, expands a leading `~`
in the path against `$HOME`, defaults mode to auto, instructions file to
AGENTS.md and default branch to main, and parses the stack, workflows and
verify lists.

## A Tilde Path Fails When The Home Directory Is Unknown
A path starting `~` or `~/` fails with a ProjectError naming the file when
the home directory cannot be resolved.

## A Missing Field Fails Naming The File And Field
A project file without frontmatter, or without the runner or path field,
fails with a ProjectError naming the file and what is missing.

## An Unknown Mode Fails
A mode other than ask or auto fails with a ProjectError naming the mode.

## The Project Template Writes Concrete Frontmatter
`ProjectTemplate` writes the file `wd projects add` creates: every field
filled, no lazyspec claim.

## An Empty Runner List Falls Back To The Project Runner
`ParseRunnerList` with an empty or blank list returns the fallback
project's runner.

## An Unknown Runner Fails Loudly
`ParseRunnerList` with a name the director does not know fails with a
ProjectError naming it and listing the known runners.

## Install Lazyspec Creates The Evolution Work Item
`InstallLazyspec` returns the title and detail of the work item that
installs the lazyspec convention in a project.

## Load Projects Skips The Readme
`LoadProjects` reads every `*.md` in the director's home except README.md,
keyed by project name.

## A Repository's Toolkit Gives Its Checks
- `ReadToolkit(dir)` takes the package manager from the lockfile: `pnpm-lock.yaml` pnpm, `yarn.lock` yarn, `bun.lock` or `bun.lockb` bun, else npm where there is a `package.json`.
- Its checks are `<manager> run <script>` for each of `lint`, `typecheck`, `check`, `validate` and `test` that `package.json` defines, in that order.
- `go.mod` adds `go vet ./...` and `go test ./...`; `Cargo.toml` adds `cargo test`.
- A repository with none of these has no checks.

## A Repository's Toolkit Names The Specifications It Keeps
- `*.lazyspec.md` files make it `lazyspec`; `SPEC.md` files make it `SPEC.md`; a repository can keep both.
- `node_modules`, `.git`, `vendor`, `dist` and `build` are not searched.
- A repository with neither keeps none.

## A Project's Checks Are Its Verify Commands, Else Its Toolkit's
- `Checks(p, kit)` returns the project's verify commands when it lists any, else the toolkit's checks.
- A listed command run with npm, pnpm, yarn or bun is refused naming both when the repository's lockfile names another package manager, or when it has no `package.json`.
- Without a lockfile, any package manager's command is run as listed.


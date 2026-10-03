# wt

`wt` manages **multi-repository workspaces** built from git worktrees.

Each repository is cloned once, as a bare repository. A workspace is a
folder (usually named after a ticket) with one worktree per repository, all
on the same branch. Creating a workspace never clones: it only checks origin
for the branches involved, so it takes about as long as checking out the
files.

```
~/.repos/
    domino.git/              # bare clone: the single source of truth
    domino/                  # primary checkout, detached at origin/<default>
~/workspaces/
    DOM-12345/
        CLAUDE.md            # generated instructions for AI agents (optional)
        domino/              # git worktree on branch DOM-12345
        compute-workload-service/
```

## Install

Requires Go 1.27+ and git 2.38+.

```bash
git clone <this-repo> && cd wt
make install                 # builds and installs to ~/.local/bin/wt
```

Use `make install PREFIX=/other/bin` to install somewhere else, and
`make uninstall` to remove it.

Then enable `wt cd` and tab completion in your shell profile:

```bash
eval "$(wt shell-init zsh)"     # ~/.zshrc, after compinit
eval "$(wt shell-init bash)"    # ~/.bashrc
wt shell-init fish | source     # ~/.config/fish/config.fish
```

Run `wt doctor` to check the setup.

## Quick start

```bash
# 1. Add repositories to the index (once per repository)
wt clone git@github.com:cerebrotech/domino.git
wt clone git@github.com:cerebrotech/compute-workload-service.git

# 2. Optional: integrations
wt integrations codegraph          # index each workspace with CodeGraph
wt integrations harness claude     # generate CLAUDE.md (enabled by default)

# 3. Optional: a bundle of repositories you often use together
wt bundle backend --repos domino,compute-workload-service

# 4. Create a workspace and jump into it
wt ws DOM-80506 --bundles backend
wt cd DOM-80506
claude                             # or: wt open DOM-80506

# 5. When the work is merged
wt cleanup
```

## Commands

| Command | What it does |
|---|---|
| `wt clone <url> [--name alias]` | Add a repository: bare clone plus primary checkout |
| `wt workspace <name> [--repos ...] [--bundles ...]` | Create a workspace or add repositories to it (alias: `ws`); without flags, pick interactively |
| `wt ws list` | List workspaces as `repo@branch` (`*` = uncommitted changes) |
| `wt ws remove <name> [--repos ...]` | Remove a workspace, or only some of its repositories |
| `wt bundle <name> [--repos ...]` | Create or update a bundle; without `--repos`, pick interactively |
| `wt bundle list` / `wt bundle remove <name>` | List or delete bundles |
| `wt cd [target]` | `cd` into a workspace, `<workspace>/<repo>`, or a primary checkout |
| `wt open [target]` | Open the same targets in your editor |
| `wt sync [repo...]` | Fetch every repository in parallel and update the primary checkouts |
| `wt cleanup` | Classify workspaces and remove the merged ones you select |
| `wt integrations [codegraph \| harness <name>]` | List integrations, or enable one (`--disable` turns it off) |
| `wt doctor [--fix]` | Health checks, with repairs for common problems |
| `wt shell-init <zsh\|bash\|fish>` / `wt completion <shell>` | Shell integration and completion |

`wt ws <name>` and `wt bundle <name>` without `--repos`/`--bundles` open an
interactive picker, as do `wt cd` and `wt open` without an argument. When the
list mixes kinds (bundles and repositories, or workspaces and repositories),
press `ctrl+t` to cycle between showing all of them or only one kind;
selections are kept while switching. In the workspace and bundle pickers,
`ctrl+b` on a repository opens a dropdown of its branches (type to search).
The first entry is the default (the workspace branch), then the default
branch, then the rest, most recent first. The chosen branch is shown next to
the repository (`domino @ feature/x`). `--repos` and `--bundles` always take a
value: comma-separated, or repeat the flag (`--repos a --repos b@main`).

### Workspaces and branches

`--repos` takes `repo` or `repo@branch`, comma-separated:

```bash
wt ws DOM-12345                                   # pick bundles and repositories
wt ws DOM-12345 --repos domino                    # branch DOM-12345
wt ws DOM-12345 --repos domino@main,cws@dev,web   # explicit branches
wt ws DOM-12345 --repos web --from release-2.4    # new branches start from release-2.4
wt ws DOM-12345 --repos domino --no-fetch         # offline: use the refs from the last sync
```

A repository without `@branch` uses the workspace name as its branch. `wt`
uses the first of these that applies:

1. An existing local branch.
2. `origin/<branch>`, checked out as a tracking branch.
3. A new branch from `origin/<default>` (or `--from`). It has no upstream
   until the first `git push`, which sets one up automatically
   (`push.autoSetupRemote`).

Notes:

- **Checks before changes.** Every repository is checked before anything is
  created. If any repository has a problem, nothing is created and all
  problems are reported together. If a worktree fails mid-way, everything
  created by that run is rolled back.
- **Re-running adds repositories.** Running `wt ws` again on an existing
  workspace adds the new repositories and leaves the existing ones alone.
- **Latest branches from origin.** Before resolving branches, `wt` asks
  origin for the branches involved (the workspace branch, the default branch
  and `--from`): one quick request per repository, in parallel, fetching
  only what changed. A branch a teammate pushed after your last sync is
  found and tracked. An existing local branch that is behind
  `origin/<branch>` is fast-forwarded. One that has diverged is left as is,
  with a note to `git pull`. If origin can't be reached, `wt` warns and uses
  the local refs; `--no-fetch` skips the check.
- **One worktree per branch.** Git allows a branch to be checked out in only
  one worktree, so two workspaces can't use the same branch of the same
  repository.
- **No metadata file.** A workspace's repositories are read from the
  worktrees it contains.

### Bundles

When a workspace combines bundles and `--repos`:

- **Repositories:** it gets every repository from every bundle and from
  `--repos`.
- **Branch from `--repos`:** an explicit `--repos repo@branch` overrides the
  bundles.
- **Bundles that disagree:** if two bundles pin different branches for the
  same repository, the first bundle listed wins.

`wt` prints a note every time it resolves one of these conflicts.

### Safety

- Nothing is forced. A worktree with uncommitted or untracked changes is
  never removed.
- A local branch is deleted only when its commits are safe elsewhere: merged
  into `origin/<default>` (regular, rebase or squash merge) or pushed to
  `origin/<branch>`.
- Remote branches are deleted only by `wt ws remove`, and only after you
  confirm (or pass `--delete-remote`). `wt cleanup` never deletes them.
- `wt sync` moves a primary checkout only when it is clean, detached, and
  contains no commits of its own.

### Cleanup

`wt cleanup` fetches the repositories in use and puts each workspace in one
of these groups:

- merged (safe to remove)
- pushed, not merged yet
- contains unpushed commits
- uncommitted changes
- no commits yet
- empty

Merged workspaces are preselected in a checklist. Options:

- `--dry-run`: only show the groups.
- `--yes`: remove the merged workspaces without asking.
- `--no-fetch`: skip the fetch.

## Configuration

`~/.wt/settings.toml` (set `WT_HOME` to use another directory). Every key is
optional:

```toml
[paths]
repos = "~/.repos"
workspaces = "~/workspaces"

[editor]
command = "code"            # default: $EDITOR, then code. Aliases don't work; e.g. "open -a 'Visual Studio Code'"

[integrations.codegraph]
enabled = false

[integrations.harness.claude]
enabled = true
template = "claude"         # ~/.wt/templates/claude.md overrides the built-in template

[integrations.harness.generic]
enabled = false
template = "agents"         # writes AGENTS.md

[bundles.backend]
repos = ["domino", "compute-workload-service@main"]
```

`wt bundle` and `wt integrations` rewrite this file, which drops any
comments in it.

## Integrations

Integrations run after a workspace is created or its repositories change.
If one fails, `wt` prints a warning and the workspace is still ready.

- **codegraph** indexes the whole workspace as one project. The first run
  uses `codegraph init`; later runs use `codegraph sync`. It needs
  [CodeGraph](https://github.com/colbymchenry/codegraph) on `PATH`; `wt doctor`
  shows whether it is installed and how to set it up:

  ```bash
  npm install -g @colbymchenry/codegraph
  codegraph telemetry off          # optional
  wt integrations codegraph
  ```
- **harness** writes an instruction file for AI agents at the workspace
  root:
  - `claude` writes `CLAUDE.md`; `generic` writes `AGENTS.md`.
  - Custom harnesses need a file name:
    `wt integrations harness cursor --file RULES.md --template agents`.
  - Templates are Go `text/template` files that can use `.Name`, `.Path`,
    `.Repos` (each with `.Name`, `.Branch` and `.Path`) and `.Codegraph`.

Generated files start with a marker line. Delete that line to keep your
edits: `wt` then never overwrites or deletes the file. Removing a workspace
deletes the generated files and `.codegraph/`.

## Development

```bash
make test     # unit and integration tests (real git in temp dirs)
make vet
make build    # bin/wt
```

Code layout:

- `internal/git`: thin git CLI wrapper.
- `internal/repo`: repository index, clone and sync.
- `internal/workspace`: plan/apply, remove and status.
- `internal/integration`: the integrations.
- `internal/doctor`: health checks.
- `internal/cli`: cobra commands.
- `internal/ui`: prompts built on charmbracelet/huh.

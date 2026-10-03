# wt

`wt` manages **multi-repository workspaces** built from git worktrees.

Each repository is cloned once, as a bare repository. A workspace is a
folder (usually named after a ticket) with one worktree per repository, all
on the same branch. Creating a workspace never clones: it only checks origin
for the branches involved, so it takes about as long as checking out the
files.

```
~/.repos/
    billing.git/   # bare clone: the single source of truth
    billing/       # primary checkout, detached at origin/<default>
~/workspaces/
    PROJ-123/
        CLAUDE.md  # generated instructions for AI agents (optional)
        billing/   # git worktree on branch PROJ-123
        worker/
```

The primary checkout is for reading the latest code: `wt open <repo>` moves
it to the latest default branch before opening it. It is detached (shell
prompts show a tag or commit hash instead of a branch name) so that the
default branch itself stays free for `repo@main` in workspaces. Do your work
in workspaces.

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

Run `wt check` to verify the setup.

## Quick start

```bash
# 1. Add repositories to the index (once per repository)
wt clone git@github.com:acme/billing.git
wt clone git@github.com:acme/worker.git

# 2. Optional: integrations
wt integrations codegraph          # index each workspace with CodeGraph
wt integrations harness claude     # generate CLAUDE.md (enabled by default)

# 3. Optional: a bundle of repositories you often use together
wt bundle backend --repos billing,worker

# 4. Create a workspace and jump into it
wt ws PROJ-456 --bundles backend
wt cd PROJ-456
claude                             # or: wt open PROJ-456

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
| `wt open [target]` | Open the same targets in your editor; a primary checkout is first updated to the latest default branch (`--no-fetch` skips it) |
| `wt sync [repo...]` | Fetch every repository in parallel and update the primary checkouts |
| `wt cleanup` | Classify workspaces and remove the merged ones you select |
| `wt integrations [codegraph \| harness <name>]` | List integrations, or enable one (`--disable` turns it off) |
| `wt check [--fix]` | Health checks, with repairs for common problems |
| `wt shell-init <zsh\|bash\|fish>` / `wt completion <shell>` | Shell integration and completion |

`wt ws <name>` and `wt bundle <name>` without `--repos`/`--bundles` open an
interactive picker, as do `wt cd` and `wt open` without an argument. When the
list mixes kinds (bundles and repositories, or workspaces and repositories),
press `ctrl+t` to cycle between showing all of them or only one kind;
selections are kept while switching. In the workspace and bundle pickers,
`ctrl+b` on a repository opens a dropdown of its branches (type to search).
The first entry is the default (the workspace branch). Then come the default
branch and the rest, most recent first. After picking a branch, choose
between creating the workspace branch from it and working directly on it.
The result is shown next to the repository in the same syntax as `--repos`
(`billing:release-2.4`, `billing@feature/x`). `--repos` and `--bundles` always
take a value: comma-separated (no spaces, or quote the list), or repeat the
flag (`--repos a --repos b:develop`).

### Workspaces and branches

`<default>` is each repository's default branch (`main`, `master`, `develop`,
...), taken from origin's `HEAD` when the repository is cloned and refreshed
by `wt sync`. Repositories in the same workspace can have different default
branches.

Each repository in `--repos` takes one of three forms:

| Form | Branch in the worktree | Typical use |
|---|---|---|
| `repo` | the workspace branch `<name>`, new from `origin/<default>` if it doesn't exist | a ticket off the default branch |
| `repo:base` | the workspace branch `<name>`, new from `base` if it doesn't exist | a hotfix or PR into `base`, or stacking on another branch |
| `repo@branch` | `branch` itself | reviewing or continuing an existing branch |

```bash
wt ws PROJ-123                                           # pick bundles and repositories
wt ws PROJ-123 --repos billing,worker                    # PROJ-123 in both
wt ws HOTFIX-77 --repos billing:release-2.4,web:develop  # HOTFIX-77 from a different base per repo
wt ws HOTFIX-78 --repos billing:release-2.4              # another hotfix from the same base
wt ws REVIEW-1  --repos billing@feature/foo              # work on feature/foo itself
wt ws PROJ-123 --repos billing --no-fetch                # offline: use the refs from the last sync
```

With `repo` and `repo:base`, the workspace branch is found or created in
this order:

1. An existing local branch.
2. `origin/<name>`, checked out as a tracking branch.
3. A new branch from `base` (or `origin/<default>` without one). For a base,
   `origin/<base>` is used unless the local branch has commits of its own
   (stacking on unpushed work). The new branch has no upstream until the
   first `git push`, which sets one up automatically (`push.autoSetupRemote`).

If the branch already exists, the base is ignored and `wt` says so.

Notes:

- **Checks before changes.** Every repository is checked before anything is
  created. If any repository has a problem, nothing is created and all
  problems are reported together. If a worktree fails mid-way, everything
  created by that run is rolled back.
- **Re-running adds repositories.** Running `wt ws` again on an existing
  workspace adds the new repositories and leaves the existing ones alone.
- **Latest branches from origin.** Before resolving branches, `wt` asks
  origin for the branches involved (the workspace branch, the default branch
  and any base): one quick request per repository, in parallel, fetching
  only what changed. A branch a teammate pushed after your last sync is
  found and tracked. An existing local branch that is behind
  `origin/<branch>` is fast-forwarded. One that has diverged is left as is,
  with a note to `git pull`. If origin can't be reached, `wt` warns and uses
  the local refs; `--no-fetch` skips the check.
- **One worktree per branch.** Git allows a branch to be checked out in only
  one worktree, so two workspaces can't both use `billing@release-2.4`. Use
  `billing:release-2.4` instead: each workspace then gets its own branch.
- **No metadata file.** A workspace's repositories are read from the
  worktrees it contains.

### Bundles

When a workspace combines bundles and `--repos`:

- **Repositories:** it gets every repository from every bundle and from
  `--repos`.
- **Branch from `--repos`:** an explicit `--repos repo@branch` or
  `repo:base` overrides the bundles.
- **Bundles that disagree:** if two bundles pin different branches or bases
  for the same repository, the first bundle listed wins.

Bundles use the same forms, e.g.
`wt bundle release --repos billing:release-2.4,web:release-2.4`.

`wt` prints a note every time it resolves one of these conflicts.

### Safety

- Nothing is forced. A worktree with uncommitted or untracked changes is
  never removed.
- "Merged" means merged into the branch's base (for `repo:base`, recorded in
  the branch's git config as `branch.<name>.wtBase`) or the default branch,
  with regular, rebase or squash merges. Before deleting anything, `wt`
  checks origin, so a stale remote-tracking branch never counts as a copy.
- A local branch is deleted only when its commits are safe elsewhere: merged,
  or pushed to an `origin/<branch>` that is kept.
- Remote branches are deleted only by `wt ws remove`, after you confirm (or
  pass `--delete-remote`), and only when merged: an open pull request, yours
  or a teammate's, is never closed. `wt cleanup` never deletes them.
- A detached worktree whose commits are on no branch is never removed.
- `wt sync` and `wt open` move a primary checkout only when it is clean,
  detached, and contains no commits of its own.

### Cleanup

`wt cleanup` fetches the repositories in use and puts each workspace in one
of these groups, judging each branch against its base (`repo:base`) or the
default branch:

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
repos = ["billing", "worker@main"]
```

`wt bundle` and `wt integrations` rewrite this file, which drops any
comments in it.

## Integrations

Integrations run after a workspace is created or its repositories change.
If one fails, `wt` prints a warning and the workspace is still ready.

- **codegraph** indexes the whole workspace as one project. The first run
  uses `codegraph init`; later runs use `codegraph sync`. It needs
  [CodeGraph](https://github.com/colbymchenry/codegraph) on `PATH`; `wt check`
  shows whether it is installed and how to set it up:

  ```bash
  npm install -g @colbymchenry/codegraph
  codegraph telemetry off          # optional
  wt integrations codegraph
  ```
- **harness** writes an instruction file for AI agents at the workspace
  root:
  - `claude` writes `CLAUDE.md`; `generic` writes `AGENTS.md` (read by
    other coding agents).
  - To customise the content, put a template in `~/.wt/templates/<name>.md`
    and select it with `template = "<name>"` (or `--template <name>`).
    Templates are Go `text/template` files that can use `.Name`, `.Path`,
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
- `internal/check`: health checks.
- `internal/cli`: cobra commands.
- `internal/ui`: prompts built on charmbracelet/huh.

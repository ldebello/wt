# wt

A small Bash script to manage git repos using the **bare repo + worktrees**
pattern: instead of a single checkout where you keep switching branches
(`git checkout`, stash, checkout, stash pop...), each branch lives in its
own folder, all sharing the same underlying `.git`, and jumping between
branches is just a `cd`.

## Installation

```bash
git clone <this-repo>
cd wt
make install
```

This copies `wt` to `~/.local/bin/wt` and makes it executable.
If `~/.local/bin` isn't in your `PATH`, `make install` will warn you and
show the line to add to your `~/.zshrc` / `~/.bashrc`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

To uninstall:

```bash
make uninstall
```

To install somewhere else (not `~/.local/bin`):

```bash
make install PREFIX=/some/other/path
```

## Usage

```
wt clone <git-url>     Clone a repo into a bare-repo + worktrees layout
wt <branch>            Create (or reuse) a worktree for <branch>
wt <branch> --from <base-branch>
                        Create <branch> as a new branch starting from
                        <base-branch> instead of the default branch
wt remove <branch>     Remove a worktree and its local branch
wt cleanup             Remove every worktree except primary, then update
wt update              Fetch all remotes, fast-forward primary, and list worktrees
wt workspace <name> --repos <r1,r2,...> [--repo-base <dir>]
                        Create (or update) a multi-repo workspace
wt workspace remove <name> [--repo-base <dir>]
                        Remove every worktree in a workspace, then the
                        workspace directory itself
wt help                Show this help
```

### Example

```bash
wt clone git@github.com:user/repo.git
cd repo/primary

wt feature/login        # creates repo/feature/login, new branch off the default branch
cd ../feature/login

wt bugfix-123           # another branch, another worktree, side by side
cd ../bugfix-123

wt hotfix --from release-2.4  # new branch off release-2.4 instead of the default branch
cd ../hotfix

wt remove feature/login # removes the worktree and local branch (with a prompt for the remote)

wt cleanup               # removes every safe-to-remove worktree except primary, then updates

wt update               # fetch --all --prune, fast-forward primary, list branches and worktrees
```

### Layout created by `wt clone`

```
repo/
├── .bare/       # the real bare repo (don't touch)
├── .git         # points to .bare
├── primary/     # permanent worktree, checked out on the default branch
└── <branch>/    # one worktree per branch created via `wt <branch>`
```

`primary` is treated as permanent: `wt remove` refuses to touch it.

You can run `wt <branch>` and `wt remove <branch>` from anywhere inside
the repo (the bare root or any worktree) — the script always resolves
the actual repo root.

By default, a brand-new branch is created off the repo's default branch
(`origin/<default>`). Pass `--from <base-branch>` to branch off something
else instead — a release branch, another feature branch, anything that
exists locally or on `origin`:

```bash
wt hotfix --from release-2.4     # release-2.4 exists on origin
wt follow-up --from feature/foo  # feature/foo is only local so far
```

`--from` only matters when `<branch>` doesn't exist yet. If `<branch>`
already has a worktree-able local or remote branch, `wt` reuses it as
usual and prints a warning that `--from` was ignored. If the base branch
itself doesn't exist anywhere, `wt` fails with an error instead of
creating the worktree.

`wt update` also fast-forwards `primary` to its upstream — but only when
`primary` has no local changes and can be fast-forwarded cleanly. If it
has uncommitted changes or has diverged (e.g. you committed directly on
`primary`), `wt update` warns and leaves it untouched instead of risking
your work; update it yourself with `git pull` in that case.

`wt cleanup` sweeps every worktree except `primary` and removes whatever
it safely can, then runs the same update as `wt update`. For each
worktree it tries `git worktree remove` (no `--force`) and `git branch
-d` (no `-D`):

- A worktree with uncommitted changes (or that's locked) is skipped
  entirely and reported — nothing is touched.
- A worktree whose branch is fully merged is removed along with its
  local branch.
- A worktree whose branch isn't fully merged still has its worktree
  removed, but the local branch is kept around (retrievable later with
  `wt <branch>`).

`wt cleanup` never touches remote branches — use `wt remove <branch>`
for that, one branch at a time. It also never touches worktrees that
belong to a workspace (see below) — those are only removed with
`wt workspace remove <name>`.

## Workspaces

A ticket that spans several repos normally means creating a same-named
worktree in each repo separately — which makes it awkward to open all
of them in one editor window (for a combined GitLens view, say) or to
build a single cross-repo view of the code.

`wt workspace` groups them instead: one worktree per repo, all nested
under a single folder, all on the same branch — `<name>` is both the
workspace folder name and the branch name, since in practice they're
the same thing (the ticket).

```bash
wt workspace DOM-1784 --repos api,worker,frontend
```

This creates:

```
~/workspaces/DOM-1784/
├── .wt-workspace        # metadata read back by 'wt workspace remove'
├── api/                 # worktree on branch DOM-1784
├── worker/              # worktree on branch DOM-1784
└── frontend/            # worktree on branch DOM-1784
```

- `--repos` is a comma-separated list of repos that already use the
  `wt` bare-repo + worktrees layout under `--repo-base` (i.e. each one
  has `<repo-base>/<repo>/.bare`).
- `--repo-base` is where the repos themselves live and defaults to
  `~/repos` if omitted. The `workspaces` folder is created as a
  **sibling** of `--repo-base`, not nested inside it — with the default
  `--repo-base`, that's `~/workspaces` (since `~/repos` and
  `~/workspaces` both live directly under `~`).
- `<name>` becomes the branch name in every repo, exactly like the
  `<branch>` argument to plain `wt <branch>` (existing local branch,
  existing remote branch, or a new branch off the default branch, in
  that order). Since it's also a path segment, unlike a plain `<branch>`
  it can't contain `/`.
- Every repo is validated up front. If one is missing or invalid,
  nothing is created; if a worktree can't be created partway through
  (e.g. the branch is already checked out elsewhere in one repo), any
  worktree already created earlier in that same run is rolled back.
- Re-running the same command later (e.g. after adding a repo to
  `--repos`) reuses worktrees that already exist and only creates the
  ones that are missing.

Open `~/workspaces/DOM-1784` as a single folder in your editor to
see and diff all three repos together.

`wt workspace remove <name> [--repo-base <dir>]` removes every worktree in
the workspace (and its local branch, same rules as `wt remove`), then
the workspace directory itself. It asks once whether to also delete the
matching branch on `origin` for every repo, instead of prompting repo
by repo.

### Optional: cross-repo code graph with CodeGraph

If [CodeGraph](https://github.com/colbymchenry/codegraph) is installed
and `WT_CODEGRAPH=true` is set in your shell, `wt workspace` also indexes
the whole workspace as one project: `codegraph init` the first time,
`codegraph index --force` on later re-runs (e.g. after a repo was
added). This gives you a knowledge graph that spans all the repos in
the workspace, not just one at a time.

If `WT_CODEGRAPH` is unset or not `"true"`, `wt workspace` never invokes
`codegraph` at all.

To set it up once:

```bash
# follow the install instructions at https://github.com/colbymchenry/codegraph
codegraph telemetry off

# in your ~/.zshrc / ~/.bashrc
export WT_CODEGRAPH=true
```

After that, every `wt workspace` call keeps the graph up to date on its
own — no extra step needed.

## Why this approach?

The typical single-checkout workflow forces you to choose between:

- Constantly switching branches, losing working-directory state
  (running processes, branch-specific `node_modules`, untracked files)
  every time you hop between tasks.
- Constantly `git stash`-ing so you can switch branches without losing
  changes, with the risk of forgetting a stash or applying the wrong one.
- Cloning the same repo multiple times into separate folders, duplicating
  the whole history and `.git` on disk for every copy.

With bare repo + worktrees:

- **Each branch has its own folder**, with its own working directory.
  You can have several branches active at the same time — each with its
  own running server, its own installed dependencies, its own uncommitted
  files — without them stepping on each other.
- **A single git history on disk** (`.bare`), shared by every worktree.
  No duplicated `.git` per branch like you'd get from cloning the repo
  multiple times.
- **Switching tasks is a `cd`**, not a `checkout`. No stashing, no
  waiting for git to rewrite the working directory, no risk of dragging
  changes from one branch into another by mistake.
- **Less friction for working in parallel**: reviewing a PR, continuing
  your feature, and testing a hotfix can all coexist as three folders at
  the same time.

The only cost is understanding the layout (`.bare`, `primary`, and one
folder per branch) — `wt` exists precisely so you don't have to manage
that layout by hand with raw `git worktree` commands.

## Using with Claude Code

If you keep multiple repos side by side in one parent folder (some using
the `wt` layout, some not) and want Claude Code to manage worktrees for
you automatically, drop a `CLAUDE.md` in that parent folder. Claude Code
reads `CLAUDE.md` files while walking up from the current directory, so
one placed there is picked up no matter which repo or worktree you open
a session in.

```markdown
# Repos in this folder using the `wt` layout

Some repos here use the `wt` bare-repo + worktrees layout, others are
plain clones. `wt` is installed and on PATH.

A repo uses the `wt` layout if `<repo>/.bare` exists. In that case:
- `<repo>/primary` is the permanent worktree on the default branch.
- `<repo>/<branch>` is a worktree created with `wt <branch>`.
- Never edit anything inside `<repo>/.bare` directly.

When asked to work on repo(s) X, Y, Z (optionally with a branch name),
for each one:
1. Check whether `<repo>/.bare` exists.
2. If it does, run `wt <branch>` with cwd anywhere inside `<repo>`
   (the repo root works, no need to `cd` into `primary` first), then do
   the actual work inside `<repo>/<branch>` — not in `primary`.
3. If it doesn't, work directly in `<repo>` with a normal `git checkout`.
4. If no branch name was given, pick one descriptive name and reuse it
   across every affected repo, unless told otherwise.

`wt remove <branch>` deletes a worktree and its local branch — don't run
it on your own initiative, only when explicitly asked.
```

Adjust the repo list / paths to your own setup. This lets you say
"we need to touch repo A and repo B for this feature" and have Claude
create or reuse the right worktree in each one before it starts editing.

## Credits

The bare-repo + worktrees layout this script automates is based on the
approach described in
[Git Worktree, like a boss](https://dev.to/metal3d/git-worktree-like-a-boss-2j1b).

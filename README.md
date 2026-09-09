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
wt remove <branch>     Remove a worktree and its local branch
wt update               Fetch all remotes, prune stale worktrees, and list them
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

wt remove feature/login # removes the worktree and local branch (with a prompt for the remote)

wt update                # fetch --all --prune, prune stale worktrees, list branches and worktrees
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

## Credits

The bare-repo + worktrees layout this script automates is based on the
approach described in
[Git Worktree, like a boss](https://dev.to/metal3d/git-worktree-like-a-boss-2j1b).

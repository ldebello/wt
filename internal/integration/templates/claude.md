# Workspace {{.Name}}

This directory is a **multi-repository workspace** managed by `wt`. It is not
a git repository itself: each folder below is a separate git worktree of a
different repository.

| Folder | Branch |
|--------|--------|
{{- range .Repos}}
| `{{.Name}}/` | `{{if .Branch}}{{.Branch}}{{else}}(detached){{end}}` |
{{- end}}

## Rules

- Run every git command inside the repository folder it applies to
  (`git -C <folder> status`, or `cd` into it first). Never run `git init` or
  git commands at the workspace root.
- Edit files only inside a repository folder. Commit, push and open pull
  requests per repository, on the branch listed above.
- Do not switch branches inside these worktrees: a branch can be checked out
  in only one worktree, so `git switch`/`git checkout` can fail or affect
  other workspaces. Ask before changing branches.
- Changes often span repositories (API contracts, shared libraries, generated
  clients). Before changing an interface, find its users in the other
  folders and update them together.
{{- if .Codegraph}}
- A CodeGraph index covers the whole workspace (`.codegraph/` at the root).
  Use it (`codegraph_explore`, or `codegraph explore "<symbols or question>"`)
  to find cross-repository callers and dependencies before making changes,
  rather than grepping each repository separately.
{{- end}}
- Do not edit or delete this file, `.codegraph/`, or anything outside the
  repository folders: `wt` manages them.

# Workspace {{.Name}}

This directory is a multi-repository workspace. It is not a git repository:
each folder is a separate git worktree.

Repositories:
{{- range .Repos}}
- `{{.Name}}/` on branch `{{if .Branch}}{{.Branch}}{{else}}(detached){{end}}`
{{- end}}

Guidelines for coding agents:

- Run git commands and edit files only inside the repository folder they
  belong to. Never run git at the workspace root.
- Keep each repository on the branch listed above; do not switch branches.
- Changes may span repositories: check the other folders for callers and
  dependencies before changing shared interfaces, and update them together.
{{- if .Codegraph}}
- Use the CodeGraph index at the workspace root (`codegraph explore "<query>"`)
  to trace cross-repository dependencies before making changes.
{{- end}}
- Do not modify files outside the repository folders; they are managed by wt.

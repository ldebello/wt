package repo

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/ldebello/wt/internal/git"
)

// maxParallel bounds concurrent network operations.
const maxParallel = 8

// Fetch runs `git fetch --prune origin` for the named repositories in
// parallel. The result maps each repository to its error (nil on success).
func (ix Index) Fetch(ctx context.Context, names []string) map[string]error {
	return ForEach(names, func(name string) error {
		_, err := git.Run(ctx, ix.BarePath(name), "fetch", "-q", "--prune", "origin")
		return err
	})
}

// FetchBranches refreshes refs/remotes/origin/<branch> for the given branches
// of repository name. It asks origin which of them exist (one ls-remote) and
// only fetches the ones that changed, so it is much cheaper than a full
// fetch. Branches missing on origin are ignored.
func (ix Index) FetchBranches(ctx context.Context, name string, branches []string) error {
	bare := ix.BarePath(name)
	args := []string{"ls-remote", "--heads", "origin"}
	for _, b := range branches {
		if b != "" {
			args = append(args, "refs/heads/"+b)
		}
	}
	out, err := git.Run(ctx, bare, args...)
	if err != nil {
		return err
	}
	var refspecs []string
	for _, line := range strings.Split(out, "\n") {
		sha, ref, ok := strings.Cut(line, "\t")
		branch, isHead := strings.CutPrefix(ref, "refs/heads/")
		// ls-remote patterns match ref suffixes; keep exact names only.
		if !ok || !isHead || !slices.Contains(branches, branch) {
			continue
		}
		if current, _ := git.Run(ctx, bare, "rev-parse", "-q", "--verify", "refs/remotes/origin/"+branch); current == sha {
			continue
		}
		refspecs = append(refspecs, "+refs/heads/"+branch+":refs/remotes/origin/"+branch)
	}
	if len(refspecs) == 0 {
		return nil
	}
	_, err = git.Run(ctx, bare, append([]string{"fetch", "-q", "--no-tags", "origin"}, refspecs...)...)
	return err
}

// ForEach runs fn for every name with bounded parallelism and collects the
// errors by name.
func ForEach(names []string, fn func(name string) error) map[string]error {
	results := make(map[string]error, len(names))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallel)
	for _, name := range names {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			err := fn(name)
			mu.Lock()
			results[name] = err
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

package repo

import (
	"context"
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

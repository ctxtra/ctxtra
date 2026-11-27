//go:build !unix

package detach

// Start starts nothing: ctxtra ships for unix systems only.
func Start(string, []string) error { return nil }

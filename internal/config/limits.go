package config

const (
	// MaxFileBytes limits every file a session reads: 1,000,000 characters,
	// which an agent can get in full, are up to 4 MB of UTF-8.
	MaxFileBytes = 4 << 20
	// MaxSessionBytes limits what one session reads in total: the config,
	// the lock, manifests, instruction files and SKILL.md files.
	MaxSessionBytes = 16 << 20
)

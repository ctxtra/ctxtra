// Package paths resolves user paths (~, relative paths, symlinks) and knows
// where ctxtra keeps its own files.
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// Home is the user's home directory, or / when it is unknown.
func Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}
	return h
}

// Expand replaces a leading ~ with the home directory.
func Expand(p string) string {
	if p == "~" {
		return Home()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(Home(), p[2:])
	}
	return p
}

// Resolve expands ~ and makes a relative p relative to base.
func Resolve(base, p string) string {
	p = Expand(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

// Real is the absolute path with symlinks resolved, or the absolute path when
// it does not exist.
func Real(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// Tilde shows a path under the home directory as ~/...
func Tilde(p string) string {
	h := Home()
	if p == h {
		return "~"
	}
	if strings.HasPrefix(p, h+string(filepath.Separator)) {
		return "~/" + filepath.ToSlash(p[len(h)+1:])
	}
	return p
}

// TildeReal is Tilde for messages about a path with symlinks resolved, also
// when the home directory is reached through a symlink. Unlike Tilde's, its
// result doesn't always expand to the same text, so it must not be saved.
func TildeReal(p string) string {
	home := Home()
	if real := Real(home); real != home {
		if rel, ok := Rel(real, p); ok {
			p = filepath.Join(home, rel)
		}
	}
	return Tilde(p)
}

// XDGVars are the folder variables shared by every ctxtra process.
var XDGVars = []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"}

func xdg(key, fallback string) string {
	base := os.Getenv(key)
	if !filepath.IsAbs(base) {
		base = filepath.Join(Home(), fallback)
	}
	return filepath.Join(base, "ctxtra")
}

// ConfigDir holds the user's config.
func ConfigDir() string { return xdg("XDG_CONFIG_HOME", ".config") }

// DataDir holds accepted source content and installed skills.
func DataDir() string { return xdg("XDG_DATA_HOME", ".local/share") }

// StateDir holds local state, backups and the hook log.
func StateDir() string { return xdg("XDG_STATE_HOME", ".local/state") }

// CacheDir holds git clones that can be fetched again.
func CacheDir() string { return xdg("XDG_CACHE_HOME", ".cache") }

// SkillDir is where ctxtra skill install NAME writes ctxtra's skill name.
func SkillDir(name string) string { return filepath.Join(DataDir(), "skills", name) }

// BackupDir is where setup keeps a copy of every agent config it changes.
func BackupDir() string { return filepath.Join(StateDir(), "backups") }

// ConfigPath is config.yaml in the config folder.
func ConfigPath() string { return filepath.Join(ConfigDir(), "config.yaml") }

// LogPath is $CTXTRA_LOG or log.jsonl in the state folder.
func LogPath() string {
	if p := os.Getenv("CTXTRA_LOG"); p != "" {
		return Expand(p)
	}
	return filepath.Join(StateDir(), "log.jsonl")
}

// Rel returns p relative to root, slash-separated, when p is root (".") or
// below it. The paths are compared by components as cleaned text, without
// resolving symlinks: ..x is a name below root, .. is not.
func Rel(root, p string) (string, bool) {
	rel, err := filepath.Rel(root, filepath.Clean(p))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

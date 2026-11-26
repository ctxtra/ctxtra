package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ctxtra/ctxtra/internal/paths"
)

// MaxServerName is the longest server name: it leaves room for tool names
// in the 38 characters that every agent accepts.
const MaxServerName = 20

// CheckServerName says what is wrong with a server name, nil when nothing.
func CheckServerName(name string) error {
	if len(name) <= MaxServerName && reServer.MatchString(name) && !strings.Contains(name, "__") && !strings.HasSuffix(name, "_") {
		return nil
	}
	msg := fmt.Sprintf("server name %q: names are 1 to %d characters of a-z, 0-9 and _, start with a letter, and have no __ and no _ at the end", name, MaxServerName)
	if s := SuggestServerName(name); s != "" && s != name {
		msg += "; for example " + s
	}
	return fmt.Errorf("%s", msg)
}

var reServer = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// SuggestServerName is name made valid, or "" when nothing is left of it.
func SuggestServerName(name string) string {
	s := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(name), "_")
	s = strings.TrimLeft(s, "_0123456789")
	if len(s) > MaxServerName {
		s = s[:MaxServerName]
	}
	return strings.TrimRight(s, "_")
}

// CheckServerURL accepts https URLs, and http only on this machine.
func CheckServerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("url %q is not a valid server URL", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if host := u.Hostname(); host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("url %q: remote servers need https (http only for 127.0.0.1, ::1 or localhost)", raw)
}

// CheckProgram returns a local server's program as the proxy starts it: an
// absolute path, ~ expanded, or a bare name looked up in PATH. A relative
// path is refused: local servers start in the session folder, where the
// repository could supply the program.
func CheckProgram(p string) (string, error) {
	if strings.HasPrefix(p, "~/") {
		p = paths.Expand(p)
	}
	switch {
	case filepath.IsAbs(p):
		return filepath.Clean(p), nil
	case !strings.Contains(p, "/") && p != "." && p != "..":
		return p, nil
	}
	return "", fmt.Errorf("command %q: the program must be an absolute path or a name in PATH, not a relative path", p)
}

// SecretHeader reports whether a header's value is likely a secret.
func SecretHeader(h string) bool {
	l := strings.ToLower(h)
	return l == "authorization" || l == "cookie" || strings.Contains(l, "token") || strings.Contains(l, "key")
}

// SecretVar reports whether a variable's value is likely a secret.
func SecretVar(v string) bool {
	u := strings.ToUpper(v)
	return strings.HasSuffix(u, "_TOKEN") || strings.Contains(u, "PASSWORD") || strings.Contains(u, "SECRET") || strings.HasSuffix(u, "_KEY")
}

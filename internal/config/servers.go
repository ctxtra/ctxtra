package config

import (
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Server is an MCP server in the config's or a manifest's mcp-servers: a
// definition, or in the config a new name for a source's server.
type Server struct {
	Name string
	Pos  yamlx.Pos
	// UseSource and UseServer are the source's server that this entry
	// names (NEW: {use: 'SOURCE:SERVER'}); "" for a definition.
	UseSource string
	UseServer string
	// URL is a remote server's address; "" for a local server.
	URL string
	// Command is a local server's program and arguments, as written.
	Command       []string
	Notes         string
	Env           map[string]string
	PassEnv       []string
	SecretEnv     []string
	Headers       map[string]string
	SecretHeaders []string
	ClientID      string
	Scopes        []string
	Errs          []*yamlx.Error
}

// Broken reports whether an error leaves out the server.
func (s *Server) Broken() bool { return broken(s.Errs) }

// Use is the SOURCE:SERVER this entry renames, "" for a definition.
func (s *Server) Use() string {
	if s.UseSource == "" {
		return ""
	}
	return s.UseSource + ":" + s.UseServer
}

var (
	localKeys  = []string{"env", "pass-env", "secret-env"}
	remoteKeys = []string{"headers", "secret-headers", "client-id", "scopes"}
	reVar      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	reHeader   = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
)

// DecodeServer reads the server in p. Only the config may rename a source's
// server with use.
func DecodeServer(d *yamlx.Doc, p yamlx.Pair, owner Owner) *Server {
	s := &Server{Name: p.Key, Pos: yamlx.At(p.K)}
	r := yamlx.NewReader(d, yamlx.Part{Kind: "MCP server", Name: p.Key}, yamlx.BrokenServer, p.K, p.V)
	defer func() { s.Errs = r.Errs }()
	if err := CheckServerName(p.Key); err != nil {
		r.Fail(p.K, "%v", err)
	}
	if r.Broken() {
		return s
	}
	keys := slices.Concat([]string{"url", "command", "notes"}, localKeys, remoteKeys)
	if owner == InConfig {
		keys = append(keys, "use")
	}
	f, ok := r.Map(p.V, "a server", keys...)
	if !ok {
		return s
	}
	if n := f["use"]; n != nil {
		if len(f) > 1 {
			r.Fail(p.V, "a server with use has no other keys: it is the source's server under a new name")
			return s
		}
		decodeUse(r, n, s)
		return s
	}
	decodeDefinition(r, p.V, f, s)
	return s
}

func decodeUse(r *yamlx.Reader, n *yaml.Node, s *Server) {
	text, ok := r.Text(n, "use")
	if !ok {
		return
	}
	source, server, found := strings.Cut(text, ":")
	switch {
	case !found:
		r.Fail(n, "use %s: write the source's server as SOURCE:SERVER", text)
	case CheckName(source) != nil:
		r.Fail(n, "use %s: %v", text, CheckName(source))
	case CheckServerName(server) != nil:
		r.Fail(n, "use %s: %v", text, CheckServerName(server))
	default:
		s.UseSource, s.UseServer = source, server
	}
}

func decodeDefinition(r *yamlx.Reader, n *yaml.Node, f map[string]*yaml.Node, s *Server) {
	switch {
	case f["url"] != nil && f["command"] != nil:
		r.Fail(n, "a server has url or command, not both")
		return
	case f["url"] == nil && f["command"] == nil:
		r.Fail(n, "a server needs url (a remote server) or command (a local one)")
		return
	}
	other, kind := remoteKeys, "local"
	if f["url"] != nil {
		other, kind = localKeys, "remote"
		if s.URL, _ = r.Text(f["url"], "url"); s.URL != "" {
			if err := CheckServerURL(s.URL); err != nil {
				r.Fail(f["url"], "%v", err)
			}
		}
	} else {
		s.Command = textList(r, f["command"], "command", nil)
		if f["command"] != nil && len(s.Command) == 0 && !r.Broken() {
			r.Fail(f["command"], "command is empty")
		}
		if len(s.Command) > 0 {
			if _, err := CheckProgram(s.Command[0]); err != nil {
				r.Fail(f["command"], "%v", err)
			}
		}
	}
	for _, k := range keysIn(f, other...) {
		r.Fail(f[k], "%s doesn't apply to a %s server", k, kind)
	}
	if v := f["notes"]; v != nil {
		s.Notes, _ = r.Text(v, "notes")
	}
	if v := f["client-id"]; v != nil {
		s.ClientID, _ = r.Text(v, "client-id")
	}
	s.Env = textMap(r, f["env"], "env", reVar)
	s.Headers = textMap(r, f["headers"], "headers", reHeader)
	s.PassEnv = textList(r, f["pass-env"], "pass-env", reVar)
	s.SecretEnv = textList(r, f["secret-env"], "secret-env", reVar)
	s.SecretHeaders = textList(r, f["secret-headers"], "secret-headers", reHeader)
	s.Scopes = textList(r, f["scopes"], "scopes", nil)
	overlap(r, n, "variable", keysOf(s.Env), s.PassEnv, s.SecretEnv)
	overlap(r, n, "header", keysOf(s.Headers), s.SecretHeaders)
}

// texts reads a list of text; re, if set, is the rule for each item.
func textList(r *yamlx.Reader, n *yaml.Node, key string, re *regexp.Regexp) []string {
	if n == nil {
		return nil
	}
	items, ok := r.List(n, key)
	if !ok {
		return nil
	}
	var out []string
	for _, it := range items {
		v, ok := r.Text(it, key+" entry")
		if !ok {
			continue
		}
		switch {
		case re != nil && !re.MatchString(v):
			r.Fail(it, "%s: %q is not a valid name", key, v)
		case re != nil && slices.Contains(out, v):
			r.Fail(it, "%s: %s is listed twice", key, v)
		}
		out = append(out, v)
	}
	return out
}

// textMap reads a map of names to text.
func textMap(r *yamlx.Reader, n *yaml.Node, key string, re *regexp.Regexp) map[string]string {
	if n == nil {
		return nil
	}
	if n.Kind != yaml.MappingNode {
		r.Fail(n, "%s must be a map", key)
		return nil
	}
	out := map[string]string{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		name, ok := r.Text(k, key+" entry")
		if !ok {
			continue
		}
		if !re.MatchString(name) {
			r.Fail(k, "%s: %q is not a valid name", key, name)
			continue
		}
		if text, ok := r.Text(v, key+"."+name); ok {
			out[k.Value] = text
		}
	}
	return out
}

// overlap reports a name that two of a server's keys set; names compare
// without case.
func overlap(r *yamlx.Reader, n *yaml.Node, what string, lists ...[]string) {
	seen := map[string]bool{}
	for _, list := range lists {
		for _, k := range list {
			if seen[strings.ToLower(k)] {
				r.Fail(n, "the %s %s is set twice", what, k)
			}
			seen[strings.ToLower(k)] = true
		}
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// checkUses reports config entries that give one source server two names:
// each names the other.
func checkUses(servers []*Server) {
	byUse := map[string][]*Server{}
	for _, s := range servers {
		if u := s.Use(); u != "" && !s.Broken() {
			byUse[u] = append(byUse[u], s)
		}
	}
	for _, s := range servers {
		same := byUse[s.Use()]
		if s.Use() == "" || len(same) < 2 {
			continue
		}
		var others []string
		for _, o := range same {
			if o != s {
				others = append(others, o.Name)
			}
		}
		s.Errs = append(s.Errs, &yamlx.Error{Pos: s.Pos, Mistake: yamlx.BrokenServer, Part: yamlx.Part{Kind: "MCP server", Name: s.Name},
			Msg: "uses " + s.Use() + ", as " + strings.Join(others, " and ") + " does; give one server one name"})
	}
}

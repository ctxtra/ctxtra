package catalog

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ctxtra/ctxtra/internal/cfg"
	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/paths"
)

// Server is an MCP server that an entry adds.
type Server struct {
	upstream.Server
	// Refs are the names the config can give it: the config's own server's
	// name, or SOURCE:SERVER and the config's new name for it.
	Refs []string
}

// Servers are the servers that positive entry e of an mcp-servers list
// adds, in the order of their declarations, and the errors of what it
// couldn't add: a config name whose source server can't be used leaves out
// only that name. A source's server is served under the config's new name
// for it, if the config gives one. A source whose content can't be used
// gives *Unavailable.
func (c *Catalog) Servers(e cfg.Entry) ([]Server, []error) {
	if e.InSource {
		return c.sourceServers(e.Source, e.Value, e.Text)
	}
	var out []Server
	var errs []error
	matched := false
	for _, s := range c.config.MCPServers {
		if !nameMatch(e.Value, s.Name) {
			continue
		}
		matched = true
		if s.Broken() {
			continue
		}
		if s.Use() == "" {
			d := define(s, s.Name)
			d.ID = s.Name
			out = append(out, Server{Server: d, Refs: []string{s.Name}})
			continue
		}
		found, ferrs := c.sourceServers(s.UseSource, s.UseServer, s.Name+" (use "+s.Use()+")")
		out = append(out, found...)
		errs = append(errs, ferrs...)
	}
	if !matched {
		return nil, []error{fmt.Errorf("the config has no MCP server %s", e.Value)}
	}
	return out, errs
}

// sourceServers are the servers of source name that pattern matches; text
// is the entry for messages.
func (c *Catalog) sourceServers(name, pattern, text string) ([]Server, []error) {
	s := c.sources[name]
	switch {
	case s == nil:
		return nil, []error{fmt.Errorf("%s: the config has no source %s", text, name)}
	case s.State != Active:
		return nil, []error{&Unavailable{s}}
	}
	m, _ := c.Manifest(s)
	if m == nil {
		return nil, []error{fmt.Errorf("%s: source %s has no %s, which declares a source's MCP servers", text, name, manifest.FileName)}
	}
	var out []Server
	matched := false
	for _, d := range m.MCPServers {
		if !nameMatch(pattern, d.Name) {
			continue
		}
		matched = true
		if d.Broken() {
			continue
		}
		ref := name + ":" + d.Name
		served := d.Name
		refs := []string{ref}
		if n := c.renamed[ref]; n != "" {
			served = n
			refs = append(refs, n)
		}
		u := define(d, served)
		u.ID, u.Source, u.Origin, u.Pinned = ref, name, origin(s), !s.Live()
		out = append(out, Server{Server: u, Refs: refs})
	}
	if !matched {
		return nil, []error{fmt.Errorf("%s: source %s declares no MCP server %s", text, name, pattern)}
	}
	return out, nil
}

// nameMatch reports whether name is pattern, or a name the pattern matches.
func nameMatch(pattern, name string) bool {
	if !hasMeta(pattern) {
		return pattern == name
	}
	ok, err := doublestar.Match(pattern, name)
	return err == nil && ok
}

// origin ties a source's servers' approvals and sign-ins to where the
// source's files come from: another repository or folder is a new source.
func origin(s *Source) string {
	if s.Live() {
		return "dir:" + paths.Real(paths.Expand(s.Dir))
	}
	o := "repo:" + cfg.RepoID(s.Repo)
	if sub := cfg.CleanSubdir(s.Subdir); sub != "" {
		o += "//" + sub
	}
	return o
}

// define is the definition of d, served as name.
func define(d *cfg.Server, name string) upstream.Server {
	return upstream.Server{
		Name:          name,
		URL:           d.URL,
		Command:       d.Command,
		Env:           d.Env,
		EnvPass:       d.PassEnv,
		SecretEnv:     d.SecretEnv,
		Headers:       d.Headers,
		SecretHeaders: d.SecretHeaders,
		ClientID:      d.ClientID,
		Scopes:        d.Scopes,
		Notes:         strings.TrimSpace(d.Notes),
	}
}

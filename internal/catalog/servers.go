package catalog

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ctxtra/ctxtra/internal/config"
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
func (c *Catalog) Servers(e config.Entry) ([]Server, []error) {
	if e.InSource {
		return c.sourceServers(e.Source, e.Value, e.Text)
	}
	var out []Server
	var errs []error
	matched := false
	for _, s := range c.conf.MCPServers {
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
	switch {
	case !matched:
		return nil, []error{fmt.Errorf("the config has no MCP server %s", e.Value)}
	case len(out) == 0 && len(errs) == 0:
		return nil, []error{fmt.Errorf("the config's MCP server %s has errors", e.Value)}
	}
	return out, errs
}

// ManifestSources are the sources whose manifests positive entry e of an
// mcp-servers list reads: its own source, or the sources of the config's
// new names it matches.
func (c *Catalog) ManifestSources(e config.Entry) []string {
	if e.InSource {
		return []string{e.Source}
	}
	var out []string
	for _, s := range c.conf.MCPServers {
		if s.Use() != "" && !s.Broken() && nameMatch(e.Value, s.Name) && !slices.Contains(out, s.UseSource) {
			out = append(out, s.UseSource)
		}
	}
	return out
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
	switch {
	case !matched:
		return nil, []error{fmt.Errorf("%s: source %s declares no MCP server %s", text, name, pattern)}
	case len(out) == 0:
		return nil, []error{fmt.Errorf("%s: the MCP server of source %s has errors", text, name)}
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
	o := "repo:" + config.RepoID(s.Repo)
	if sub := config.CleanSubdir(s.Subdir); sub != "" {
		o += "//" + sub
	}
	return o
}

// define is the definition of d, served as name.
func define(d *config.Server, name string) upstream.Server {
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

// SourceServers are every server that the manifest of source name
// declares, without broken ones, served under the config's new names; the
// errors are the manifest's. A source whose content can't be used has
// none.
func (c *Catalog) SourceServers(name string) ([]Server, []*Error) {
	s := c.sources[name]
	if s == nil || s.State != Active {
		return nil, nil
	}
	m, errs := c.Manifest(s)
	if m == nil {
		return nil, errs
	}
	found, _ := c.sourceServers(name, "*", name+":*")
	return found, errs
}

// AllServers are the config's own servers and every server of the
// manifests of the sources this machine can read, in config order: what
// commands that list, sign in to or approve servers know. The errors are
// those of the manifests read.
func (c *Catalog) AllServers() ([]Server, []*Error) {
	var out []Server
	for _, s := range c.conf.MCPServers {
		if s.Broken() || s.Use() != "" {
			continue
		}
		d := define(s, s.Name)
		d.ID = s.Name
		out = append(out, Server{Server: d, Refs: []string{s.Name}})
	}
	var errs []*Error
	for _, src := range c.order {
		found, e := c.SourceServers(src.Name)
		out = append(out, found...)
		errs = append(errs, e...)
	}
	return out, errs
}

// Server is the server that ref names: a config server's name, the
// config's new name for a source's server, or SOURCE:SERVER. ok is false
// when no usable server has that name.
func (c *Catalog) Server(ref string) (Server, bool) {
	if hasMeta(ref) {
		return Server{}, false
	}
	e := config.Entry{Text: ref, Value: ref}
	if source, name, found := strings.Cut(ref, ":"); found {
		e.InSource, e.Source, e.Value = true, source, name
	}
	found, errs := c.Servers(e)
	if len(found) != 1 || len(errs) > 0 {
		return Server{}, false
	}
	return found[0], true
}

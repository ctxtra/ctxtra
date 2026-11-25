// Package cfg is the model of the config file, config.yaml: what the user
// wrote, entry by entry, with each entry's ! flag, position and errors.
// Parsing checks the shape and the names; it reads no other file.
package cfg

import (
	"cmp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// Options are what parsing needs to know beyond the file.
type Options struct {
	// Agents are the names that the agents condition accepts.
	Agents []string
}

// Config is the config file as written. Named things keep the file's order.
type Config struct {
	Sources     []*Source
	MCPServers  []*Server
	Collections []*Collection
	RepoSets    []*RepoSet
	// Bindings are in list order: the order of their text in a session.
	Bindings []*Binding
	// MaxCtxChars is the most text added at session start; nil when unset.
	MaxCtxChars *int
	// CheckReleases is false when checks for new ctxtra releases are off;
	// nil when unset.
	CheckReleases  *bool
	SessionMessage SessionMessage
	// Errors are every error in the file, in file order. A config that
	// can't be read has one error and nothing else.
	Errors []*yamlx.Error
}

// SessionMessage is how much the session message lists; nil when unset.
type SessionMessage struct {
	MaxSkills       *int
	MaxInstructions *int
}

var topKeys = []string{"schema", "sources", "mcp-servers", "collections", "repo-sets", "bindings", "max-ctx-chars", "check-releases", "session-message"}

// Parse reads a config file. It always returns a config: parts with errors
// are left out as the errors say, and the rest is there.
func Parse(data []byte, opts Options) *Config {
	c := &Config{}
	d, err := yamlx.Parse(data, yamlx.ConfigKind)
	if err != nil {
		c.Errors = []*yamlx.Error{err}
		return c
	}
	pairs, errs := d.Pairs(d.Root, keyPart)
	c.Errors = errs
	named := func(p yamlx.Pair, kind string) []yamlx.Pair {
		entries, errs := NamedMap(d, p, kind)
		c.Errors = append(c.Errors, errs...)
		return entries
	}
	for _, p := range pairs {
		switch p.Key {
		case "schema":
		case "sources":
			for _, e := range named(p, "source") {
				c.Sources = append(c.Sources, decodeSource(d, e))
			}
		case "mcp-servers":
			for _, e := range named(p, "MCP server") {
				c.MCPServers = append(c.MCPServers, DecodeServer(d, e, InConfig))
			}
		case "collections":
			for _, e := range named(p, "collection") {
				c.Collections = append(c.Collections, DecodeCollection(d, e, InConfig))
			}
		case "repo-sets":
			for _, e := range named(p, "repo set") {
				c.RepoSets = append(c.RepoSets, DecodeRepoSet(d, e))
			}
		case "bindings":
			items, err := TopList(d, p)
			c.Errors = append(c.Errors, errList(err)...)
			for i, n := range items {
				c.Bindings = append(c.Bindings, decodeBinding(d, n, i, opts.Agents))
			}
		case "max-ctx-chars", "check-releases", "session-message":
			c.decodeSetting(d, p)
		default:
			c.Errors = append(c.Errors, UnknownKey(p, topKeys[1:]))
		}
	}
	checkUses(c.MCPServers)
	checkBindingNames(c.Bindings)
	for _, s := range c.Sources {
		c.Errors = append(c.Errors, s.Errs...)
	}
	for _, s := range c.MCPServers {
		c.Errors = append(c.Errors, s.Errs...)
	}
	for _, s := range c.Collections {
		c.Errors = append(c.Errors, s.Errs...)
	}
	for _, s := range c.RepoSets {
		c.Errors = append(c.Errors, s.Errs...)
	}
	for _, b := range c.Bindings {
		c.Errors = append(c.Errors, b.Errs...)
	}
	SortErrors(c.Errors)
	return c
}

// SortErrors puts errs in file order.
func SortErrors(errs []*yamlx.Error) {
	slices.SortStableFunc(errs, func(a, b *yamlx.Error) int {
		return cmp.Or(cmp.Compare(a.Pos.Line, b.Pos.Line), cmp.Compare(a.Pos.Column, b.Pos.Column))
	})
}

func keyPart(key string) yamlx.Part { return yamlx.Part{Kind: "key", Name: key} }

// NamedMap returns the entries of the map under top-level key p. A key
// that isn't a map is left out; an entry whose name is there twice is left
// out with the others of that name.
func NamedMap(d *yamlx.Doc, p yamlx.Pair, kind string) ([]yamlx.Pair, []*yamlx.Error) {
	if err := topNode(d, p, yaml.MappingNode, "a map of "+kind+"s by name"); err != nil || yamlx.IsNull(p.V) {
		return nil, errList(err)
	}
	return d.Pairs(p.V, func(name string) yamlx.Part { return yamlx.Part{Kind: kind, Name: name} })
}

// TopList returns the items of the list under top-level key p.
func TopList(d *yamlx.Doc, p yamlx.Pair) ([]*yaml.Node, *yamlx.Error) {
	if err := topNode(d, p, yaml.SequenceNode, "a list"); err != nil || yamlx.IsNull(p.V) {
		return nil, err
	}
	return p.V.Content, nil
}

// UnknownKey is the error of a top-level key that the file doesn't have.
func UnknownKey(p yamlx.Pair, known []string) *yamlx.Error {
	return yamlx.Errorf(p.K, yamlx.UnknownTopKey, keyPart(p.Key), "unknown key %q (known: %s)", p.Key, strings.Join(known, ", "))
}

// topNode checks the value of a top-level key that holds parts: null is
// empty, and a tag or another kind leaves out the key.
func topNode(d *yamlx.Doc, p yamlx.Pair, kind yaml.Kind, what string) *yamlx.Error {
	if err := d.CheckNode(keyPart(p.Key), p.V); err != nil {
		return err
	}
	if !yamlx.IsNull(p.V) && p.V.Kind != kind {
		return yamlx.Errorf(p.V, yamlx.BadValue, keyPart(p.Key), "%s must be %s", p.Key, what)
	}
	return nil
}

func errList(err *yamlx.Error) []*yamlx.Error {
	if err == nil {
		return nil
	}
	return []*yamlx.Error{err}
}

// decodeSetting reads a program setting. A wrong value leaves out the key,
// so its default applies.
func (c *Config) decodeSetting(d *yamlx.Doc, p yamlx.Pair) {
	r := yamlx.NewReader(d, keyPart(p.Key), yamlx.BadValue, p.V)
	defer func() { c.Errors = append(c.Errors, r.Errs...) }()
	if r.Broken() {
		return
	}
	switch p.Key {
	case "max-ctx-chars":
		if v, ok := r.Count(p.V, p.Key); ok {
			c.MaxCtxChars = &v
		}
	case "check-releases":
		if v, ok := r.Bool(p.V, p.Key); ok {
			c.CheckReleases = &v
		}
	case "session-message":
		f, ok := r.Map(p.V, "session-message", "max-skills", "max-instructions")
		if !ok {
			return
		}
		var m SessionMessage
		for key, dst := range map[string]**int{"max-skills": &m.MaxSkills, "max-instructions": &m.MaxInstructions} {
			if n := f[key]; n != nil {
				if v, ok := r.Count(n, "session-message."+key); ok {
					*dst = &v
				}
			}
		}
		if !r.Broken() {
			c.SessionMessage = m
		}
	}
}

// Source is the source called name, nil when there is none.
func (c *Config) Source(name string) *Source { return find(c.Sources, name, (*Source).name) }

// MCPServer is the config's server called name, nil when there is none.
func (c *Config) MCPServer(name string) *Server { return find(c.MCPServers, name, (*Server).name) }

// Collection is the config's collection called name, nil when there is none.
func (c *Config) Collection(name string) *Collection {
	return find(c.Collections, name, (*Collection).name)
}

// RepoSet is the config's repo set called name, nil when there is none.
func (c *Config) RepoSet(name string) *RepoSet { return find(c.RepoSets, name, (*RepoSet).name) }

// Binding is the binding called name, nil when there is none.
func (c *Config) Binding(name string) *Binding { return find(c.Bindings, name, (*Binding).name) }

func (s *Source) name() string     { return s.Name }
func (s *Server) name() string     { return s.Name }
func (s *Collection) name() string { return s.Name }
func (s *RepoSet) name() string    { return s.Name }
func (s *Binding) name() string    { return s.Name }

func find[T any](list []*T, name string, key func(*T) string) *T {
	for _, x := range list {
		if key(x) == name {
			return x
		}
	}
	return nil
}

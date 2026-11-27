// Package manifest is the model of a source's manifest, ctxtra-source.yaml:
// the collections, MCP servers and repo sets that the source offers. A
// manifest has no bindings and no sources: the user's config binds what a
// source offers.
package manifest

import (
	"github.com/ctxtra/ctxtra/internal/config"
	"github.com/ctxtra/ctxtra/internal/yamlx"
)

// FileName is the manifest's name at the top of a source.
const FileName = "ctxtra-source.yaml"

// Manifest is a manifest as written. Named things keep the file's order.
type Manifest struct {
	Collections []*config.Collection
	MCPServers  []*config.Server
	RepoSets    []*config.RepoSet
	// Errors are every error in the file, in file order. A manifest that
	// can't be read has one error and nothing else.
	Errors []*yamlx.Error
}

var topKeys = []string{"schema", "collections", "mcp-servers", "repo-sets"}

// Parse reads a manifest. It always returns a manifest: parts with errors
// are left out as the errors say, and the rest is there.
func Parse(data []byte) *Manifest {
	m := &Manifest{}
	d, err := yamlx.Parse(data, yamlx.ManifestKind)
	if err != nil {
		m.Errors = []*yamlx.Error{err}
		return m
	}
	for i := 0; i+1 < len(d.Root.Content); i += 2 {
		k := d.Root.Content[i]
		if k.Value == "bindings" || k.Value == "sources" {
			m.Errors = []*yamlx.Error{yamlx.Errorf(k, yamlx.Unreadable, yamlx.Part{},
				"a manifest has no %s: a source provides content, and the user's config binds it", k.Value)}
			return m
		}
	}
	pairs, errs := d.Pairs(d.Root, func(key string) yamlx.Part { return yamlx.Part{Kind: "key", Name: key} })
	m.Errors = errs
	named := func(p yamlx.Pair, kind string) []yamlx.Pair {
		entries, errs := config.NamedMap(d, p, kind)
		m.Errors = append(m.Errors, errs...)
		return entries
	}
	for _, p := range pairs {
		switch p.Key {
		case "schema":
		case "collections":
			for _, e := range named(p, "collection") {
				c := config.DecodeCollection(d, e, config.InManifest)
				m.Collections = append(m.Collections, c)
				m.Errors = append(m.Errors, c.Errs...)
			}
		case "mcp-servers":
			for _, e := range named(p, "MCP server") {
				s := config.DecodeServer(d, e, config.InManifest)
				m.MCPServers = append(m.MCPServers, s)
				m.Errors = append(m.Errors, s.Errs...)
			}
		case "repo-sets":
			for _, e := range named(p, "repo set") {
				s := config.DecodeRepoSet(d, e)
				m.RepoSets = append(m.RepoSets, s)
				m.Errors = append(m.Errors, s.Errs...)
			}
		default:
			m.Errors = append(m.Errors, config.UnknownKey(p, topKeys[1:]))
		}
	}
	config.SortErrors(m.Errors)
	return m
}

// Collection is the collection called name, nil when there is none.
func (m *Manifest) Collection(name string) *config.Collection {
	for _, c := range m.Collections {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// MCPServer is the server called name, nil when there is none.
func (m *Manifest) MCPServer(name string) *config.Server {
	for _, s := range m.MCPServers {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// RepoSet is the repo set called name, nil when there is none.
func (m *Manifest) RepoSet(name string) *config.RepoSet {
	for _, s := range m.RepoSets {
		if s.Name == name {
			return s
		}
	}
	return nil
}

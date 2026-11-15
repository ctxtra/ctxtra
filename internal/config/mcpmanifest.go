package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ctxtra/ctxtra/internal/fsutil"
	"github.com/ctxtra/ctxtra/internal/paths"
)

// MCPManifest is the only server declaration file in a source.
const MCPManifest = "mcp-servers.yaml"

// MCPDeclared reports valid and invalid declarations, including source
// definitions. It distinguishes removal of a definition from credential cleanup.
func (c *Config) MCPDeclared(ref string) bool {
	_, ok := c.MCPServer(ref)
	return ok || c.mcpBroken[ref]
}

// CheckMCPRef validates a config name or a source-qualified reference.
func CheckMCPRef(ref string) error {
	scope, name, qualified := strings.Cut(ref, "/")
	if !qualified {
		return CheckServerName(ref)
	}
	if !ValidName(scope) {
		return fmt.Errorf("server %q: invalid source name %q", ref, scope)
	}
	return CheckServerName(name)
}

func (s Source) mcpOrigin() string {
	switch {
	case s.Git != "":
		return "git:" + s.Git
	case s.LocalGit != "":
		return "local-git:" + s.LocalGit
	case s.dir != "":
		return "check:" + paths.Real(s.dir)
	default:
		return "trusted-path:" + paths.Real(s.TrustedPath)
	}
}

// LoadMCPCatalog loads every source manifest for list, setup and doctor.
// Session binding resolution loads only the sources it references.
func (c *Config) LoadMCPCatalog() {
	if !mcpOn() || c.Ignored() {
		return
	}
	for _, src := range c.Sources {
		c.loadMCPManifest(src)
	}
}

// LoadMCPSource loads only the named source's manifest for source review.
func (c *Config) LoadMCPSource(name string) {
	if src, ok := c.Source(name); ok && !c.Ignored() {
		c.loadMCPManifest(src)
	}
}

func (c *Config) loadMCPManifest(src Source) {
	if !mcpOn() || c.mcpLoaded[src.Name] {
		return
	}
	if c.mcpLoaded == nil {
		c.mcpLoaded = map[string]bool{}
	}
	c.mcpLoaded[src.Name] = true
	if c.Trust == nil {
		c.Trust = c.policy()
	}
	ref := Scheme + src.Name + "/" + MCPManifest
	var file string
	var err error
	switch {
	case src.dir != "":
		file = filepath.Join(src.dir, MCPManifest)
		if reason := LeftOut(src.dir, file); reason != "" {
			c.problem(ref, fmt.Errorf("manifest %s; skipped", reason))
			return
		}
	case src.Pinned():
		file, _, err = c.inStore(ref, src, MCPManifest)
	default:
		file = filepath.Join(src.TrustedPath, MCPManifest)
		err = c.Trust.Check(file)
	}
	if err != nil {
		c.problem(ref, err)
		return
	}
	if c.mcpReader == nil {
		c.mcpReader = fsutil.NewReader(MaxFileBytes, MaxSessionBytes)
	}
	data, err := c.mcpReader.Read(file)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		c.problem(ref, err)
		return
	}
	docs, err := parseDocs(data)
	if err == nil {
		switch {
		case len(docs) != 1:
			err = errors.New("manifest must be a single YAML document")
		case docs[0].err != nil:
			err = docs[0].err
		default:
			f, ferr := fields(docs[0].node, "schema", "mcp-servers")
			err = ferr
			if err == nil {
				err = checkSchema(docs[0].node, f)
			}
			if err == nil {
				err = require(docs[0].node, f, "mcp-servers")
			}
			if err == nil {
				c.decodeServerMap(f["mcp-servers"], ref, &src)
				return
			}
		}
	}
	c.problem(ref, fmt.Errorf("%s; no servers from this manifest are used", flat(err)))
}

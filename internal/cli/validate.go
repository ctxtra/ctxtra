package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/ctxtra/ctxtra/internal/catalog"
	"github.com/ctxtra/ctxtra/internal/manifest"
	"github.com/ctxtra/ctxtra/internal/mcp/upstream"
	"github.com/ctxtra/ctxtra/internal/paths"
	"github.com/ctxtra/ctxtra/internal/resolve"
)

// validateResult is the data of source validate --json.
type validateResult struct {
	Dir         string `json:"dir"`
	Collections int    `json:"collections"`
	MCPServers  int    `json:"mcp_servers"`
	RepoSets    int    `json:"repo_sets"`
	Errors      int    `json:"errors"`
	Warnings    int    `json:"warnings"`
}

// validateSource is source validate: it reads the manifest of the folder
// dir, a checkout of a source, and its collections, the way sessions read
// them from an accepted commit, and reports their mistakes. It reads no
// config and runs and fetches nothing. 1 when it finds an error.
func validateSource(stdio IO, dir string, asJSON bool) int {
	fail := func(err error) int {
		fmt.Fprintf(stdio.Err, "ctxtra source validate: %v\n", err)
		return 1
	}
	if info, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return fail(fmt.Errorf("%s does not exist", paths.Tilde(dir)))
	} else if err != nil {
		return fail(err)
	} else if !info.IsDir() {
		return fail(fmt.Errorf("%s is not a folder", paths.Tilde(dir)))
	}
	file := filepath.Join(dir, manifest.FileName)
	switch st, err := os.Lstat(file); {
	case errors.Is(err, fs.ErrNotExist):
		return fail(fmt.Errorf("%s has no %s", paths.Tilde(dir), manifest.FileName))
	case err != nil:
		return fail(err)
	case !st.Mode().IsRegular():
		return fail(fmt.Errorf("%s is not a regular file: sessions don't read a symlink in a repo source", manifest.FileName))
	}
	// One read, with the limits of sessions.
	top := paths.Real(dir)
	name := filepath.Base(top)
	cat := catalog.Checkout(name, top)
	m, merrs := cat.Manifest(cat.Source(name))
	if m == nil {
		m = &manifest.Manifest{}
	}
	var errs, warns []string
	for _, e := range merrs {
		errs = append(errs, e.Err.Error())
	}
	for _, e := range resolve.CheckCollections(cat, name) {
		errs = append(errs, e.Err.Error())
	}
	for _, s := range m.MCPServers {
		if !s.Broken() {
			warns = append(warns, secretNotes(s.Name, upstream.Server{Headers: s.Headers, Env: s.Env}, "manifest")...)
		}
	}
	if asJSON {
		writeJSON(stdio.Out, validateResult{dir, len(m.Collections), len(m.MCPServers), len(m.RepoSets), len(errs), len(warns)},
			slices.Concat(diagnostics("error", errs), diagnostics("warning", warns)))
	} else {
		fmt.Fprintf(stdio.Out, "validated %s: %s, %s, %s\n", paths.Tilde(file), plural(len(m.Collections), "collection"),
			plural(len(m.MCPServers), "MCP server"), plural(len(m.RepoSets), "repo set"))
		for _, e := range errs {
			fmt.Fprintf(stdio.Out, "error: %s\n", e)
		}
		for _, w := range warns {
			fmt.Fprintf(stdio.Out, "warning: %s\n", w)
		}
		fmt.Fprintf(stdio.Out, "%s, %s\n", plural(len(errs), "error"), plural(len(warns), "warning"))
	}
	if len(errs) > 0 {
		return 1
	}
	return 0
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

package main

import (
	"go/types"
	"strings"
)

// isStdlibPath reports whether an import path belongs to the Go standard
// library: the first path element of stdlib packages never contains a dot
// (e.g. "fmt", "net/http", "sync/atomic"), while modules are rooted at a
// domain ("github.com/...", "golang.org/x/...").
func isStdlibPath(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// classify labels the package that owns a callee: local (this module),
// stdlib, or module (external dependency).
func (a *analyzer) classify(pkg *types.Package) string {
	if pkg == nil {
		return "builtin"
	}
	if a.localPkgs[pkg] {
		return "local"
	}
	path := pkg.Path()
	if path == a.modPath || strings.HasPrefix(path, a.modPath+"/") {
		return "local"
	}
	if isStdlibPath(path) {
		return "stdlib"
	}
	return "module"
}

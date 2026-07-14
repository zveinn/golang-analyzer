package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func usage() {
	fmt.Fprintf(os.Stderr, `usage: %[1]s

Starts the code-analyzer server:
  - web UI on http://%[2]s
  - TCP intake on %[3]s accepting:
      file:line[:param:value]...   trace the function at file:line
      scan:dir                     scan every .go file under dir for
                                   data races, closed-channel writes,
                                   unclosed files and goroutine leaks

Analysis is triggered exclusively via the TCP intake — use ./client:
  ./client <file.go> <line> [param value]...
  ./client scan <dir>

Trace parameters:
  depth  <n>          max call-expansion depth (default %[4]d)
  expand once|all     expand each function body once (default) or at
                      every call site
`, filepath.Base(os.Args[0]), uiAddr, tcpAddr, defaultMaxDepth)
	os.Exit(2)
}

func main() {
	if len(os.Args) != 1 {
		usage()
	}
	if err := runServer(); err != nil {
		fatal(err)
	}
}

// runTrace analyzes the function at path:line and returns the structured
// trace tree. Called only from the TCP intake.
func runTrace(path string, line int, params map[string]string) (*node, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	a, err := newAnalyzer(abs)
	if err != nil {
		return nil, err
	}
	if err := a.applyParams(params); err != nil {
		return nil, err
	}
	t, err := a.findFunc(abs, line)
	if err != nil {
		return nil, err
	}
	return a.trace(t), nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func usage() {
	fmt.Fprintf(os.Stderr, `usage: %s <file.go> <line>

Traces all execution paths of the function declared at <file.go>:<line>.

Labels:
  [local]   function defined in the target file's module (traced into)
  [stdlib]  Go standard library (not traced into)
  [module]  external dependency (not traced into)

Trace markers:
  [GOROUTINE LAUNCH]  a goroutine is started
  [LOOP N]            calls inside are executed in a loop
  [CHAN SEND/RECV/CLOSE] channel operation, with opposite endpoints listed
  arg x ← …           where a call argument was allocated / produced
`, filepath.Base(os.Args[0]))
	os.Exit(2)
}

func main() {
	if len(os.Args) != 3 {
		usage()
	}
	file, err := filepath.Abs(os.Args[1])
	if err != nil {
		fatal(err)
	}
	line, err := strconv.Atoi(os.Args[2])
	if err != nil || line < 1 {
		fmt.Fprintf(os.Stderr, "invalid line number %q\n", os.Args[2])
		os.Exit(2)
	}
	a, err := newAnalyzer(file)
	if err != nil {
		fatal(err)
	}
	t, err := a.findFunc(file, line)
	if err != nil {
		fatal(err)
	}
	render(os.Stdout, a.trace(t))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

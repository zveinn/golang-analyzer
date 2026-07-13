// Command client sends a single analysis request to the code-analyzer
// server's TCP intake and prints the acknowledgement.
//
// usage: client <file.go> <line> [param value]...
// example: client examples/main.go 36 depth 10 expand all
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultAddr = "127.0.0.1:1112"

func main() {
	args := os.Args[1:]
	if len(args) < 2 || len(args)%2 != 0 {
		fmt.Fprintf(os.Stderr, `usage: %s <file.go> <line> [param value]...

Sends "file:line[:param:value]..." to the code-analyzer TCP intake
(%s, override with CODE_ANALYZER_ADDR).
`, filepath.Base(os.Args[0]), defaultAddr)
		os.Exit(2)
	}

	// The server resolves relative paths against its own working directory,
	// so send an absolute path.
	if abs, err := filepath.Abs(args[0]); err == nil {
		args[0] = abs
	}

	addr := os.Getenv("CODE_ANALYZER_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot reach server at %s: %v\n", addr, err)
		os.Exit(1)
	}
	defer conn.Close()

	if _, err := fmt.Fprintln(conn, strings.Join(args, ":")); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	resp, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: no response:", err)
		os.Exit(1)
	}
	resp = strings.TrimSpace(resp)
	fmt.Println(resp)
	if strings.HasPrefix(resp, "error:") {
		os.Exit(1)
	}
}

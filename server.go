package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	uiAddr      = "0.0.0.0:1111"
	tcpAddr     = "0.0.0.0:1112"
	historySize = 50
)

//go:embed all:ui/dist
var uiFiles embed.FS

// wsMessage is the envelope pushed to the UI. The UI renders it blindly —
// all analysis happens on this side. Traces carry the structured node tree
// in Root; errors carry a message in Text.
type wsMessage struct {
	Type   string            `json:"type"` // "trace" | "error"
	Target string            `json:"target"`
	Params map[string]string `json:"params,omitempty"`
	Time   string            `json:"time"`
	Text   string            `json:"text,omitempty"`
	Root   *node             `json:"root,omitempty"`
}

// hub fans analysis results out to every connected websocket and replays
// recent history to freshly connected UIs.
type hub struct {
	mu        sync.Mutex
	clients   map[*websocket.Conn]bool
	history   [][]byte
	analyzeMu sync.Mutex // one analysis at a time keeps memory bounded
}

func newHub() *hub {
	return &hub{clients: map[*websocket.Conn]bool{}}
}

func runServer() error {
	h := newHub()

	ln, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		return fmt.Errorf("tcp intake: %w", err)
	}
	log.Printf("TCP intake listening on %s (format: file:line[:param:value]...)", tcpAddr)
	go h.acceptTCP(ln)

	dist, err := fs.Sub(uiFiles, "ui/dist")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(dist)))
	mux.HandleFunc("/ws", h.handleWS)
	log.Printf("UI listening on http://%s (all interfaces)", uiAddr)
	return http.ListenAndServe(uiAddr, mux)
}

// --- websocket side ---

var upgrader = websocket.Upgrader{
	// The UI is served from the same host; allow local tools too.
	CheckOrigin: func(*http.Request) bool { return true },
}

func (h *hub) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.mu.Lock()
	h.clients[conn] = true
	for _, m := range h.history {
		if err := conn.WriteMessage(websocket.TextMessage, m); err != nil {
			break
		}
	}
	h.mu.Unlock()
	log.Printf("UI connected (%s)", r.RemoteAddr)

	// Nothing is expected from the UI; read only to notice the close.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				h.mu.Lock()
				delete(h.clients, conn)
				h.mu.Unlock()
				conn.Close()
				return
			}
		}
	}()
}

func (h *hub) broadcast(m wsMessage) {
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history = append(h.history, data)
	if len(h.history) > historySize {
		h.history = h.history[len(h.history)-historySize:]
	}
	for c := range h.clients {
		if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
			c.Close()
			delete(h.clients, c)
		}
	}
}

// --- tcp intake side ---

func (h *hub) acceptTCP(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("tcp accept: %v", err)
			return
		}
		go h.handleTCP(conn)
	}
}

func (h *hub) handleTCP(conn net.Conn) {
	defer conn.Close()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fmt.Fprintln(conn, h.process(line))
	}
}

// process handles one request from the TCP intake:
//
//	file:line[:param:value]...   trace the function at file:line
//	scan:dir                     scan every .go file under dir
//
// The analysis result is pushed to the UI; the return value is the ack for
// the TCP client.
func (h *hub) process(msg string) string {
	now := time.Now().Format("15:04:05")
	req, err := parseRequest(msg)
	if err != nil {
		h.broadcast(wsMessage{Type: "error", Target: msg, Time: now, Text: err.Error()})
		return "error: " + err.Error()
	}

	if req.scan {
		log.Printf("scanning %s", req.dir)
		h.analyzeMu.Lock()
		root, err := runScan(req.dir, req.params)
		h.analyzeMu.Unlock()
		if err != nil {
			h.broadcast(wsMessage{Type: "error", Target: req.dir, Time: now, Text: err.Error()})
			return "error: " + err.Error()
		}
		h.broadcast(wsMessage{Type: "scan", Target: req.dir, Time: now, Root: root})
		return fmt.Sprintf("ok: scan of %s pushed to UI (%d findings)", req.dir, countFindings(root))
	}

	target := fmt.Sprintf("%s:%d", req.file, req.line)
	log.Printf("analyzing %s %v", target, req.params)
	h.analyzeMu.Lock()
	root, err := runTrace(req.file, req.line, req.params)
	h.analyzeMu.Unlock()
	if err != nil {
		h.broadcast(wsMessage{Type: "error", Target: target, Params: req.params, Time: now, Text: err.Error()})
		return "error: " + err.Error()
	}
	h.broadcast(wsMessage{Type: "trace", Target: target, Params: req.params, Time: now, Root: root})
	return fmt.Sprintf("ok: trace of %s pushed to UI (%d nodes)", target, countNodes(root))
}

func countNodes(n *node) int {
	total := 1
	for _, k := range n.Kids {
		total += countNodes(k)
	}
	return total
}

type request struct {
	scan   bool
	file   string
	line   int
	dir    string
	params map[string]string
}

func parseRequest(msg string) (*request, error) {
	parts := strings.Split(msg, ":")
	if len(parts) < 2 {
		return nil, fmt.Errorf("bad request %q (want file:line[:param:value]... or scan:dir)", msg)
	}
	var req request
	var rest []string
	if parts[0] == "scan" {
		req.scan = true
		req.dir = parts[1]
		rest = parts[2:]
	} else {
		req.file = parts[0]
		line, err := strconv.Atoi(parts[1])
		if err != nil || line < 1 {
			return nil, fmt.Errorf("bad line number %q in %q", parts[1], msg)
		}
		req.line = line
		rest = parts[2:]
	}
	if len(rest)%2 != 0 {
		return nil, fmt.Errorf("parameters must be key:value pairs, got %q", strings.Join(rest, ":"))
	}
	if len(rest) > 0 {
		req.params = make(map[string]string, len(rest)/2)
		for i := 0; i < len(rest); i += 2 {
			req.params[rest[i]] = rest[i+1]
		}
	}
	return &req, nil
}

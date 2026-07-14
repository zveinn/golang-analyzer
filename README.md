# code-analyzer

Static execution-path tracer for Go. Point it at a function declaration and it
traces every possible execution path as an indented call tree, including
goroutine launches, loops, channel endpoints and where call arguments were
allocated. Traces are displayed in a web UI (embedded React app), or on
stdout in CLI mode.

## Build & run

```
$ ./build-and-run.sh
```

This builds the UI (`ui/dist`, embedded via go:embed), the backend and the
TCP client, stops a previously running instance, and starts the server.
Manual steps, if you prefer:

```
$ cd ui && npm install && npm run build && cd ..   # builds ui/dist (embedded via go:embed)
$ go build -o code-analyzer .                      # server
$ go build -o client/client ./client               # TCP client
```

## Usage

```
$ ./code-analyzer                # UI on http://0.0.0.0:1111, TCP intake on :1112

# trace a function
$ ./client/client <file.go> <line> [param value]...
$ ./client/client examples/main.go 36 depth 10 expand all

# scan a repository
$ ./client/client scan <dir>
$ ./client/client scan examples/buggy
```

Analysis is triggered exclusively through the TCP intake on **:1112**, which
accepts requests in the format `file:line[:param:value]...` (the client
builds this string for you and absolutizes the path; `<line>` may be any
line inside the target function's declaration). Each request is analyzed in
the backend and the resulting **structured trace tree** is pushed as JSON to
the React UI on **:1111** over a websocket. New UI connections get the last
50 results replayed.

The UI is a trace inspector: a sidebar of received traces, and a collapsible
tree with the source position of every entry in a left gutter, kind badges
(goroutine launches, loops, channel ops, interface dispatch, …),
local/stdlib/module chips, live text filtering, and expand/collapse-all.
It renders the backend's tree verbatim — no code analysis happens
client-side, the UI only maps backend-provided node kinds to styling.
Received traces and scans are persisted in the browser's localStorage
(last 30, ~4 MB cap) and restored on reload, deduplicated against the
server's history replay; the sidebar's **clear all + storage** button wipes
both the list and the stored copy.

Supported parameters:

| param | values | meaning |
| --- | --- | --- |
| `depth` | positive integer | max call-expansion depth (default 40) |
| `expand` | `once` (default) / `all` | expand each function body once per trace, or at every call site |

## Editor integration

The TCP client is a good fit for an editor keybinding: bind a key to run
`client <current-file> <current-line>` to trace the function under the cursor
(the trace opens in the web UI on :1111), or `client scan <dir>` to scan the
repo. The examples below assume the client is installed as `code-analyzer` on
your `$PATH` (e.g. `go build -o /usr/local/bin/code-analyzer ./client`).

### Helix

In `~/.config/helix/config.toml`, bind keys under `[keys.normal.space]` (or a
namespace of your choice):

```toml
[keys.normal.space]
2 = ":sh /usr/local/bin/code-analyzer %sh{pwd}/%{buffer_name} %{cursor_line}"
1 = ":sh /usr/local/bin/code-analyzer scan %sh{pwd}"
```

`space 2` traces the function under the cursor; `space 1` scans the working
directory. The client absolutizes paths, so the `%sh{pwd}/` prefix keeps
relative buffer names resolvable from Helix's working directory.

### Neovim

In your `init.lua` (Neovim 0.10+, uses async `vim.system`):

```lua
local CLIENT = "/usr/local/bin/code-analyzer"

-- <leader>ca: trace the function under the cursor
vim.keymap.set("n", "<leader>ca", function()
  local file = vim.fn.expand("%:p")   -- absolute path of current buffer
  if file == "" then
    vim.notify("code-analyzer: no file in buffer", vim.log.levels.WARN)
    return
  end
  vim.system({ CLIENT, file, tostring(vim.fn.line(".")) }, { text = true }, function(res)
    local msg = ((res.stdout ~= "" and res.stdout or res.stderr) or ""):gsub("%s+$", "")
    vim.schedule(function()
      vim.notify("code-analyzer: " .. msg,
        res.code == 0 and vim.log.levels.INFO or vim.log.levels.ERROR)
    end)
  end)
end, { desc = "code-analyzer: trace function under cursor" })

-- <leader>cs: scan the current buffer's directory
vim.keymap.set("n", "<leader>cs", function()
  vim.system({ CLIENT, "scan", vim.fn.expand("%:p:h") }, { text = true }, function(res)
    local msg = ((res.stdout ~= "" and res.stdout or res.stderr) or ""):gsub("%s+$", "")
    vim.schedule(function() vim.notify("code-analyzer: " .. msg) end)
  end)
end, { desc = "code-analyzer: scan repo" })
```

Both editors just fire the request at the running server and surface the
one-line ack; the resulting trace/scan renders in the web UI. Set
`CODE_ANALYZER_ADDR` if the server isn't on the default `127.0.0.1:1112`.

## Wire format

Each websocket message is one envelope:

```json
{"type": "trace", "target": "/abs/path/main.go:36", "params": {"depth": "6"},
 "time": "12:36:30", "root": { ...node... }}
{"type": "error", "target": "...", "time": "...", "text": "message"}
```

A node is `{pos, kind, label, num, text, spans, kids}` where `kind` is one of:
`root`, `call`, `interface-call`, `func-value-call`, `indirect-call`,
`impl`, `bound`, `go`, `defer`, `loop` (with `num`), `branch`, `case`,
`select`, `chan-send`, `chan-recv`, `chan-close`, `peer`, `arg`, `note`;
`label` classifies callees as `local`/`stdlib`/`module`.

`spans` splits the node text into segments `{t, v}`: segments with `v != 0`
are variable occurrences, and `v` is the variable's **alias-class ID** —
stable across the whole trace. Two variables connected by argument passing
(including method receivers), assignment, struct-literal field init or
return values share one ID, computed by the backend's union-find. The UI
colors variables by ID and, when one is clicked, highlights every occurrence
and auto-expands each collapsed path the variable propagates through — all
without doing any analysis client-side.

## Repo scanner

`scan:<dir>` loads every Go package under `<dir>` (only `.go` files are
parsed) and runs four whole-repo detectors; results appear in the UI as the
same navigable tree, with per-category counts and evidence rows:

| Finding | Heuristic |
| --- | --- |
| **potential data races** | a variable captured by a `go func(){…}` closure that is written on one side of the goroutine boundary and accessed on the other. Not reported at all (synchronized): accesses after a `wg.Wait()`/channel-receive that joins the goroutine, variables mediated entirely by `sync/atomic` calls, per-iteration loop variables (Go ≥ 1.22), accesses before the launching loop, and header-only reads (`len`/`cap`/keyless `range`) beside element writes. Findings are graded — **RACE** is reserved for races concrete in the current codebase; **RACE WARN** marks theoretical ones with the reason: **index-sharded fan-out** (each instance writes a distinct slice element — the dsync `releaseAll` pattern), **mutex-serialized** accesses (same lock class on both sides; pairing not verified), writes inside **sync.Once.Do**, **branch-exclusive** launch/access (possibly mutually exclusive conditions), or an **unreachable** enclosing function (no callers in this codebase). Concurrent map writes are never sharding-safe and stay RACE. A loop-reused variable (`for v = range …`) whose address escapes each iteration into a call, send or struct literal is flagged **RACE WARN**. `examples/theoretical` is a corpus of Warn/Concrete/Safe specimens exercising every grade. |
| **writes to closed channels** | a send following a `close` of the same channel in one sequential function flow, and channels closed by a function that isn't one of their senders while senders exist elsewhere (closes preceded by `sync.WaitGroup.Wait` count as coordinated). Graded like races — **CLOSED CH** when concrete: reaching the send implies the close ran (branch arms) and, for cross-function cases, both the closer and at least one sender are reachable from the module's entry points; **CLOSED CH WARN** when the close and send sit in possibly mutually exclusive branches or an endpoint has no callers in this codebase. |
| **unclosed file handles** | `*os.File` values bound to a variable whose alias class is never `Close()`d anywhere in the module and never returned to a caller. **FD LEAK WARN** when a `Close` exists on the main path but early returns between the open and the close (or its `defer` registration) skip it — the open's immediate error guard is exempt. |
| **potential goroutine leaks** | goroutines blocking on a channel op with no counterpart anywhere in the module (ops inside multi-case selects are exempt), and goroutines spinning in a call-free infinite loop. Graded: **LEAK** only for purely local channels whose counterpart provably doesn't exist; **LEAK WARN** when the counterpart is merely unverifiable (channel is a parameter, struct field or return value; buffered sends; channels behind pointers; unreachable functions). Externally-owned channels (`ctx.Done()`, `ticker.C`, `time.After`, library-returned, `signal.Notify`) are not leak evidence at all. |

The detectors model real Go synchronization: WaitGroup/pool joins (any
`.Wait()` paired with `Done`/`Give` on the same object), channel joins
(receiving from a channel the goroutine sends on or closes), `sync/atomic`
mediation, mutex regions, `sync.Once`, deferred closes, field-level and
slice-element disjointness, pointer-value vs pointee accesses, and
externally-owned channels (`ctx.Done()`, `time.Ticker.C`, library-returned
or `signal.Notify`-registered). What remains is graded concrete vs
theoretical; findings carry the evidence positions (launch site,
conflicting access, closer, senders) to judge quickly. Scan views have an **export .md** button in the
UI toolbar that downloads the report as structured markdown (categories as
sections, numbered findings with positions, evidence as nested bullets). Scanning is fast enough for large codebases
(~900-file repos in ~4s, ~2300-file repos in ~8s). `examples/buggy`
contains a planted specimen of every finding plus correct variants that
must not be flagged.

## Trace vocabulary

| Node kind | Meaning |
| --- | --- |
| `call` + label | a resolved call; `local` callees are traced into, `stdlib`/`module` are labeled only |
| `loop` (`num`) | calls underneath execute inside a loop (numbered in trace order) |
| `go` | a goroutine is started here |
| `chan-send` / `chan-recv` / `chan-close` | channel operation; `peer` children list the opposite endpoints (writers/readers/closers) found anywhere in the module |
| `interface-call` + `impl` | dynamic dispatch; every implementation in the module is listed and local ones are traced |
| `func-value-call` + `bound` | call through a function-typed variable; the bound literal/function is resolved when statically possible |
| `defer` | deferred call (runs at function exit) |
| `arg` | where a call argument was allocated or produced (`make`, `&T{…}`, parameter, call result, range variable, …); `pos` points at the allocation site |
| `param` | caller→callee name binding for local calls (`dir ← filepath.Dir(absFile)`), so a value stays trackable across the rename at every call boundary |
| `branch` / `case` / `select` | control-flow context — all paths are traced |
| `note` | recursion cut-offs, "body already traced" references (`pos` points at the first expansion), missing peers, depth limits |

## How it works

Simple string matching cannot resolve what `s.Area()` actually calls, so the
analyzer is built on the Go compiler front-end:

- **`golang.org/x/tools/go/packages`** loads the module that owns the target
  file (located via its `go.mod`) with full syntax trees and type information,
  including dependencies' type data.
- **`go/ast`** provides the parsed syntax; a source-order walker visits every
  statement and expression of the target function.
- **`go/types`** resolves every identifier: callees (including generic
  instantiations with inferred type arguments), interface method sets,
  channel-typed expressions and variable definitions.

On top of that sit three module-wide indexes built in a pre-pass:

1. **Value alias classes** — a union-find that connects variables, struct
   fields and channels across argument passing (including receivers),
   returns, plain assignments and struct literal fields. This is how a
   `<-p.jobs` in a worker finds the `jobs <- i` in a producer three
   functions away, and how clicking `ctx` in the UI lights up the same
   context inside every callee it was passed to.
2. **Interface implementations** — every named type in the module is checked
   against the called interface (`types.Implements`), so dynamic dispatch
   sites list all possible concrete targets.
3. **Definition sites** — every variable is mapped to the syntax that defines
   it, so call arguments can be traced back to their allocation
   (`make`/`new`/`&T{}`/literal/parameter/call result).

Recursion is cut off via the call stack (`[recursive — not expanding]`), and
depth/node limits guard against pathological blow-up. Each function body is
expanded once per trace — later call sites print a
`↳ body already traced @ …` reference — so shared helpers don't get
re-printed (and their loops re-numbered) at every call site.

## Limitations

- Channel aliasing is flow-insensitive: channels stored in maps/slices are
  identified by their container, and channels routed through interfaces or
  reflection are reported as `peers unknown`.
- Function values are resolved only when bound to a literal or named function
  at their definition site; callback parameters are reported as
  `depends on caller`.
- Calls into stdlib/external modules are labeled but never entered (by design).

## Examples

`./examples` is a self-contained module (with a fake external dependency in
`./examples/extlib`, wired via `replace`, so `[module]` labels work offline)
covering worker pools, pipelines, select, generics, interface dispatch,
atomics, recursion and function values.

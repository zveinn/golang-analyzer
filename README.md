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
$ ./client/client <file.go> <line> [param value]...
$ ./client/client examples/main.go 36 depth 10 expand all
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

Supported parameters:

| param | values | meaning |
| --- | --- | --- |
| `depth` | positive integer | max call-expansion depth (default 40) |
| `expand` | `once` (default) / `all` | expand each function body once per trace, or at every call site |

## Wire format

Each websocket message is one envelope:

```json
{"type": "trace", "target": "/abs/path/main.go:36", "params": {"depth": "6"},
 "time": "12:36:30", "root": { ...node... }}
{"type": "error", "target": "...", "time": "...", "text": "message"}
```

A node is `{pos, kind, label, num, text, kids}` where `kind` is one of:
`root`, `call`, `interface-call`, `func-value-call`, `indirect-call`,
`impl`, `bound`, `go`, `defer`, `loop` (with `num`), `branch`, `case`,
`select`, `chan-send`, `chan-recv`, `chan-close`, `peer`, `arg`, `note`;
`label` classifies callees as `local`/`stdlib`/`module`.

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

1. **Channel alias classes** — a union-find that connects the two ends of a
   channel across argument passing, returns, plain assignments and struct
   literal fields. This is how a `<-p.jobs` in a worker finds the `jobs <- i`
   in a producer three functions away.
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

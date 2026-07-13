# code-analyzer

Static execution-path tracer for Go. Point it at a function declaration and it
prints every possible execution path as an indented call tree, including
goroutine launches, loops, channel endpoints and where call arguments were
allocated.

## Usage

```
$ go build -o code-analyzer .
$ ./code-analyzer <file.go> <line>
```

`<line>` may be any line inside the target function's declaration.

Positions are printed in a fixed left gutter; the call tree is indented to
the right of it. Annotation lines (`↳ …`, `arg …`) carry the position they
refer to (peer endpoint, allocation site, first expansion).

```
$ ./code-analyzer examples/main.go 36
examples/main.go:36    │ func Run(ctx context.Context, cfg *Config) [local]
examples/main.go:37    │   main.NewPool(cfg.Workers) [local]
examples/main.go:14    │     arg cfg.Workers ← struct field Workers
examples/main.go:40    │   [LOOP 1] for i < cfg.Workers
examples/main.go:42    │     [GOROUTINE LAUNCH] go pool.worker(…)
examples/main.go:42    │       (*main.Pool).worker(ctx, &wg, i) [local]
...
examples/main.go:67    │         select
examples/main.go:68    │             case job, ok := <-p.jobs:
examples/main.go:68    │               <-p.jobs  [CHAN RECV]
examples/main.go:84    │                 ↳ writer: main.produce
examples/main.go:86    │                 ↳ closed by: main.produce
```

## Output vocabulary

| Marker | Meaning |
| --- | --- |
| `[local]` | function defined in the target module — traced into |
| `[stdlib]` | Go standard library — labeled, never traced into |
| `[module]` | external dependency — labeled, never traced into |
| `[LOOP N]` | calls underneath execute inside a loop (numbered in trace order) |
| `[GOROUTINE LAUNCH]` | a goroutine is started here |
| `[CHAN SEND/RECV/CLOSE]` | channel operation; the opposite endpoints (writers/readers/closers) found anywhere in the module are listed under it |
| `[interface method, …]` | dynamic dispatch; every implementation in the module is listed and local ones are traced |
| `[func value]` | call through a function-typed variable; the bound literal/function is resolved when statically possible |
| `[defer]` | deferred call (runs at function exit) |
| `arg x ← …` | where a call argument was allocated or produced (`make`, `&T{…}`, parameter, call result, range variable, …) |
| `if … / else / case …` | branch context — all paths are traced |
| `↳ body already traced @ …` | this function's body was expanded earlier in the trace (at the given call site); it is not re-printed |

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

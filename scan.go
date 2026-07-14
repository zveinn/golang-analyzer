package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// runScan loads every Go package under dir and runs the whole-repo
// detectors: data races, writes to closed channels, unclosed file handles
// and goroutine leaks. Only .go files are parsed (packages.Load considers
// nothing else).
func runScan(dir string, params map[string]string) (*node, error) {
	if len(params) > 0 {
		return nil, fmt.Errorf("scan accepts no parameters")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	a, err := newAnalyzerAt(abs)
	if err != nil {
		return nil, err
	}
	a.cwd = abs // report positions relative to the scanned repo
	return a.scan(abs), nil
}

// finding pairs a result node with its position for deterministic ordering.
type finding struct {
	pos token.Pos
	n   *node
}

func (a *analyzer) scan(base string) *node {
	root := &node{Kind: "root", Text: "scan " + base}
	total := 0
	for _, cat := range []struct {
		title    string
		findings []finding
	}{
		{"potential data races", a.findRaces()},
		{"writes to closed channels", a.findClosedChanWrites()},
		{"unclosed file handles", a.findUnclosedFiles()},
		{"potential goroutine leaks", a.findGoroutineLeaks()},
	} {
		slices.SortFunc(cat.findings, func(x, y finding) int { return int(x.pos) - int(y.pos) })
		c := root.add(&node{Kind: "branch", Text: fmt.Sprintf("%s (%d)", cat.title, len(cat.findings))})
		if len(cat.findings) == 0 {
			c.note("none found")
			continue
		}
		for _, f := range cat.findings {
			c.add(f.n)
		}
		total += len(cat.findings)
	}
	root.Text = fmt.Sprintf("scan %s — %d findings", base, total)
	return root
}

// countFindings counts result nodes for the TCP acknowledgement.
func countFindings(n *node) int {
	c := 0
	switch n.Kind {
	case "race", "race-warn", "chan-closed", "chan-closed-warn", "fd-leak", "fd-leak-warn", "go-leak", "go-leak-warn":
		c++
	}
	for _, k := range n.Kids {
		c += countFindings(k)
	}
	return c
}

// ---------- shared helpers ----------

// fileFor locates the package and file containing pos.
func (a *analyzer) fileFor(pos token.Pos) (*packages.Package, *ast.File) {
	tf := a.fset.File(pos)
	if tf == nil {
		return nil, nil
	}
	for _, p := range a.pkgs {
		for _, f := range p.Syntax {
			if a.fset.File(f.Pos()) == tf {
				return p, f
			}
		}
	}
	return nil, nil
}

// enclosingFuncBody returns the innermost function body containing pos.
// Deferred literals are transparent — they execute in the enclosing
// function's flow.
func enclosingFuncBody(f *ast.File, pos token.Pos) *ast.BlockStmt {
	var body *ast.BlockStmt
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if pos < n.Pos() || pos > n.End() {
			return false
		}
		switch d := n.(type) {
		case *ast.FuncDecl:
			if d.Body != nil {
				body = d.Body
			}
		case *ast.FuncLit:
			if !isDeferredLit(d, stack) {
				body = d.Body
			}
		}
		stack = append(stack, n)
		return true
	})
	return body
}

// walkSameFlow visits nodes of root without descending into function
// literals or go statements (code that runs in a different flow).
func walkSameFlow(root ast.Node, fn func(ast.Node) bool) {
	ast.Inspect(root, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt:
			return false
		}
		return fn(n)
	})
}

type identAcc struct {
	v     *types.Var
	pos   token.Pos
	write bool
	// index is the first index expression applied to the variable
	// (v[i], (*v)[i]); nil for direct accesses.
	index ast.Expr
	// slice reports the indexed container is a slice/array — distinct
	// elements are distinct memory. False for maps (never sharding-safe).
	slice bool
	// lenCap: the access is only len(v)/cap(v) — header-only, safe beside
	// element writes.
	lenCap bool
	// atomic: the access is &v passed to a sync/atomic function.
	atomic bool
	// addrOnly: the access is &v (address taken, not passed to atomics) —
	// it reads/writes no memory of v itself.
	addrOnly bool
	// field is the first field selected on the variable (v.f…); nil for
	// whole-variable accesses. Distinct fields are distinct memory.
	field *types.Var
	// deref: the access goes through the variable (v.f, *v, v[i]) rather
	// than touching the variable's own storage. For pointer variables a
	// plain read (return v, f(v)) copies the pointer and cannot race with
	// writes through it.
	deref bool
	// methodRecv: the variable is used as a method-call receiver — what
	// the method touches is not statically visible.
	methodRecv bool
}

// fieldsCompat reports whether two accesses can touch the same memory:
// whole-variable accesses overlap everything, field accesses only overlap
// the same field, and — for pointer variables — a plain read of the
// pointer value (return v, f(v)) overlaps nothing accessed through it.
func fieldsCompat(a, b identAcc) bool {
	if isPointerVar(a.v) {
		if (!a.deref && !a.write && b.deref) || (!b.deref && !b.write && a.deref) {
			return false // pointer-value copy vs access through the pointer
		}
	}
	return a.field == nil || b.field == nil || a.field == b.field
}

func isPointerVar(v *types.Var) bool {
	if v == nil {
		return false
	}
	_, ok := v.Type().Underlying().(*types.Pointer)
	return ok
}

// collectAccesses records every variable read/write under root, skipping
// the given subtree. With skipGo, nested `go` statements are excluded —
// their accesses belong to their own goroutine (analyzed separately).
func collectAccesses(info *types.Info, root ast.Node, skipNode ast.Node, skipGo bool) []identAcc {
	var out []identAcc
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if n == skipNode {
			return false
		}
		if skipGo {
			if _, ok := n.(*ast.GoStmt); ok {
				return false
			}
		}
		if id, ok := n.(*ast.Ident); ok && id.Name != "_" {
			obj := info.Uses[id]
			if obj == nil {
				obj = info.Defs[id]
			}
			if v, ok := obj.(*types.Var); ok {
				acc := classifyAccess(info, id, stack)
				acc.v = v
				acc.pos = id.Pos()
				out = append(out, acc)
			}
		}
		stack = append(stack, n)
		return true
	})
	return out
}

// classifyAccess walks up from the identifier to determine how the
// variable is accessed: read/write, through which index, via len/cap, or
// mediated by a sync/atomic call.
func classifyAccess(info *types.Info, id *ast.Ident, stack []ast.Node) identAcc {
	var acc identAcc
	var child ast.Node = id
	for i := len(stack) - 1; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.SelectorExpr:
			if p.X != child {
				return acc // id is the field name; the root ident decides
			}
			acc.deref = true
			if sel, ok := info.Selections[p]; ok {
				if acc.field == nil && acc.index == nil && sel.Kind() == types.FieldVal {
					if fv, ok := sel.Obj().(*types.Var); ok {
						acc.field = fv
					}
				}
				if sel.Kind() == types.MethodVal {
					acc.methodRecv = true
				}
			}
			child = p
		case *ast.IndexExpr:
			if p.X != child {
				return acc // id is inside the index expression — a read
			}
			acc.deref = true
			if acc.index == nil {
				acc.index = p.Index
				acc.slice = isSliceOrArray(info.TypeOf(p.X))
			}
			child = p
		case *ast.StarExpr:
			acc.deref = true
			child = p
		case *ast.ParenExpr:
			child = p
		case *ast.UnaryExpr:
			if p.Op == token.AND && p.X == child {
				// &v passed to a sync/atomic function is an atomic write;
				// any other address-of touches no memory of v — counting it
				// as an access produced false evidence for every
				// `return &x` / `f(&x)`.
				if i > 0 {
					if call, ok := stack[i-1].(*ast.CallExpr); ok && isAtomicCall(info, call) {
						acc.atomic = true
						acc.write = true
						return acc
					}
				}
				acc.addrOnly = true
				return acc
			}
			return acc
		case *ast.CallExpr:
			// variable used directly as an argument — len/cap are
			// header-only reads
			if bid, ok := ast.Unparen(p.Fun).(*ast.Ident); ok && (bid.Name == "len" || bid.Name == "cap") {
				if _, isB := info.Uses[bid].(*types.Builtin); isB {
					acc.lenCap = true
				}
			}
			return acc
		case *ast.AssignStmt:
			acc.write = slices.ContainsFunc(p.Lhs, func(l ast.Expr) bool { return l == child })
			return acc
		case *ast.IncDecStmt:
			acc.write = p.X == child
			return acc
		case *ast.RangeStmt:
			if p.X == child && p.Value == nil {
				// `for i := range v` reads only the header — safe beside
				// element writes, like len()
				acc.lenCap = true
				return acc
			}
			acc.write = p.Key == child || p.Value == child
			return acc
		default:
			return acc
		}
	}
	return acc
}

func isSliceOrArray(t types.Type) bool {
	if t == nil {
		return false
	}
	switch deref(t).Underlying().(type) {
	case *types.Slice, *types.Array:
		return true
	}
	return false
}

func isAtomicCall(info *types.Info, call *ast.CallExpr) bool {
	fn, ok := typeutil.Callee(info, call).(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == "sync/atomic"
}

// syncSafeType reports types that are safe (or expected) to share between
// goroutines: channels, sync.* / sync/atomic.* types, contexts.
func syncSafeType(t types.Type) bool {
	if t == nil {
		return true
	}
	for {
		if ptr, ok := t.Underlying().(*types.Pointer); ok {
			t = ptr.Elem()
			continue
		}
		break
	}
	if _, ok := t.Underlying().(*types.Chan); ok {
		return true
	}
	if named, ok := types.Unalias(t).(*types.Named); ok {
		if pkg := named.Obj().Pkg(); pkg != nil {
			switch pkg.Path() {
			case "sync", "sync/atomic", "context":
				return true
			}
		}
	}
	return false
}

func within(pos token.Pos, n ast.Node) bool {
	return n.Pos() <= pos && pos <= n.End()
}

// ---------- 1. potential data races ----------

// findRaces flags variables captured by a goroutine closure that are
// written on one side of the goroutine boundary and accessed on the other,
// with no synchronization type involved.
func (a *analyzer) findRaces() []finding {
	var out []finding
	for _, p := range a.pkgs {
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.GoStmt:
					if lit, ok := ast.Unparen(x.Call.Fun).(*ast.FuncLit); ok {
						out = append(out, a.racesInGoroutine(p, f, x, lit)...)
					}
				case *ast.RangeStmt:
					out = append(out, a.loopVarAddrEscape(p, x)...)
				}
				return true
			})
		}
	}
	return out
}

// addrEscapes reports whether an &v expression (top of stack minus the
// UnaryExpr) flows into a call argument, a channel send, or a composite
// literal — i.e. the pointer can be retained past the iteration. A bare
// `&v` used locally (e.g. immediately dereferenced) does not escape.
func addrEscapes(stack []ast.Node) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		switch n := stack[i].(type) {
		case *ast.ParenExpr, *ast.KeyValueExpr:
			continue // transparent wrappers
		case *ast.CompositeLit:
			continue // struct/slice holding the pointer — keep climbing
		case *ast.CallExpr:
			return n.Fun != stackChild(stack, i) // &v is an argument, not the callee
		case *ast.SendStmt:
			return true
		default:
			return false // hit a statement or other expr — stays local
		}
	}
	return false
}

func stackChild(stack []ast.Node, i int) ast.Node {
	if i+1 < len(stack) {
		return stack[i+1]
	}
	return nil
}

// loopVarAddrEscape flags `for v = range ch { … f(&v) … }`: v is declared
// OUTSIDE the loop (Tok is =, not :=) so every iteration reuses the same
// storage, and its address is handed to a callee — if the receiver holds
// it (UI models, collectors, other goroutines), it observes later
// iterations' overwrites.
func (a *analyzer) loopVarAddrEscape(p *packages.Package, rs *ast.RangeStmt) []finding {
	if rs.Tok != token.ASSIGN || !isChanType(p.TypesInfo.TypeOf(rs.X)) {
		return nil
	}
	keyID, ok := ast.Unparen(rs.Key).(*ast.Ident)
	if !ok {
		return nil
	}
	v, ok := p.TypesInfo.Uses[keyID].(*types.Var)
	if !ok || syncSafeType(v.Type()) {
		return nil
	}
	var out []finding
	var stack []ast.Node
	ast.Inspect(rs.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(out) > 0 {
			return false // one finding per loop
		}
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.AND {
			if id, ok := ast.Unparen(u.X).(*ast.Ident); ok && p.TypesInfo.Uses[id] == types.Object(v) && addrEscapes(stack) {
				spans := []span{{T: "address of loop-reused variable "},
					{T: v.Name(), V: a.varID(v)},
					{T: " escapes on every iteration — the receiver may observe later overwrites"}}
				fn := nodeWithSpans(a.relPos(u.Pos()), "race-warn", "", spans)
				fn.notep(a.relPos(rs.For), "loop reuses the variable (for %s = range …)", v.Name())
				fn.note("declare the variable inside the loop (for %s := range …) or copy it before taking its address", v.Name())
				out = append(out, finding{pos: u.Pos(), n: fn})
			}
		}
		stack = append(stack, n)
		return true
	})
	return out
}

func (a *analyzer) racesInGoroutine(p *packages.Package, f *ast.File, gs *ast.GoStmt, lit *ast.FuncLit) []finding {
	encl := enclosingFuncBody(f, gs.Pos())
	if encl == nil {
		return nil
	}
	loop := enclosingLoop(encl, gs)

	type sides struct{ in, out []identAcc }
	byVar := map[*types.Var]*sides{}
	get := func(v *types.Var) *sides {
		s := byVar[v]
		if s == nil {
			s = &sides{}
			byVar[v] = s
		}
		return s
	}
	for _, acc := range collectAccesses(p.TypesInfo, lit.Body, nil, true) {
		// captured: declared outside the literal; address-of touches
		// nothing
		if acc.addrOnly || !acc.v.Pos().IsValid() || within(acc.v.Pos(), lit) || syncSafeType(acc.v.Type()) {
			continue
		}
		get(acc.v).in = append(get(acc.v).in, acc)
	}
	for _, acc := range collectAccesses(p.TypesInfo, encl, lit, false) {
		if acc.addrOnly {
			continue
		}
		if s, tracked := byVar[acc.v]; tracked {
			// Concurrent with the goroutine: anything after the launch — or
			// inside the launching loop's body (later iterations run beside
			// earlier goroutines) when the variable outlives iterations.
			// Accesses before the loop happen-before every launch, and
			// range/loop-local variables are per-iteration (Go ≥1.22): each
			// goroutine gets its own copy, so only same-iteration accesses
			// after the launch can race.
			perIteration := loop != nil && within(acc.v.Pos(), loop)
			if acc.pos > gs.End() || (loop != nil && !perIteration && within(acc.pos, loop)) {
				s.out = append(s.out, acc)
			}
		}
	}

	gsArms := branchArms(encl, gs.Pos())
	reachable := a.declReachable(p, f, gs.Pos())

	// Join points after which parent accesses are synchronized: wg.Wait()
	// for WaitGroups the goroutine calls Done on, and receives from
	// channels the goroutine sends on or closes.
	doneWGs := a.wgDoneClasses(p, lit)
	signals := a.chanSignalClasses(p, lit)
	joins := a.wgWaitCalls(p, encl)
	for _, r := range a.chanRecvPoints(p, encl) {
		if signals[r.key] {
			joins = append(joins, r)
		}
	}
	syncedAfterLaunch := func(accPos token.Pos) bool {
		for _, w := range joins {
			if w.pos > gs.End() && w.pos < accPos && (doneWGs[w.key] || signals[w.key]) {
				return true
			}
		}
		return false
	}

	// Receives the goroutine blocks on before touching a variable gate its
	// accesses on an external signal.
	var litRecvs []token.Pos
	walkSameFlow(lit.Body, func(n ast.Node) bool {
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.ARROW {
			litRecvs = append(litRecvs, u.OpPos)
		}
		return true
	})
	recvGated := func(pos token.Pos) bool {
		for _, r := range litRecvs {
			if r < pos {
				return true
			}
		}
		return false
	}

	var out []finding
	for v, s := range byVar {
		wIn := firstRealWrite(s.in)

		// A goroutine launched in a loop that writes the variable (not via
		// sync/atomic) races its own instances — concrete no matter what
		// the parent does.
		multiInstance := wIn != nil && loop != nil && !within(v.Pos(), loop)

		// Conflicting outside accesses: pair-wise — same memory (field
		// compatibility), at least one side writes, not both atomic,
		// address-of touches nothing.
		conflicting := func(out identAcc) bool {
			if out.addrOnly {
				return false
			}
			for _, in := range s.in {
				if !fieldsCompat(in, out) {
					continue
				}
				if !in.write && !out.write {
					continue
				}
				if in.atomic && out.atomic {
					continue
				}
				return true
			}
			return false
		}
		insideWritesIndexed := wIn != nil
		for _, acc := range s.in {
			if acc.write && acc.index == nil && !acc.atomic {
				insideWritesIndexed = false
				break
			}
		}
		var live []identAcc
		for _, c := range s.out {
			if !conflicting(c) || syncedAfterLaunch(c.pos) {
				continue
			}
			// header-only reads (len/cap/keyless range) don't conflict with
			// element writes
			if c.lenCap && insideWritesIndexed {
				continue
			}
			live = append(live, c)
		}
		if !multiInstance && len(live) == 0 {
			continue // no unsynchronized counterpart — not a race
		}

		// An access is CONFIRMED concurrent if — given the goroutine was
		// launched — it executes unconditionally: every branch arm guarding
		// it also guards the launch. Accesses guarded by other branches
		// (possibly mutually exclusive with the launch, e.g. the else of a
		// condition correlated with spawning) only race if the branch
		// conditions can coincide.
		var conflict *identAcc
		confirmed := false
		for pass := 0; pass < 2 && conflict == nil; pass++ {
			for i := range live {
				if pass == 0 && live[i].pos <= gs.End() {
					continue // prefer evidence after the launch
				}
				if armsSubset(branchArms(encl, live[i].pos), gsArms) {
					conflict = &live[i]
					confirmed = true
					break
				}
			}
		}
		if conflict == nil && len(live) > 0 {
			conflict = &live[0]
			for i := range live {
				if live[i].pos > gs.End() {
					conflict = &live[i]
					break
				}
			}
		}

		// Grade: RACE only when the race is concrete in the current
		// codebase — everything theoretical is RACE WARN with the reason.
		concrete := false
		var reasons []string

		if multiInstance {
			switch {
			case shardedAccesses(p.TypesInfo, s.in, lit, loop):
				reasons = append(reasons,
					"index-sharded fan-out: each goroutine instance accesses a distinct slice element — disjoint elements do not race")
			case onceGuardedWrites(p, lit, s.in):
				reasons = append(reasons,
					"the goroutine's writes are inside sync.Once.Do — they execute at most once")
			case len(a.mutexHeldAt(p, lit.Body, wIn.pos)) > 0:
				reasons = append(reasons,
					"the goroutine's writes appear serialized by a mutex (lock pairing not verified)")
			default:
				concrete = true
			}
		}
		if !concrete && conflict != nil {
			allGated := true
			for _, in := range s.in {
				if fieldsCompat(in, *conflict) && !recvGated(in.pos) {
					allGated = false
					break
				}
			}
			// element-vs-element: both sides index the slice — overlap not
			// provable either way
			bothIndexed := conflict.index != nil && conflict.slice
			if bothIndexed {
				for _, in := range s.in {
					if !fieldsCompat(in, *conflict) || (!in.write && !conflict.write) || in.addrOnly || in.lenCap || in.atomic {
						continue
					}
					if in.index == nil || !in.slice {
						bothIndexed = false
						break
					}
				}
			}
			switch {
			case !confirmed:
				reasons = append(reasons,
					"the branches guarding the launch and this access may be mutually exclusive — racy only if a code change lets them overlap")
			case allGated:
				reasons = append(reasons,
					"the goroutine blocks on a channel receive before touching the variable — ordering depends on when that signal fires")
			case bothIndexed:
				reasons = append(reasons,
					"both sides access slice elements at different indices — racy only if the index ranges overlap")
			default:
				// use each access's innermost function body — the
				// conflicting access may live in a sibling goroutine
				inPos := s.in[0].pos
				if wIn != nil {
					inPos = wIn.pos
				}
				inBody := enclosingFuncBody(f, inPos)
				outBody := enclosingFuncBody(f, conflict.pos)
				if inBody == nil {
					inBody = lit.Body
				}
				if outBody == nil {
					outBody = encl
				}
				inGuards := a.mutexHeldAt(p, inBody, inPos)
				if intersects(inGuards, a.mutexHeldAt(p, outBody, conflict.pos)) {
					reasons = append(reasons,
						"both accesses appear guarded by the same mutex (lock pairing not verified)")
				} else {
					concrete = true
				}
			}
		}
		if !reachable {
			concrete = false
			reasons = append(reasons,
				"the enclosing function has no callers in this codebase — the race needs new calling code to occur")
		}
		kind := "race"
		if concrete {
			reasons = nil // downgrade reasons don't apply to a concrete race
		} else {
			kind = "race-warn"
		}

		suffix := " — captured by goroutine without synchronization"
		if kind == "race-warn" {
			suffix = " — captured by goroutine; theoretical in the current codebase"
		}
		spans := []span{{T: "potential data race on "}, {T: v.Name(), V: a.varID(v)}, {T: suffix}}
		n := nodeWithSpans(a.relPos(s.in[0].pos), kind, "", spans)
		n.notep(a.relPos(gs.Pos()), "goroutine launched here")
		if multiInstance {
			n.notep(a.relPos(loop.Pos()), "launched inside a loop — multiple goroutine instances access the variable concurrently")
		}
		if wIn != nil {
			n.notep(a.relPos(wIn.pos), "written inside the goroutine")
		}
		if conflict != nil {
			n.notep(a.relPos(conflict.pos), "%s outside the goroutine", accWord(conflict))
		}
		for _, r := range reasons {
			n.note("%s", r)
		}
		out = append(out, finding{pos: s.in[0].pos, n: n})
	}
	return out
}

// branchArms returns the branch arms (if/else blocks, switch/select cases)
// enclosing pos within root. Loops are not arms: their bodies are treated
// as executing.
func branchArms(root ast.Node, pos token.Pos) map[ast.Node]bool {
	var best, stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if pos < n.Pos() || pos > n.End() {
			return false
		}
		stack = append(stack, n)
		if len(stack) > len(best) {
			best = append(best[:0], stack...)
		}
		return true
	})
	arms := map[ast.Node]bool{}
	for i, n := range best {
		switch x := n.(type) {
		case *ast.CaseClause, *ast.CommClause:
			arms[n] = true
		case *ast.IfStmt:
			if i+1 < len(best) {
				child := best[i+1]
				if child == ast.Node(x.Body) || (x.Else != nil && child == x.Else) {
					arms[child] = true
				}
			}
		}
	}
	return arms
}

// armsSubset reports whether every arm in a also encloses b's position set.
func armsSubset(a, b map[ast.Node]bool) bool {
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// firstRealWrite returns the first non-atomic write.
func firstRealWrite(accs []identAcc) *identAcc {
	for i := range accs {
		if accs[i].write && !accs[i].atomic {
			return &accs[i]
		}
	}
	return nil
}

func accWord(acc *identAcc) string {
	if acc.write {
		return "written"
	}
	return "read"
}

// enclosingLoop returns the innermost for/range statement between root and
// target, or nil.
func enclosingLoop(root ast.Node, target ast.Node) ast.Node {
	var loop ast.Node
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if n == target {
			for _, s := range stack {
				switch s.(type) {
				case *ast.ForStmt, *ast.RangeStmt:
					loop = s
				}
			}
			return false
		}
		stack = append(stack, n)
		return true
	})
	return loop
}

// ---------- reachability ----------

// buildReachability computes the set of local functions reachable from the
// module's entry points. Roots: main, init, every exported function, every
// method (they may be invoked through interfaces), and functions referenced
// from package-level declarations. Unexported plain functions must be
// referenced (called or used as a value) from a reachable function.
func (a *analyzer) buildReachability() {
	edges := map[*types.Func][]*types.Func{}
	a.callersOf = map[*types.Func]map[*types.Func]bool{}
	var roots []*types.Func

	for fn, def := range a.funcs {
		fd := def.decl
		if fd.Recv != nil || fd.Name.IsExported() || fd.Name.Name == "main" || fd.Name.Name == "init" {
			roots = append(roots, fn)
		}
		if fd.Body == nil {
			continue
		}
		info := def.pkg.TypesInfo
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if callee, ok := info.Uses[id].(*types.Func); ok {
					if _, local := a.funcs[callee.Origin()]; local {
						edges[fn] = append(edges[fn], callee.Origin())
						if a.callersOf[callee.Origin()] == nil {
							a.callersOf[callee.Origin()] = map[*types.Func]bool{}
						}
						a.callersOf[callee.Origin()][fn] = true
					}
				}
			}
			return true
		})
	}

	// package-level declarations referencing functions (var handler = fn)
	for _, p := range a.pkgs {
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				ast.Inspect(gd, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok {
						if fn, ok := p.TypesInfo.Uses[id].(*types.Func); ok {
							if _, local := a.funcs[fn.Origin()]; local {
								roots = append(roots, fn.Origin())
							}
						}
					}
					return true
				})
			}
		}
	}

	reach := map[*types.Func]bool{}
	queue := roots
	for len(queue) > 0 {
		fn := queue[0]
		queue = queue[1:]
		if reach[fn] {
			continue
		}
		reach[fn] = true
		queue = append(queue, edges[fn]...)
	}
	a.reachableFns = reach
}

// declReachable reports whether the function declaration enclosing pos is
// reachable from the module's entry points.
func (a *analyzer) declReachable(p *packages.Package, f *ast.File, pos token.Pos) bool {
	if a.reachableFns == nil {
		a.buildReachability()
	}
	fd := enclosingFuncDecl(f, pos)
	if fd == nil {
		return true // package-level initializer — runs at import
	}
	fn, ok := p.TypesInfo.Defs[fd.Name].(*types.Func)
	if !ok {
		return true
	}
	return a.reachableFns[fn.Origin()]
}

func enclosingFuncDecl(f *ast.File, pos token.Pos) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Pos() <= pos && pos <= fd.End() {
			return fd
		}
	}
	return nil
}

// ---------- sync.WaitGroup helpers ----------

func isWaitGroupType(t types.Type) bool {
	if t == nil {
		return false
	}
	named, ok := types.Unalias(deref(t)).(*types.Named)
	if !ok {
		return false
	}
	pkg := named.Obj().Pkg()
	return pkg != nil && pkg.Path() == "sync" && named.Obj().Name() == "WaitGroup"
}

// wgDoneClasses returns the alias classes of join objects this goroutine
// signals completion on: X.Done() or X.Give() on any type (sync.WaitGroup,
// errgroup-style groups, worker pools), including in defers and nested
// literals.
func (a *analyzer) wgDoneClasses(p *packages.Package, lit *ast.FuncLit) map[any]bool {
	out := map[any]bool{}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Done" && sel.Sel.Name != "Give") {
			return true
		}
		if k := varRootObj(p.TypesInfo, sel.X); k != nil {
			out[a.find(k)] = true
		}
		return true
	})
	return out
}

type wgWait struct {
	key any
	pos token.Pos
}

// wgWaitCalls collects non-deferred X.Wait() calls on any receiver type in
// the same flow as body (deferred Waits run at function exit and don't
// order in-body accesses). Wait/Done|Give pairing is matched by alias
// class, so worker pools and errgroup-style types join like WaitGroups.
func (a *analyzer) wgWaitCalls(p *packages.Package, body *ast.BlockStmt) []wgWait {
	var out []wgWait
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt:
			return false
		case *ast.CallExpr:
			if len(x.Args) != 0 {
				return true
			}
			sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Wait" {
				return true
			}
			if k := varRootObj(p.TypesInfo, sel.X); k != nil {
				out = append(out, wgWait{key: a.find(k), pos: x.Pos()})
			}
		}
		return true
	})
	return out
}

// chanEscapes reports whether a channel's identity extends beyond what the
// analysis can see: rooted in (or aliased to) a function parameter or
// struct field, or returned from a local function — callers own the other
// end.
func (a *analyzer) chanEscapes(key any) bool {
	if a.escapingChanObj(key) {
		return true
	}
	if a.escapeRoots == nil {
		a.escapeRoots = map[any]bool{}
		mark := func(k any) {
			if a.escapingChanObj(k) {
				a.escapeRoots[a.find(k)] = true
			}
			switch o := k.(type) {
			case resultKey:
				// returned from a local function (callers own the other
				// end) or produced by a non-local call (library-owned)
				a.escapeRoots[a.find(k)] = true
			case types.Object:
				if pkg := o.Pkg(); pkg != nil && a.classify(pkg) != "local" {
					a.escapeRoots[a.find(k)] = true
				}
			}
		}
		// both sides of the union-find: values may be keys never seen on
		// the left (e.g. resultKeys unioned as roots)
		for k, v := range a.aliasParent {
			mark(k)
			mark(v)
		}
		for _, ek := range a.extChanKeys {
			a.escapeRoots[a.find(ek)] = true
		}
		for _, ek := range a.escapedChanKeys {
			a.escapeRoots[a.find(ek)] = true
		}
	}
	return a.escapeRoots[a.find(key)]
}

// escapingChanObj reports a channel-typed variable that is a struct field
// or a function parameter.
func (a *analyzer) escapingChanObj(key any) bool {
	v, ok := key.(*types.Var)
	if !ok || !isChanType(v.Type()) {
		return false
	}
	if v.IsField() {
		return true
	}
	if site, ok := a.defs[v]; ok {
		if _, isParam := site.node.(*ast.Field); isParam {
			return true
		}
	}
	return false
}

// chanBuffered reports whether the channel was created with a nonzero
// capacity.
func (a *analyzer) chanBuffered(key any) bool {
	v, ok := key.(types.Object)
	if !ok {
		return false
	}
	site, found := a.defs[v]
	if !found {
		return false
	}
	buffered := false
	ast.Inspect(site.node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && id.Name == "make" {
			if lit, isLit := ast.Unparen(call.Args[1]).(*ast.BasicLit); !isLit || lit.Value != "0" {
				buffered = true
			}
		}
		return true
	})
	return buffered
}

// externalChanKey reports channels whose other end lives outside the
// module: produced by a non-local call (ctx.Done(), time.After, library
// streams), stored in a non-local type's field (time.Ticker.C), or handed
// to non-local code (signal.Notify).
func (a *analyzer) externalChanKey(key any) bool {
	switch k := key.(type) {
	case resultKey:
		if _, local := a.funcs[k.fn]; !local {
			return true
		}
	case types.Object:
		if pkg := k.Pkg(); pkg != nil && a.classify(pkg) != "local" {
			return true
		}
	}
	root := a.find(key)
	for _, ek := range a.extChanKeys {
		if a.find(ek) == root {
			return true
		}
	}
	return false
}

// ---------- theoretical-race guards ----------

// indexPerInstance reports whether the index expression involves a
// variable private to this goroutine instance: declared inside the literal
// (a parameter or local) or inside the launching loop (per-iteration in
// Go ≥1.22).
func indexPerInstance(info *types.Info, index ast.Expr, lit *ast.FuncLit, loop ast.Node) bool {
	per := false
	ast.Inspect(index, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		obj := info.Uses[id]
		if obj == nil {
			obj = info.Defs[id]
		}
		if v, ok := obj.(*types.Var); ok && v.Pos().IsValid() {
			if within(v.Pos(), lit) || (loop != nil && within(v.Pos(), loop)) {
				per = true
			}
		}
		return true
	})
	return per
}

// shardedAccesses reports whether the goroutine's writes to the variable
// are slice/array element writes at per-instance indices — the
// fan-out-by-index pattern where distinct instances touch distinct memory.
// Reads must not overlap the written region either: element reads at
// per-instance indices, len/cap, and reads of other (unwritten) fields are
// fine.
func shardedAccesses(info *types.Info, accs []identAcc, lit *ast.FuncLit, loop ast.Node) bool {
	writtenFields := map[*types.Var]bool{}
	wholeFieldWritten := false
	sawWrite := false
	for _, acc := range accs {
		if !acc.write || acc.atomic {
			continue
		}
		if acc.index == nil || !acc.slice || !indexPerInstance(info, acc.index, lit, loop) {
			return false // non-sharded write (incl. maps — never safe)
		}
		sawWrite = true
		if acc.field == nil {
			wholeFieldWritten = true
		} else {
			writtenFields[acc.field] = true
		}
	}
	if !sawWrite {
		return false
	}
	for _, acc := range accs {
		if acc.write || acc.atomic || acc.lenCap || acc.addrOnly || acc.methodRecv {
			continue
		}
		// reads of fields nobody writes can't overlap the sharded writes
		if acc.field != nil && !wholeFieldWritten && !writtenFields[acc.field] {
			continue
		}
		if acc.index == nil || !acc.slice || !indexPerInstance(info, acc.index, lit, loop) {
			return false
		}
	}
	return true
}

func isMutexType(t types.Type) bool {
	named, ok := types.Unalias(deref(t)).(*types.Named)
	if !ok {
		return false
	}
	pkg := named.Obj().Pkg()
	if pkg == nil || pkg.Path() != "sync" {
		return false
	}
	name := named.Obj().Name()
	return name == "Mutex" || name == "RWMutex"
}

// mutexHeldAt approximates the set of mutex alias classes held at pos
// within body: a (R)Lock call earlier in the same flow, released by a
// deferred (R)Unlock or an (R)Unlock after pos.
func (a *analyzer) mutexHeldAt(p *packages.Package, body *ast.BlockStmt, pos token.Pos) map[any]bool {
	type mcall struct {
		key      any
		pos      token.Pos
		lock     bool
		deferred bool
	}
	var calls []mcall
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch x := n.(type) {
		case *ast.GoStmt:
			return false
		case *ast.FuncLit:
			// deferred literals run in this goroutine at exit — their
			// locks guard their accesses; other literals are foreign flow
			if !isDeferredLit(x, stack) {
				return false
			}
		}
		if call, ok := n.(*ast.CallExpr); ok && len(call.Args) == 0 {
			if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && isMutexType(p.TypesInfo.TypeOf(sel.X)) {
				name := sel.Sel.Name
				if name == "Lock" || name == "RLock" || name == "Unlock" || name == "RUnlock" {
					if k := varRootObj(p.TypesInfo, sel.X); k != nil {
						// deferred means the CALL itself is deferred
						// (defer mu.Unlock()); calls inside a deferred
						// literal's body execute as normal statements when
						// the literal runs
						deferred := false
						for i := len(stack) - 1; i >= 0; i-- {
							if _, ok := stack[i].(*ast.FuncLit); ok {
								break
							}
							if _, ok := stack[i].(*ast.DeferStmt); ok {
								deferred = true
								break
							}
						}
						calls = append(calls, mcall{
							key:      a.find(k),
							pos:      call.Pos(),
							lock:     name == "Lock" || name == "RLock",
							deferred: deferred,
						})
					}
				}
			}
		}
		stack = append(stack, n)
		return true
	})
	held := map[any]bool{}
	for _, lk := range calls {
		if !lk.lock || lk.deferred || lk.pos > pos {
			continue
		}
		for _, ul := range calls {
			if !ul.lock && ul.key == lk.key && (ul.deferred || ul.pos > pos) {
				held[lk.key] = true
				break
			}
		}
	}
	return held
}

func intersects(a, b map[any]bool) bool {
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}

// onceGuardedWrites reports whether every write the goroutine makes to the
// variable happens inside a sync.Once.Do function literal.
func onceGuardedWrites(p *packages.Package, lit *ast.FuncLit, accs []identAcc) bool {
	var regions [][2]token.Pos
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Do" {
			return true
		}
		if named, ok := types.Unalias(deref(p.TypesInfo.TypeOf(sel.X))).(*types.Named); ok {
			if pkg := named.Obj().Pkg(); pkg != nil && pkg.Path() == "sync" && named.Obj().Name() == "Once" {
				if fl, ok := ast.Unparen(call.Args[0]).(*ast.FuncLit); ok {
					regions = append(regions, [2]token.Pos{fl.Pos(), fl.End()})
				}
			}
		}
		return true
	})
	sawWrite := false
	for _, acc := range accs {
		if !acc.write {
			continue
		}
		sawWrite = true
		inRegion := false
		for _, r := range regions {
			if acc.pos >= r[0] && acc.pos <= r[1] {
				inRegion = true
				break
			}
		}
		if !inRegion {
			return false
		}
	}
	return sawWrite
}

// chanSignalClasses returns the alias classes of channels this goroutine
// sends on or closes — receiving from them joins the goroutine's writes.
func (a *analyzer) chanSignalClasses(p *packages.Package, lit *ast.FuncLit) map[any]bool {
	out := map[any]bool{}
	add := func(e ast.Expr) {
		if k := a.chanKey(p.TypesInfo, e); k != nil {
			out[a.find(k)] = true
		}
	}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SendStmt:
			add(x.Chan)
		case *ast.CallExpr:
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && id.Name == "close" && len(x.Args) == 1 {
				if _, isB := p.TypesInfo.Uses[id].(*types.Builtin); isB {
					add(x.Args[0])
				}
			}
		}
		return true
	})
	return out
}

// chanRecvPoints collects channel receives in the same flow as body.
func (a *analyzer) chanRecvPoints(p *packages.Package, body *ast.BlockStmt) []wgWait {
	var out []wgWait
	walkSameFlow(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.UnaryExpr:
			if x.Op == token.ARROW {
				if k := a.chanKey(p.TypesInfo, x.X); k != nil {
					out = append(out, wgWait{key: a.find(k), pos: x.OpPos})
				}
			}
		case *ast.RangeStmt:
			if isChanType(p.TypesInfo.TypeOf(x.X)) {
				if k := a.chanKey(p.TypesInfo, x.X); k != nil {
					out = append(out, wgWait{key: a.find(k), pos: x.For})
				}
			}
		}
		return true
	})
	return out
}

// ---------- 2. writes to closed channels ----------

func (a *analyzer) findClosedChanWrites() []finding {
	var out []finding
	out = append(out, a.sendsAfterCloseInFlow()...)
	out = append(out, a.multiSenderCloses()...)
	return out
}

// sendsAfterCloseInFlow flags a send that follows a close of the same
// channel within one sequential function flow.
func (a *analyzer) sendsAfterCloseInFlow() []finding {
	var out []finding
	for _, p := range a.pkgs {
		info := p.TypesInfo
		for _, f := range p.Syntax {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				type opRec struct {
					key any
					pos token.Pos
					ch  ast.Expr
				}
				var closes, sends []opRec
				var stack []ast.Node
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					if n == nil {
						stack = stack[:len(stack)-1]
						return true
					}
					switch n.(type) {
					case *ast.FuncLit, *ast.GoStmt:
						return false
					}
					switch x := n.(type) {
					case *ast.SendStmt:
						sends = append(sends, opRec{a.chanKey(info, x.Chan), x.Arrow, x.Chan})
					case *ast.CallExpr:
						if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && id.Name == "close" && len(x.Args) == 1 {
							if _, isB := info.Uses[id].(*types.Builtin); isB {
								// `defer close(ch)` runs at function exit —
								// after every send in the body, regardless
								// of source position
								deferred := false
								for _, s := range stack {
									if _, ok := s.(*ast.DeferStmt); ok {
										deferred = true
									}
								}
								if !deferred {
									closes = append(closes, opRec{a.chanKey(info, x.Args[0]), x.Pos(), x.Args[0]})
								}
							}
						}
					}
					stack = append(stack, n)
					return true
				})
				for _, s := range sends {
					if s.key == nil {
						continue
					}
					for _, c := range closes {
						if c.key != nil && c.pos < s.pos && a.find(c.key) == a.find(s.key) {
							// Concrete only if reaching the send implies the
							// close ran (every branch arm guarding the close
							// also guards the send) and the function has
							// callers.
							kind := "chan-closed"
							var reasons []string
							if !armsSubset(branchArms(fd.Body, c.pos), branchArms(fd.Body, s.pos)) {
								kind = "chan-closed-warn"
								reasons = append(reasons,
									"the close and this send are in different branches — they may be mutually exclusive")
							}
							if !a.declReachable(p, f, s.pos) {
								kind = "chan-closed-warn"
								reasons = append(reasons,
									"the enclosing function has no callers in this codebase — needs new calling code to occur")
							}
							spans := append([]span{{T: "send on closed channel: "}}, a.exprSpans(p, s.ch)...)
							spans = append(spans, span{T: " is closed earlier in this function"})
							if kind == "chan-closed-warn" {
								spans = append(spans, span{T: " — theoretical in the current codebase"})
							}
							n := nodeWithSpans(a.relPos(s.pos), kind, "", truncateSpans(spans, 110))
							n.notep(a.relPos(c.pos), "closed here")
							for _, r := range reasons {
								n.note("%s", r)
							}
							out = append(out, finding{pos: s.pos, n: n})
							break
						}
					}
				}
			}
		}
	}
	return out
}

// multiSenderCloses flags a channel closed by a function that is not one of
// its senders, while senders exist elsewhere — a send racing the close will
// panic. Closes preceded by a sync.WaitGroup.Wait() are considered
// coordinated and skipped.
func (a *analyzer) multiSenderCloses() []finding {
	groups := map[any][]chanOp{}
	for _, op := range a.chanOps {
		if op.key != nil {
			root := a.find(op.key)
			groups[root] = append(groups[root], op)
		}
	}
	var out []finding
	for _, ops := range groups {
		var closes, sends []chanOp
		closerFns := map[token.Pos]bool{}
		for _, op := range ops {
			switch op.kind {
			case chanClose:
				closes = append(closes, op)
				closerFns[op.fnPos] = true
			case chanSend:
				sends = append(sends, op)
			}
		}
		if len(closes) == 0 {
			continue
		}
		var rogue []chanOp
		for _, s := range sends {
			if !closerFns[s.fnPos] {
				rogue = append(rogue, s)
			}
		}
		if len(rogue) == 0 {
			continue
		}
		coordinated := true
		for _, c := range closes {
			if !a.waitGroupWaitBefore(c.pos) {
				coordinated = false
				break
			}
		}
		if coordinated {
			continue
		}
		c := closes[0]

		// Concrete only if both ends can actually run concurrently: the
		// closing function and at least one rogue sender's function are
		// reachable, the senders aren't merely synchronous callees of the
		// closing function, and the sends aren't recover-guarded.
		kind := "chan-closed"
		var reasons []string
		closerReachable := a.posReachable(c.pos)
		senderReachable := false
		for _, s := range rogue {
			if a.posReachable(s.pos) {
				senderReachable = true
				break
			}
		}
		if !closerReachable {
			kind = "chan-closed-warn"
			reasons = append(reasons, "the closing function has no callers in this codebase")
		}
		if !senderReachable {
			kind = "chan-closed-warn"
			reasons = append(reasons, "none of the sending functions have callers in this codebase")
		}
		if kind == "chan-closed" {
			closerFn := a.declFnAt(c.pos)
			allSequential := closerFn != nil
			allRecovered := true
			for _, s := range rogue {
				// A sender inside a GO-LAUNCHED literal is concurrent with
				// the closer by construction. Senders in other literals
				// (closures invoked synchronously, walk callbacks) run in
				// their declaration's flow — the sequential rule applies.
				if a.opInGoLit(s) || !a.onlyReachedFrom(a.declFnAt(s.pos), closerFn, map[*types.Func]bool{}) {
					allSequential = false
				}
				if !a.recoverGuarded(s.pos) {
					allRecovered = false
				}
			}
			if allSequential {
				kind = "chan-closed-warn"
				reasons = append(reasons,
					"every sender is invoked only from the closing function's own call tree — likely sequential with the close, not concurrent")
			} else if allRecovered {
				kind = "chan-closed-warn"
				reasons = append(reasons,
					"the sends are recover()-guarded — a send on the closed channel is a handled path")
			}
		}

		text := fmt.Sprintf("channel closed in %s while %d sender(s) elsewhere may still write to it", c.fn, len(rogue))
		if kind == "chan-closed-warn" {
			text += " — theoretical in the current codebase"
		}
		n := &node{Pos: a.relPos(c.pos), Kind: kind, Text: text}
		for i, s := range rogue {
			if i == 5 {
				n.note("… and %d more senders", len(rogue)-5)
				break
			}
			n.notep(a.relPos(s.pos), "sender: %s", s.fn)
		}
		for _, r := range reasons {
			n.note("%s", r)
		}
		out = append(out, finding{pos: c.pos, n: n})
	}
	return out
}

// posReachable reports whether the function declaration containing pos is
// reachable from the module's entry points.
func (a *analyzer) posReachable(pos token.Pos) bool {
	p, f := a.fileFor(pos)
	if f == nil {
		return true
	}
	return a.declReachable(p, f, pos)
}

// opInGoLit reports whether a channel op's innermost enclosing function is
// a literal that is launched as a goroutine (go func(){…}()) — such ops
// run concurrently with their declaration's flow. Literals invoked
// synchronously (callbacks, closures) do not count.
func (a *analyzer) opInGoLit(op chanOp) bool {
	_, f := a.fileFor(op.pos)
	if f == nil {
		return false
	}
	fd := enclosingFuncDecl(f, op.pos)
	if fd != nil && fd.Pos() == op.fnPos {
		return false // op attributed to the named declaration
	}
	launched := false
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if op.fnPos < n.Pos() || op.fnPos > n.End() {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok && lit.Pos() == op.fnPos && len(stack) >= 2 {
			if call, ok := stack[len(stack)-1].(*ast.CallExpr); ok && ast.Unparen(call.Fun) == ast.Expr(lit) {
				if _, ok := stack[len(stack)-2].(*ast.GoStmt); ok {
					launched = true
				}
			}
		}
		stack = append(stack, n)
		return true
	})
	return launched
}

// declFnAt returns the named function whose declaration contains pos.
func (a *analyzer) declFnAt(pos token.Pos) *types.Func {
	p, f := a.fileFor(pos)
	if f == nil {
		return nil
	}
	fd := enclosingFuncDecl(f, pos)
	if fd == nil {
		return nil
	}
	if fn, ok := p.TypesInfo.Defs[fd.Name].(*types.Func); ok {
		return fn.Origin()
	}
	return nil
}

// onlyReachedFrom reports whether fn is invoked exclusively from root's
// call tree — i.e. every chain of callers of fn ends at root. Such a
// "sender" runs synchronously within the closer's flow.
func (a *analyzer) onlyReachedFrom(fn, root *types.Func, seen map[*types.Func]bool) bool {
	if fn == nil || root == nil {
		return false
	}
	if fn == root {
		return true
	}
	if a.reachableFns == nil {
		a.buildReachability()
	}
	callers := a.callersOf[fn]
	if len(callers) == 0 {
		return false
	}
	seen[fn] = true
	for c := range callers {
		if seen[c] {
			continue // recursion — don't recurse forever
		}
		if !a.onlyReachedFrom(c, root, seen) {
			return false
		}
	}
	return true
}

// recoverGuarded reports whether the function enclosing pos contains a
// recover() call — panics (like send on closed channel) are handled.
func (a *analyzer) recoverGuarded(pos token.Pos) bool {
	p, f := a.fileFor(pos)
	if f == nil {
		return false
	}
	body := enclosingFuncBody(f, pos)
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && id.Name == "recover" {
				if _, isB := p.TypesInfo.Uses[id].(*types.Builtin); isB {
					found = true
				}
			}
		}
		return true
	})
	return found
}

// waitGroupWaitBefore reports whether the function enclosing pos calls a
// zero-arg X.Wait() before pos (sync.WaitGroup, errgroup, worker pools) —
// directly, or through one level of local helper (p.stopAndWait()) — the
// usual close coordination.
func (a *analyzer) waitGroupWaitBefore(pos token.Pos) bool {
	p, f := a.fileFor(pos)
	if f == nil {
		return false
	}
	body := enclosingFuncBody(f, pos)
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() > pos {
			return true
		}
		if len(call.Args) == 0 {
			if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "Wait" {
				found = true
				return true
			}
		}
		if fn, ok := typeutil.Callee(p.TypesInfo, call).(*types.Func); ok && a.fnContainsWait(fn.Origin()) {
			found = true
		}
		return true
	})
	return found
}

// fnContainsWait reports whether a local function's own flow calls a
// zero-arg .Wait(); memoized.
func (a *analyzer) fnContainsWait(fn *types.Func) bool {
	if a.waitInside == nil {
		a.waitInside = map[*types.Func]bool{}
	}
	if v, ok := a.waitInside[fn]; ok {
		return v
	}
	a.waitInside[fn] = false // cycle guard
	def, ok := a.funcs[fn]
	if !ok || def.decl.Body == nil {
		return false
	}
	found := false
	walkSameFlow(def.decl.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && len(call.Args) == 0 {
			if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "Wait" {
				found = true
			}
		}
		return !found
	})
	a.waitInside[fn] = found
	return found
}

func deref(t types.Type) types.Type {
	if ptr, ok := t.Underlying().(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// ---------- 3. unclosed file handles ----------

// findUnclosedFiles flags *os.File values that are bound to a variable
// whose alias class is never Close()d anywhere in the module and never
// returned to a caller.
func (a *analyzer) findUnclosedFiles() []finding {
	// every alias class with a .Close() call on it
	closed := map[any]bool{}
	for _, p := range a.pkgs {
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 0 {
					return true
				}
				if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "Close" {
					if k := varRootObj(p.TypesInfo, sel.X); k != nil {
						closed[a.find(k)] = true
					}
				}
				return true
			})
		}
	}

	var out []finding
	for _, p := range a.pkgs {
		info := p.TypesInfo
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok || len(as.Rhs) != 1 {
					return true
				}
				call, ok := ast.Unparen(as.Rhs[0]).(*ast.CallExpr)
				if !ok || !returnsOSFile(info, call) {
					return true
				}
				id, ok := ast.Unparen(as.Lhs[0]).(*ast.Ident)
				if !ok || id.Name == "_" {
					return true
				}
				v := varRootObj(info, id)
				if v == nil || a.escapesViaReturn(f, as.Pos(), v) {
					return true
				}
				if closed[a.find(v)] {
					// a Close exists — but do all paths reach it?
					if warn := a.fdEarlyReturnWarn(p, f, as, call, v, id); warn != nil {
						out = append(out, *warn)
					}
					return true
				}
				spans := append([]span{{T: "file handle "}, {T: id.Name, V: a.varID(v)},
					{T: " ← "}}, truncateSpans(a.exprSpans(p, call), 50)...)
				spans = append(spans, span{T: " is never closed"})
				out = append(out, finding{pos: as.Pos(),
					n: nodeWithSpans(a.relPos(as.Pos()), "fd-leak", "", spans)})
				return true
			})
		}
	}
	return out
}

// fdEarlyReturnWarn flags opens whose Close IS reached on the main path
// but skipped by early returns between the open and the Close (or its
// defer registration). The error-guard immediately after the open is
// exempt — on that path the handle was never valid.
func (a *analyzer) fdEarlyReturnWarn(p *packages.Package, f *ast.File, as *ast.AssignStmt, call *ast.CallExpr, v types.Object, id *ast.Ident) *finding {
	body := enclosingFuncBody(f, as.Pos())
	if body == nil {
		return nil
	}
	root := a.find(v)

	// earliest Close (or defer registration) on v's class in this function
	var closePos token.Pos
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if c, ok := n.(*ast.CallExpr); ok && len(c.Args) == 0 {
			if sel, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "Close" {
				if k := varRootObj(p.TypesInfo, sel.X); k != nil && a.find(k) == root {
					pos := c.Pos()
					for i := len(stack) - 1; i >= 0; i-- {
						if _, ok := stack[i].(*ast.FuncLit); ok {
							break
						}
						if d, ok := stack[i].(*ast.DeferStmt); ok {
							pos = d.Pos() // registration point
							break
						}
					}
					if !closePos.IsValid() || pos < closePos {
						closePos = pos
					}
				}
			}
		}
		stack = append(stack, n)
		return true
	})
	if !closePos.IsValid() || closePos < as.End() {
		return nil // closed elsewhere / before — nothing to path-check here
	}

	guard := stmtAfter(body, as) // the `if err != nil { return … }` idiom
	var returns []token.Pos
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.GoStmt:
			return false
		}
		if ret, ok := n.(*ast.ReturnStmt); ok {
			if ret.Pos() > as.End() && ret.Pos() < closePos {
				if guard == nil || !within(ret.Pos(), guard) {
					returns = append(returns, ret.Pos())
				}
			}
		}
		return true
	})
	if len(returns) == 0 {
		return nil
	}

	spans := append([]span{{T: "file handle "}, {T: id.Name, V: a.varID(v)},
		{T: " ← "}}, truncateSpans(a.exprSpans(p, call), 45)...)
	spans = append(spans, span{T: fmt.Sprintf(" is closed, but %d early-return path(s) skip the close", len(returns))})
	n := nodeWithSpans(a.relPos(as.Pos()), "fd-leak-warn", "", spans)
	n.notep(a.relPos(closePos), "closed here (registered on the main path only)")
	for i, r := range returns {
		if i == 4 {
			n.note("… and %d more early returns", len(returns)-4)
			break
		}
		n.notep(a.relPos(r), "returns without closing")
	}
	return &finding{pos: as.Pos(), n: n}
}

// stmtAfter returns the statement immediately following target in its
// enclosing block, if it is an if-statement (the open's error guard).
func stmtAfter(body *ast.BlockStmt, target ast.Stmt) ast.Stmt {
	var next ast.Stmt
	ast.Inspect(body, func(n ast.Node) bool {
		blk, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, s := range blk.List {
			if s == target && i+1 < len(blk.List) {
				if ifs, ok := blk.List[i+1].(*ast.IfStmt); ok {
					next = ifs
				}
				return false
			}
		}
		return true
	})
	return next
}

func returnsOSFile(info *types.Info, call *ast.CallExpr) bool {
	t := info.TypeOf(call)
	if tuple, ok := t.(*types.Tuple); ok {
		if tuple.Len() == 0 {
			return false
		}
		t = tuple.At(0).Type()
	}
	return t != nil && types.TypeString(t, nil) == "*os.File"
}

// escapesViaReturn reports whether v's alias class is returned by the
// function enclosing pos — the caller then owns closing it.
func (a *analyzer) escapesViaReturn(f *ast.File, pos token.Pos, v types.Object) bool {
	body := enclosingFuncBody(f, pos)
	if body == nil {
		return false
	}
	p, _ := a.fileFor(pos)
	escapes := false
	ast.Inspect(body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, res := range ret.Results {
			if k := varRootObj(p.TypesInfo, res); k != nil && a.find(k) == a.find(v) {
				escapes = true
			}
		}
		return true
	})
	return escapes
}

// ---------- 4. potential goroutine leaks ----------

// findGoroutineLeaks flags goroutines that (a) block forever on a channel
// operation with no counterpart anywhere in the module, or (b) spin in an
// infinite loop with no return and no channel wait.
func (a *analyzer) findGoroutineLeaks() []finding {
	var out []finding
	for _, p := range a.pkgs {
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				gs, ok := n.(*ast.GoStmt)
				if !ok {
					return true
				}
				body, bodyPkg := a.goroutineBody(p, gs)
				if body == nil {
					return true
				}
				out = append(out, a.leaksInBody(bodyPkg, gs, body)...)
				return true
			})
		}
	}
	return out
}

// goroutineBody resolves the body a go statement executes: a function
// literal, or the declaration of a directly-invoked local function.
func (a *analyzer) goroutineBody(p *packages.Package, gs *ast.GoStmt) (*ast.BlockStmt, *packages.Package) {
	if lit, ok := ast.Unparen(gs.Call.Fun).(*ast.FuncLit); ok {
		return lit.Body, p
	}
	if fn, ok := typeutil.Callee(p.TypesInfo, gs.Call).(*types.Func); ok {
		if def, ok := a.funcs[fn.Origin()]; ok {
			return def.decl.Body, def.pkg
		}
	}
	return nil, nil
}

func (a *analyzer) leaksInBody(p *packages.Package, gs *ast.GoStmt, body *ast.BlockStmt) []finding {
	info := p.TypesInfo
	var out []finding

	// channel operations in this goroutine's own flow, skipping operations
	// inside multi-case selects (another case can unblock them)
	var skipRanges [][2]token.Pos
	walkSameFlow(body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectStmt); ok {
			cases := sel.Body.List
			if len(cases) > 1 {
				for _, c := range cases {
					if cc, ok := c.(*ast.CommClause); ok && cc.Comm != nil {
						skipRanges = append(skipRanges, [2]token.Pos{cc.Comm.Pos(), cc.Comm.End()})
					}
				}
			}
		}
		return true
	})
	skipped := func(pos token.Pos) bool {
		for _, r := range skipRanges {
			if pos >= r[0] && pos <= r[1] {
				return true
			}
		}
		return false
	}

	check := func(kind chanOpKind, ch ast.Expr, pos token.Pos, what string) {
		if skipped(pos) {
			return
		}
		key := a.chanKey(info, ch)
		if key == nil {
			return
		}
		if a.externalChanKey(key) {
			// the other end lives outside the module (ctx.Done, time.After,
			// ticker.C, library-returned or signal.Notify-registered
			// channels) — library-owned behavior, not a leak signal
			return
		}
		if len(a.chanPeers(key, kind, pos)) > 0 {
			return
		}

		// Grade: concrete LEAK only for a purely local channel with no
		// counterpart anywhere; unverifiable cases are warnings with the
		// reason.
		leakKind := "go-leak"
		var reasons []string
		if _, ok := ast.Unparen(ch).(*ast.StarExpr); ok {
			leakKind = "go-leak-warn"
			reasons = append(reasons, "the channel sits behind a pointer — its identity (and counterpart) can't be verified")
		}
		if a.chanEscapes(key) {
			leakKind = "go-leak-warn"
			reasons = append(reasons, "the channel escapes this function (parameter, struct field or return value) — the counterpart may live in code the analysis can't link")
		}
		if kind == chanSend && a.chanBuffered(key) {
			leakKind = "go-leak-warn"
			reasons = append(reasons, "the channel is buffered — the send only blocks once the buffer is full")
		}
		if leakKind == "go-leak" && !a.posReachable(gs.Pos()) {
			leakKind = "go-leak-warn"
			reasons = append(reasons, "the enclosing function has no callers in this codebase")
		}

		spans := append([]span{{T: "goroutine may block forever: "}}, truncateSpans(a.exprSpans(p, ch), 40)...)
		spans = append(spans, span{T: " has no " + what + " in the module"})
		n := nodeWithSpans(a.relPos(pos), leakKind, "", spans)
		n.notep(a.relPos(gs.Pos()), "goroutine launched here")
		for _, r := range reasons {
			n.note("%s", r)
		}
		out = append(out, finding{pos: pos, n: n})
	}

	walkSameFlow(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SendStmt:
			check(chanSend, x.Chan, x.Arrow, "readers")
		case *ast.UnaryExpr:
			if x.Op == token.ARROW {
				check(chanRecv, x.X, x.OpPos, "writers or closers")
			}
		case *ast.RangeStmt:
			if isChanType(info.TypeOf(x.X)) {
				check(chanRecv, x.X, x.For, "writers or closers")
			}
		}
		return true
	})

	// infinite loops with no way out — only pure spins: any function call
	// inside (time.Sleep, blocking I/O, work) means the loop makes
	// progress or blocks legitimately, the pattern of long-lived daemons
	walkSameFlow(body, func(n ast.Node) bool {
		loop, ok := n.(*ast.ForStmt)
		if !ok || loop.Cond != nil {
			return true
		}
		hasExit := false
		walkSameFlow(loop.Body, func(inner ast.Node) bool {
			switch y := inner.(type) {
			case *ast.ReturnStmt, *ast.SelectStmt, *ast.RangeStmt, *ast.CallExpr:
				hasExit = true
			case *ast.BranchStmt:
				if y.Tok == token.BREAK || y.Tok == token.GOTO {
					hasExit = true
				}
			case *ast.UnaryExpr:
				if y.Op == token.ARROW {
					hasExit = true
				}
			}
			return !hasExit
		})
		if !hasExit {
			n := &node{Pos: a.relPos(loop.For), Kind: "go-leak",
				Text: "goroutine spins in an infinite loop with no return, break or channel wait"}
			n.notep(a.relPos(gs.Pos()), "goroutine launched here")
			out = append(out, finding{pos: loop.For, n: n})
		}
		return true
	})

	return out
}

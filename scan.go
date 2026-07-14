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
	case "race", "chan-closed", "fd-leak", "go-leak":
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
func enclosingFuncBody(f *ast.File, pos token.Pos) *ast.BlockStmt {
	var body *ast.BlockStmt
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return false
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
			body = d.Body
		}
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
}

// collectAccesses records every variable read/write under root, skipping
// the given subtree.
func collectAccesses(info *types.Info, root ast.Node, skipNode ast.Node) []identAcc {
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
		if id, ok := n.(*ast.Ident); ok && id.Name != "_" {
			obj := info.Uses[id]
			if obj == nil {
				obj = info.Defs[id]
			}
			if v, ok := obj.(*types.Var); ok {
				out = append(out, identAcc{v: v, pos: id.Pos(), write: isWriteAccess(id, stack)})
			}
		}
		stack = append(stack, n)
		return true
	})
	return out
}

// isWriteAccess reports whether the identifier is (part of) an assignment
// target, incremented, ranged into, or has its address taken.
func isWriteAccess(id *ast.Ident, stack []ast.Node) bool {
	var child ast.Node = id
	for i := len(stack) - 1; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.SelectorExpr:
			if p.X != child {
				return false // id is the field name; the root ident decides
			}
			child = p
		case *ast.IndexExpr:
			if p.X != child {
				return false
			}
			child = p
		case *ast.StarExpr:
			child = p
		case *ast.ParenExpr:
			child = p
		case *ast.UnaryExpr:
			// address taken — anything could write through it
			return p.Op == token.AND && p.X == child
		case *ast.AssignStmt:
			return slices.ContainsFunc(p.Lhs, func(l ast.Expr) bool { return l == child })
		case *ast.IncDecStmt:
			return p.X == child
		case *ast.RangeStmt:
			return p.Key == child || p.Value == child
		default:
			return false
		}
	}
	return false
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
				gs, ok := n.(*ast.GoStmt)
				if !ok {
					return true
				}
				lit, ok := ast.Unparen(gs.Call.Fun).(*ast.FuncLit)
				if !ok {
					return true
				}
				out = append(out, a.racesInGoroutine(p, f, gs, lit)...)
				return true
			})
		}
	}
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
	for _, acc := range collectAccesses(p.TypesInfo, lit.Body, nil) {
		// captured: declared outside the literal
		if !acc.v.Pos().IsValid() || within(acc.v.Pos(), lit) || syncSafeType(acc.v.Type()) {
			continue
		}
		get(acc.v).in = append(get(acc.v).in, acc)
	}
	for _, acc := range collectAccesses(p.TypesInfo, encl, lit) {
		if s, tracked := byVar[acc.v]; tracked {
			// Concurrent with the goroutine: anything after the launch —
			// or anywhere in the loop body when the launch sits inside a
			// loop AND the variable outlives iterations. Range/loop-local
			// variables are per-iteration (Go ≥1.22): each goroutine gets
			// its own copy, so only same-iteration accesses after the
			// launch can race.
			perIteration := loop != nil && within(acc.v.Pos(), loop)
			if acc.pos > gs.End() || (loop != nil && !perIteration) {
				s.out = append(s.out, acc)
			}
		}
	}

	var out []finding
	for v, s := range byVar {
		wIn := firstWrite(s.in)
		wOut := firstWrite(s.out)
		var conflict *identAcc
		switch {
		case wIn != nil && len(s.out) > 0:
			// cite an access after the launch when one exists — clearer
			// evidence than a pre-launch access in a loop
			conflict = &s.out[0]
			for i := range s.out {
				if s.out[i].pos > gs.End() {
					conflict = &s.out[i]
					break
				}
			}
		case wOut != nil && len(s.in) > 0:
			conflict = wOut
		default:
			continue
		}
		spans := []span{{T: "potential data race on "}, {T: v.Name(), V: a.varID(v)},
			{T: " — captured by goroutine without synchronization"}}
		n := nodeWithSpans(a.relPos(s.in[0].pos), "race", "", spans)
		n.notep(a.relPos(gs.Pos()), "goroutine launched here")
		if wIn != nil {
			n.notep(a.relPos(wIn.pos), "written inside the goroutine")
		}
		n.notep(a.relPos(conflict.pos), "%s outside the goroutine, concurrent with it", accWord(conflict))
		out = append(out, finding{pos: s.in[0].pos, n: n})
	}
	return out
}

func firstWrite(accs []identAcc) *identAcc {
	for i := range accs {
		if accs[i].write {
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
				walkSameFlow(fd.Body, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.SendStmt:
						sends = append(sends, opRec{a.chanKey(info, x.Chan), x.Arrow, x.Chan})
					case *ast.CallExpr:
						if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && id.Name == "close" && len(x.Args) == 1 {
							if _, isB := info.Uses[id].(*types.Builtin); isB {
								closes = append(closes, opRec{a.chanKey(info, x.Args[0]), x.Pos(), x.Args[0]})
							}
						}
					}
					return true
				})
				for _, s := range sends {
					if s.key == nil {
						continue
					}
					for _, c := range closes {
						if c.key != nil && c.pos < s.pos && a.find(c.key) == a.find(s.key) {
							spans := append([]span{{T: "send on closed channel: "}}, a.exprSpans(p, s.ch)...)
							spans = append(spans, span{T: " is closed earlier in this function"})
							n := nodeWithSpans(a.relPos(s.pos), "chan-closed", "", truncateSpans(spans, 90))
							n.notep(a.relPos(c.pos), "closed here")
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
		n := &node{Pos: a.relPos(c.pos), Kind: "chan-closed",
			Text: fmt.Sprintf("channel closed in %s while %d sender(s) elsewhere may still write to it", c.fn, len(rogue))}
		for i, s := range rogue {
			if i == 5 {
				n.note("… and %d more senders", len(rogue)-5)
				break
			}
			n.notep(a.relPos(s.pos), "sender: %s", s.fn)
		}
		out = append(out, finding{pos: c.pos, n: n})
	}
	return out
}

// waitGroupWaitBefore reports whether the function enclosing pos calls
// (*sync.WaitGroup).Wait before pos — the usual close coordination.
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
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Wait" {
			return true
		}
		if t := p.TypesInfo.TypeOf(sel.X); t != nil {
			if named, ok := types.Unalias(deref(t)).(*types.Named); ok {
				if pkg := named.Obj().Pkg(); pkg != nil && pkg.Path() == "sync" && named.Obj().Name() == "WaitGroup" {
					found = true
				}
			}
		}
		return true
	})
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
				if v == nil || closed[a.find(v)] || a.escapesViaReturn(f, as.Pos(), v) {
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
		if len(a.chanPeers(key, kind, pos)) > 0 {
			return
		}
		spans := append([]span{{T: "goroutine may block forever: "}}, truncateSpans(a.exprSpans(p, ch), 40)...)
		spans = append(spans, span{T: " has no " + what + " in the module"})
		n := nodeWithSpans(a.relPos(pos), "go-leak", "", spans)
		n.notep(a.relPos(gs.Pos()), "goroutine launched here")
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

	// infinite loops with no way out
	walkSameFlow(body, func(n ast.Node) bool {
		loop, ok := n.(*ast.ForStmt)
		if !ok || loop.Cond != nil {
			return true
		}
		hasExit := false
		walkSameFlow(loop.Body, func(inner ast.Node) bool {
			switch y := inner.(type) {
			case *ast.ReturnStmt, *ast.SelectStmt, *ast.RangeStmt:
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

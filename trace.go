package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const lineBudget = 200

// trace walks the target function and produces the execution trace tree: a
// verbatim, line-by-line rendering of the source along the execution path,
// with every variable highlighted for tracking. Local calls are expanded
// inline — the callee's body is nested beneath the line that made the call.
func (a *analyzer) trace(t *target) *node {
	root := nodeWithSpans(a.relPos(t.def.decl.Pos()), "root", "local",
		a.rootSpans(t.def.pkg, t.def.decl))
	if root.Spans == nil {
		root.Text = types.ObjectString(t.fn, types.RelativeTo(t.fn.Pkg()))
	}
	a.expandedAt[t.fn.Origin()] = a.relPos(t.def.decl.Pos())
	a.stack = append(a.stack, t.fn.Origin())
	a.block(t.def.pkg, t.def.decl.Body, root)
	a.stack = a.stack[:0]
	prune(root)
	var loops int
	numberLoops(root, &loops)
	// Collapse per-object variable tokens into final colors using only the
	// aliasing edges whose gating context was expanded in this trace.
	a.remapVarIDs(root)
	if a.truncated {
		root.note("… trace truncated (node limit %d reached)", maxNodes)
	}
	return root
}

// full enforces the global node budget.
func (a *analyzer) full() bool {
	if a.nodeCount >= maxNodes {
		a.truncated = true
		return true
	}
	a.nodeCount++
	return false
}

func (a *analyzer) block(p *packages.Package, b *ast.BlockStmt, parent *node) {
	if b == nil {
		return
	}
	for _, s := range b.List {
		a.stmt(p, s, parent)
	}
}

// line builds a node holding the verbatim source of [start,end) with every
// variable occurrence marked for tracking.
func (a *analyzer) line(p *packages.Package, kind string, start, end token.Pos, root ast.Node) *node {
	return nodeWithSpans(a.relPos(start), kind, "",
		truncateSpans(a.spansForRange(p, start, end, root), lineBudget))
}

// header renders a compound statement's opening line up to and including the
// block's "{" ("if cond {", "for … {", "switch x {").
func (a *analyzer) header(p *packages.Package, kind string, start token.Pos, body *ast.BlockStmt, root ast.Node) *node {
	return a.line(p, kind, start, body.Lbrace+1, root)
}

// stmt renders one statement as its source line(s) and expands the local calls
// it makes (their bodies nested beneath).
func (a *analyzer) stmt(p *packages.Package, s ast.Stmt, parent *node) {
	if s == nil || a.full() {
		return
	}
	switch x := s.(type) {
	case *ast.IfStmt:
		if x.Init != nil {
			a.stmt(p, x.Init, parent)
		}
		hn := parent.add(a.header(p, "branch", x.If, x.Body, x))
		a.expandCallsIn(p, x.Cond, hn)
		a.block(p, x.Body, hn)
		if x.Else != nil {
			if blk, ok := x.Else.(*ast.BlockStmt); ok {
				en := parent.add(&node{Pos: a.relPos(x.Else.Pos()), Kind: "branch", Text: "} else {"})
				a.block(p, blk, en)
			} else {
				a.stmt(p, x.Else, parent) // "else if …"
			}
		}
	case *ast.ForStmt:
		if x.Init != nil {
			a.stmt(p, x.Init, parent)
		}
		hn := parent.add(a.header(p, "loop", x.For, x.Body, x))
		hn.loop = true
		a.expandCallsIn(p, x.Cond, hn)
		a.block(p, x.Body, hn)
		a.expandCallsIn(p, x.Post, hn)
	case *ast.RangeStmt:
		hn := parent.add(a.header(p, "loop", x.For, x.Body, x))
		hn.loop = true
		a.expandCallsIn(p, x.X, hn)
		a.block(p, x.Body, hn)
	case *ast.SwitchStmt:
		if x.Init != nil {
			a.stmt(p, x.Init, parent)
		}
		hn := parent.add(a.header(p, "branch", x.Switch, x.Body, x))
		a.expandCallsIn(p, x.Tag, hn)
		a.caseClauses(p, x.Body, hn)
	case *ast.TypeSwitchStmt:
		if x.Init != nil {
			a.stmt(p, x.Init, parent)
		}
		hn := parent.add(a.header(p, "branch", x.Switch, x.Body, x))
		a.caseClauses(p, x.Body, hn)
	case *ast.SelectStmt:
		hn := parent.add(&node{Pos: a.relPos(x.Pos()), Kind: "select", Text: "select {"})
		a.caseClauses(p, x.Body, hn)
	case *ast.BlockStmt:
		a.block(p, x, parent)
	case *ast.LabeledStmt:
		a.stmt(p, x.Stmt, parent)
	case *ast.GoStmt:
		a.stmtCall(p, x, x.Call, parent)
	case *ast.DeferStmt:
		a.stmtCall(p, x, x.Call, parent)
	default:
		ln := parent.add(a.line(p, stmtKind(s), s.Pos(), s.End(), s))
		a.expandCallsIn(p, s, ln)
	}
}

func stmtKind(s ast.Stmt) string {
	switch s.(type) {
	case *ast.ReturnStmt:
		return "return"
	case *ast.SendStmt:
		return "chan-send"
	}
	return "stmt"
}

// stmtCall renders a `go`/`defer` statement as one verbatim line and expands
// the called function/closure beneath it. For an inline closure, only the
// header up to its body "{" is shown (the body becomes the nested children).
func (a *analyzer) stmtCall(p *packages.Package, stmt ast.Node, call *ast.CallExpr, parent *node) {
	if lit, ok := ast.Unparen(call.Fun).(*ast.FuncLit); ok {
		hn := parent.add(a.line(p, "stmt", stmt.Pos(), lit.Body.Lbrace+1, stmt))
		a.expandLit(p, lit, a.relPos(call.Lparen), hn)
		for _, arg := range call.Args {
			a.expandCallsIn(p, arg, hn)
		}
		return
	}
	ln := parent.add(a.line(p, "stmt", stmt.Pos(), stmt.End(), stmt))
	a.expandCallsIn(p, call, ln)
}

// caseClauses renders the case/comm clauses of a switch or select.
func (a *analyzer) caseClauses(p *packages.Package, body *ast.BlockStmt, parent *node) {
	for _, c := range body.List {
		switch cc := c.(type) {
		case *ast.CaseClause:
			cn := parent.add(a.line(p, "case", cc.Case, cc.Colon+1, cc))
			for _, e := range cc.List {
				a.expandCallsIn(p, e, cn)
			}
			for _, bs := range cc.Body {
				a.stmt(p, bs, cn)
			}
		case *ast.CommClause:
			cn := parent.add(a.line(p, "case", cc.Case, cc.Colon+1, cc))
			a.expandCallsIn(p, cc.Comm, cn)
			for _, bs := range cc.Body {
				a.stmt(p, bs, cn)
			}
		}
	}
}

// expandCallsIn expands every local call found in an expression/statement,
// nesting the callee bodies under parent. Function-literal bodies are not
// descended into — they run only when invoked, handled at the call site.
func (a *analyzer) expandCallsIn(p *packages.Package, root ast.Node, parent *node) {
	if root == nil {
		return
	}
	ast.Inspect(root, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			a.expandCall(p, x, parent)
		}
		return true
	})
}

// expandCall resolves a call's callee and expands its body under parent,
// without emitting a node for the call itself (the call already appears in the
// enclosing source line). Nested calls in arguments/receiver are handled by
// the caller's ast.Inspect walk.
func (a *analyzer) expandCall(p *packages.Package, call *ast.CallExpr, parent *node) {
	if a.full() {
		return
	}
	info := p.TypesInfo
	fun := ast.Unparen(call.Fun)
	at := a.relPos(call.Lparen)

	if lit, ok := fun.(*ast.FuncLit); ok {
		a.expandLit(p, lit, at, parent)
		return
	}
	switch callee := typeutil.Callee(info, call).(type) {
	case *types.Func:
		if recv := callee.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
			sel, ok := fun.(*ast.SelectorExpr)
			iface, _ := recv.Type().Underlying().(*types.Interface)
			if ok && iface != nil {
				resolved := dedupFuncs(a.concreteImpls(info, sel.X, iface, callee.Name()))
				if len(resolved) == 1 && a.classify(resolved[0].Pkg()) == "local" {
					a.expand(resolved[0], "local", at, parent)
				}
			}
			return
		}
		a.expand(callee, a.classify(callee.Pkg()), at, parent)
	case *types.Var:
		a.expandFuncValue(p, callee, at, parent)
	}
}

// expandFuncValue resolves a call through a function-typed variable and
// expands the bound function/literal/method value, when statically known.
func (a *analyzer) expandFuncValue(p *packages.Package, v *types.Var, at string, parent *node) {
	site, ok := a.defs[v]
	if !ok {
		return
	}
	rhs := rhsForVar(site, v)
	if rhs == nil {
		return
	}
	info := site.pkg.TypesInfo
	switch r := ast.Unparen(rhs).(type) {
	case *ast.FuncLit:
		a.expandLit(site.pkg, r, at, parent)
	case *ast.Ident, *ast.SelectorExpr:
		fn, ok := exprObj(info, r).(*types.Func)
		if !ok {
			return
		}
		// A method value on an interface receiver (getFoo := iface.Method)
		// resolves like an interface call: enter the concrete implementation.
		if sel, isSel := r.(*ast.SelectorExpr); isSel {
			if recv := fn.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
				if iface, _ := recv.Type().Underlying().(*types.Interface); iface != nil {
					resolved := dedupFuncs(a.concreteImpls(info, sel.X, iface, fn.Name()))
					if len(resolved) == 1 && a.classify(resolved[0].Pkg()) == "local" {
						a.expand(resolved[0], "local", at, parent)
					}
				}
				return
			}
		}
		a.expand(fn, a.classify(fn.Pkg()), at, parent)
	}
}

// rootSpans renders the traced function's signature with its receiver and
// parameters marked as trackable variables.
func (a *analyzer) rootSpans(p *packages.Package, decl *ast.FuncDecl) []span {
	roots := []ast.Node{decl.Type}
	if decl.Recv != nil {
		roots = append(roots, decl.Recv)
	}
	spans := a.spansForRange(p, decl.Type.Pos(), decl.Type.End(), roots...)
	return truncateSpans(spans, 120)
}

// concreteImpls resolves the concrete type(s) that actually flow into an
// interface-call receiver by scanning its alias class for concrete
// (non-interface) types implementing the interface. Empty when the receiver
// stays fully abstract (no concrete value pinned down in the module).
func (a *analyzer) concreteImpls(info *types.Info, recv ast.Expr, iface *types.Interface, method string) []*types.Func {
	key := a.aliasKey(info, recv)
	if key == nil {
		return nil
	}
	a.buildClassConcretes()
	seen := map[*types.Func]bool{}
	var out []*types.Func
	for _, t := range a.classConcretes[a.find(key)] {
		named := namedOf(t)
		if named == nil || named.TypeParams().Len() > 0 {
			continue
		}
		recvT := t
		if !types.Implements(recvT, iface) {
			ptr := types.NewPointer(named)
			if !types.Implements(ptr, iface) {
				continue
			}
			recvT = ptr
		}
		if obj, _, _ := types.LookupFieldOrMethod(recvT, true, named.Obj().Pkg(), method); obj != nil {
			if f, ok := obj.(*types.Func); ok && !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}

// buildClassConcretes groups every concrete type in the module by its alias
// class root, once. Unions are frozen after indexing, so roots are stable.
func (a *analyzer) buildClassConcretes() {
	if a.classConcretes != nil {
		return
	}
	a.classConcretes = map[any][]types.Type{}
	for k := range a.aliasParent {
		var t types.Type
		switch v := k.(type) {
		case *types.Var:
			t = v.Type()
		case resultKey:
			if res := v.fn.Signature().Results(); v.idx < res.Len() {
				t = res.At(v.idx).Type()
			}
		}
		if t == nil || types.IsInterface(t) || namedOf(t) == nil {
			continue
		}
		r := a.find(k)
		a.classConcretes[r] = append(a.classConcretes[r], t)
	}
}

// namedOf returns the named type of t, dereferencing a single pointer.
func namedOf(t types.Type) *types.Named {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	n, _ := t.(*types.Named)
	return n
}

// dedupFuncs removes duplicate methods that differ only by type-check pass
// (a package and its test variant yield distinct *types.Func for the same
// method), keyed by their display name.
func dedupFuncs(fns []*types.Func) []*types.Func {
	seen := map[string]bool{}
	out := fns[:0]
	for _, f := range fns {
		name := funcDisplayName(f)
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, f)
	}
	return out
}

// rhsForVar finds the right-hand-side expression bound to v at its
// definition site.
func rhsForVar(site defSite, v *types.Var) ast.Expr {
	info := site.pkg.TypesInfo
	switch d := site.node.(type) {
	case *ast.AssignStmt:
		if len(d.Rhs) != len(d.Lhs) {
			return nil
		}
		for i, lhs := range d.Lhs {
			if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
				if info.Defs[id] == types.Object(v) || info.Uses[id] == types.Object(v) {
					return d.Rhs[i]
				}
			}
		}
	case *ast.ValueSpec:
		if len(d.Values) != len(d.Names) {
			return nil
		}
		for i, name := range d.Names {
			if info.Defs[name] == types.Object(v) {
				return d.Values[i]
			}
		}
	}
	return nil
}

func exprObj(info *types.Info, e ast.Expr) types.Object {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		return info.Uses[x]
	case *ast.SelectorExpr:
		return info.Uses[x.Sel]
	}
	return nil
}

// expand traces into a callee's body if — and only if — it is local. Stdlib
// and external-module calls are never entered. Each body is expanded once per
// trace (later call sites show the call line with no nested body).
func (a *analyzer) expand(fn *types.Func, label string, at string, n *node) {
	if label != "local" {
		return
	}
	origin := fn.Origin()
	def, ok := a.funcs[origin]
	if !ok || def.decl.Body == nil {
		return
	}
	if slices.Contains(a.stack, origin) {
		return // recursion — stop silently
	}
	if _, done := a.expandedAt[origin]; done && !a.expandAll {
		return
	}
	if len(a.stack) >= a.maxDepth {
		return
	}
	a.expandedAt[origin] = at
	a.stack = append(a.stack, origin)
	a.block(def.pkg, def.decl.Body, n)
	a.stack = a.stack[:len(a.stack)-1]
}

// expandLit traces a function literal's body with depth protection (literals
// aren't on the named-function cycle stack).
func (a *analyzer) expandLit(p *packages.Package, lit *ast.FuncLit, at string, n *node) {
	if _, done := a.expandedLits[lit]; done && !a.expandAll {
		return
	}
	if len(a.stack)+a.litDepth >= a.maxDepth {
		return
	}
	a.expandedLits[lit] = at
	a.litDepth++
	a.block(p, lit.Body, n)
	a.litDepth--
}

func funcDisplayName(fn *types.Func) string {
	qual := func(p *types.Package) string { return p.Name() }
	if recv := fn.Signature().Recv(); recv != nil {
		return "(" + types.TypeString(recv.Type(), qual) + ")." + fn.Name()
	}
	if fn.Pkg() != nil {
		return fn.Pkg().Name() + "." + fn.Name()
	}
	return fn.Name()
}

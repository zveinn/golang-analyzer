package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// trace walks the target function and produces the execution trace tree.
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

func (a *analyzer) stmt(p *packages.Package, s ast.Stmt, parent *node) {
	if s == nil || a.full() {
		return
	}
	switch x := s.(type) {
	case *ast.ExprStmt:
		a.expr(p, x.X, parent)
	case *ast.AssignStmt:
		for _, e := range x.Rhs {
			a.expr(p, e, parent)
		}
		for _, e := range x.Lhs {
			a.expr(p, e, parent)
		}
	case *ast.GoStmt:
		spans := []span{{T: "go func literal(…)"}}
		if _, ok := ast.Unparen(x.Call.Fun).(*ast.FuncLit); !ok {
			spans = append(truncateSpans(append([]span{{T: "go "}}, a.exprSpans(p, x.Call.Fun)...), 60), span{T: "(…)"})
		}
		gn := parent.add(nodeWithSpans(a.relPos(x.Pos()), "go", "", spans))
		a.call(p, x.Call, gn)
	case *ast.DeferStmt:
		dn := parent.add(&node{Pos: a.relPos(x.Pos()), Kind: "defer", Text: "defer", structural: true})
		a.call(p, x.Call, dn)
	case *ast.SendStmt:
		a.expr(p, x.Value, parent)
		a.chanEvent(p, chanSend, x.Chan, x.Value, x.Arrow, parent)
	case *ast.IfStmt:
		a.stmt(p, x.Init, parent)
		in := parent.add(a.structuralNode(p, a.relPos(x.If), "branch", "if ", x.Cond))
		a.expr(p, x.Cond, in)
		a.block(p, x.Body, in)
		if x.Else != nil {
			en := parent.add(&node{Pos: a.relPos(x.Else.Pos()), Kind: "branch", Text: "else", structural: true})
			a.stmt(p, x.Else, en)
		}
	case *ast.BlockStmt:
		a.block(p, x, parent)
	case *ast.ForStmt:
		a.stmt(p, x.Init, parent)
		ln := a.structuralNode(p, a.relPos(x.For), "loop", "for", nil)
		if x.Cond != nil {
			ln = a.structuralNode(p, a.relPos(x.For), "loop", "for ", x.Cond)
		}
		ln.loop = true
		parent.add(ln)
		a.expr(p, x.Cond, ln)
		a.block(p, x.Body, ln)
		a.stmt(p, x.Post, ln)
	case *ast.RangeStmt:
		a.expr(p, x.X, parent) // range expression is evaluated once, before the loop
		ln := a.structuralNode(p, a.relPos(x.For), "loop", "for range ", x.X)
		ln.loop = true
		parent.add(ln)
		if isChanType(p.TypesInfo.TypeOf(x.X)) {
			a.chanEvent(p, chanRecv, x.X, nil, x.For, ln)
		}
		a.block(p, x.Body, ln)
	case *ast.SwitchStmt:
		a.stmt(p, x.Init, parent)
		sn := a.structuralNode(p, a.relPos(x.Switch), "branch", "switch", nil)
		if x.Tag != nil {
			sn = a.structuralNode(p, a.relPos(x.Switch), "branch", "switch ", x.Tag)
		}
		parent.add(sn)
		a.expr(p, x.Tag, sn)
		a.caseClauses(p, x.Body, sn)
	case *ast.TypeSwitchStmt:
		a.stmt(p, x.Init, parent)
		sn := parent.add(&node{Pos: a.relPos(x.Switch), Kind: "branch", Text: "type switch", structural: true})
		a.stmt(p, x.Assign, sn)
		a.caseClauses(p, x.Body, sn)
	case *ast.SelectStmt:
		sn := parent.add(&node{Pos: a.relPos(x.Pos()), Kind: "select", Text: "select", structural: true})
		for _, c := range x.Body.List {
			cc := c.(*ast.CommClause)
			spans := []span{{T: "default:"}}
			if cc.Comm != nil {
				spans = append(append([]span{{T: "case "}}, a.commSpans(p, cc.Comm)...), span{T: ":"})
			}
			cn := sn.add(nodeWithSpans(a.relPos(cc.Case), "case", "", spans))
			cn.structural = true
			a.stmt(p, cc.Comm, cn)
			for _, bs := range cc.Body {
				a.stmt(p, bs, cn)
			}
		}
	case *ast.ReturnStmt:
		for _, e := range x.Results {
			a.expr(p, e, parent)
		}
	case *ast.DeclStmt:
		if gd, ok := x.Decl.(*ast.GenDecl); ok {
			for _, spec := range gd.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok {
					for _, v := range vs.Values {
						a.expr(p, v, parent)
					}
				}
			}
		}
	case *ast.LabeledStmt:
		a.stmt(p, x.Stmt, parent)
	case *ast.IncDecStmt:
		a.expr(p, x.X, parent)
	}
}

func (a *analyzer) caseClauses(p *packages.Package, body *ast.BlockStmt, parent *node) {
	for _, c := range body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			continue
		}
		cn := parent.add(nodeWithSpans(a.relPos(cc.Case), "case", "", a.caseSpans(p, cc.List)))
		cn.structural = true
		for _, e := range cc.List {
			a.expr(p, e, cn)
		}
		for _, bs := range cc.Body {
			a.stmt(p, bs, cn)
		}
	}
}

func (a *analyzer) caseSpans(p *packages.Package, list []ast.Expr) []span {
	if len(list) == 0 {
		return []span{{T: "default:"}}
	}
	spans := []span{{T: "case "}}
	for i, e := range list {
		if i > 0 {
			spans = append(spans, span{T: ", "})
		}
		spans = append(spans, a.exprSpans(p, e)...)
	}
	return append(truncateSpans(spans, 70), span{T: ":"})
}

// commSpans renders a select case's communication statement with variable
// markers.
func (a *analyzer) commSpans(p *packages.Package, s ast.Stmt) []span {
	if spans := a.spansForRange(p, s.Pos(), s.End(), s); spans != nil {
		return truncateSpans(spans, 70)
	}
	return []span{{T: "?"}}
}

// structuralNode builds a branch/loop header node like "if <cond>" with the
// condition's variables marked.
func (a *analyzer) structuralNode(p *packages.Package, pos, kind, head string, e ast.Expr) *node {
	spans := []span{{T: head}}
	if e != nil {
		spans = append(spans, truncateSpans(a.exprSpans(p, e), 60)...)
	}
	n := nodeWithSpans(pos, kind, "", spans)
	n.structural = true
	return n
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

// expr walks an expression, emitting nodes for calls and channel receives.
func (a *analyzer) expr(p *packages.Package, e ast.Expr, parent *node) {
	switch x := e.(type) {
	case nil:
	case *ast.CallExpr:
		a.call(p, x, parent)
	case *ast.UnaryExpr:
		a.expr(p, x.X, parent)
		if x.Op == token.ARROW {
			a.chanEvent(p, chanRecv, x.X, nil, x.OpPos, parent)
		}
	case *ast.BinaryExpr:
		a.expr(p, x.X, parent)
		a.expr(p, x.Y, parent)
	case *ast.ParenExpr:
		a.expr(p, x.X, parent)
	case *ast.StarExpr:
		a.expr(p, x.X, parent)
	case *ast.TypeAssertExpr:
		a.expr(p, x.X, parent)
	case *ast.SelectorExpr:
		a.expr(p, x.X, parent)
	case *ast.IndexExpr:
		a.expr(p, x.X, parent)
		a.expr(p, x.Index, parent)
	case *ast.IndexListExpr:
		a.expr(p, x.X, parent)
	case *ast.SliceExpr:
		a.expr(p, x.X, parent)
		a.expr(p, x.Low, parent)
		a.expr(p, x.High, parent)
		a.expr(p, x.Max, parent)
	case *ast.CompositeLit:
		for _, el := range x.Elts {
			a.expr(p, el, parent)
		}
	case *ast.KeyValueExpr:
		a.expr(p, x.Key, parent)
		a.expr(p, x.Value, parent)
	case *ast.FuncLit:
		// A definition, not a call — its body runs only when invoked.
	}
}

// call resolves and traces a single call expression.
func (a *analyzer) call(p *packages.Package, call *ast.CallExpr, parent *node) {
	if a.full() {
		return
	}
	info := p.TypesInfo

	// Type conversion, e.g. []byte(s) — not a function call.
	if tv, ok := info.Types[call.Fun]; ok && tv.IsType() {
		for _, arg := range call.Args {
			a.expr(p, arg, parent)
		}
		return
	}

	fun := ast.Unparen(call.Fun)

	// Immediately-invoked function literal: func(){...}()
	if lit, ok := fun.(*ast.FuncLit); ok {
		n := parent.add(&node{Pos: a.relPos(call.Lparen), Kind: "call", Label: "local", Text: "func literal()"})
		a.walkArgs(p, call, nil, n)
		a.expandLit(p, lit, a.relPos(call.Lparen), n)
		return
	}

	switch callee := typeutil.Callee(info, call).(type) {
	case *types.Builtin:
		if callee.Name() == "close" && len(call.Args) == 1 {
			a.chanEvent(p, chanClose, call.Args[0], nil, call.Pos(), parent)
			return
		}
		// Other builtins (append, len, panic, …) are not traced as calls,
		// but their arguments may contain calls.
		for _, arg := range call.Args {
			a.expr(p, arg, parent)
		}
	case *types.Func:
		a.staticCall(p, call, callee, parent)
	case *types.Var:
		a.funcValueCall(p, call, callee, parent)
	default:
		n := parent.add(nodeWithSpans(a.relPos(call.Lparen), "indirect-call", "", a.callSpans(p, call)))
		a.expr(p, call.Fun, n)
		a.walkArgs(p, call, nil, n)
	}
}

// callSpans renders a call expression as its exact source text with every
// variable occurrence marked.
func (a *analyzer) callSpans(p *packages.Package, call *ast.CallExpr) []span {
	return truncateSpans(a.exprSpans(p, call), 90)
}

// staticCall handles a call whose target is a known function or method.
func (a *analyzer) staticCall(p *packages.Package, call *ast.CallExpr, fn *types.Func, parent *node) {
	if recv := fn.Signature().Recv(); recv != nil && types.IsInterface(recv.Type()) {
		a.interfaceCall(p, call, fn, parent)
		return
	}
	label := a.classify(fn.Pkg())
	spans := a.callSpans(p, call)
	if inst := a.instanceSuffix(p, call); inst != "" {
		spans = append(spans, span{T: " " + inst})
	}
	n := parent.add(nodeWithSpans(a.relPos(call.Lparen), "call", label, spans))
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		a.expr(p, sel.X, n) // receiver expression may itself contain calls
	}
	a.walkArgs(p, call, fn, n)
	a.expand(fn, label, a.relPos(call.Lparen), n)
}

// interfaceCall handles a method call through an interface: the concrete
// target is unknown statically, so every implementation in the module is
// listed (and traced, when local).
func (a *analyzer) interfaceCall(p *packages.Package, call *ast.CallExpr, fn *types.Func, parent *node) {
	label := a.classify(fn.Pkg())
	n := parent.add(nodeWithSpans(a.relPos(call.Lparen), "interface-call", label, a.callSpans(p, call)))
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		a.expr(p, sel.X, n)
	}
	a.walkArgs(p, call, nil, n)

	iface, _ := fn.Signature().Recv().Type().Underlying().(*types.Interface)
	if iface == nil {
		return
	}
	if types.Identical(iface, types.Universe.Lookup("error").Type().Underlying()) {
		n.note("error interface — implementations not enumerated")
		return
	}
	impls := a.implementations(iface, fn.Name())
	if len(impls) == 0 {
		n.note("no implementations found in module")
		return
	}
	const maxImpls = 8
	for i, impl := range impls {
		if i == maxImpls {
			n.note("… and %d more implementations", len(impls)-maxImpls)
			break
		}
		lbl := a.classify(impl.Pkg())
		in := n.add(&node{Pos: a.relPos(impl.Pos()), Kind: "impl", Label: lbl,
			Text: "possible impl: " + funcDisplayName(impl)})
		a.expand(impl, lbl, a.relPos(call.Lparen), in)
	}
}

// implementations finds every non-generic named type in the module that
// satisfies iface, returning the corresponding method.
func (a *analyzer) implementations(iface *types.Interface, method string) []*types.Func {
	var out []*types.Func
	for _, named := range a.named {
		if named.TypeParams().Len() > 0 || types.IsInterface(named) {
			continue
		}
		var recv types.Type = named
		if !types.Implements(named, iface) {
			ptr := types.NewPointer(named)
			if !types.Implements(ptr, iface) {
				continue
			}
			recv = ptr
		}
		obj, _, _ := types.LookupFieldOrMethod(recv, true, named.Obj().Pkg(), method)
		if f, ok := obj.(*types.Func); ok {
			out = append(out, f)
		}
	}
	return out
}

// funcValueCall handles calling through a variable of function type.
func (a *analyzer) funcValueCall(p *packages.Package, call *ast.CallExpr, v *types.Var, parent *node) {
	n := parent.add(nodeWithSpans(a.relPos(call.Lparen), "func-value-call", "", a.callSpans(p, call)))
	a.walkArgs(p, call, nil, n)
	site, ok := a.defs[v]
	if !ok {
		n.note("callee unknown at analysis time")
		return
	}
	switch d := site.node.(type) {
	case *ast.Field:
		n.note("func parameter %q of %s — concrete callee depends on caller",
			v.Name(), a.enclosingFuncName(site.pkg, site.file, site.node.Pos()))
		return
	case *ast.AssignStmt, *ast.ValueSpec:
		_ = d
	default:
		n.note("callee unknown at analysis time")
		return
	}
	rhs := rhsForVar(site, v)
	if rhs == nil {
		n.notep(a.relPos(site.node.Pos()), "bound here — concrete callee not statically known")
		return
	}
	switch r := ast.Unparen(rhs).(type) {
	case *ast.FuncLit:
		ln := n.add(&node{Pos: a.relPos(r.Pos()), Kind: "bound", Label: "local", Text: "bound to func literal"})
		a.expandLit(site.pkg, r, a.relPos(call.Lparen), ln)
	case *ast.Ident, *ast.SelectorExpr:
		if fn, ok := exprObj(site.pkg.TypesInfo, r).(*types.Func); ok {
			lbl := a.classify(fn.Pkg())
			in := n.add(&node{Pos: a.relPos(fn.Pos()), Kind: "bound", Label: lbl,
				Text: "bound to " + funcDisplayName(fn)})
			a.expand(fn, lbl, a.relPos(call.Lparen), in)
			return
		}
		n.notep(a.relPos(site.node.Pos()), "bound to %s — not statically resolvable", exprStr(rhs))
	default:
		n.notep(a.relPos(site.node.Pos()), "bound to %s — not statically resolvable", exprStr(rhs))
	}
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

// expand traces into a callee's body if — and only if — it is local.
// Stdlib and external-module calls are labeled but never entered. Each
// body is expanded once per trace: later call sites reference the first
// expansion instead of re-printing it (and re-numbering its loops).
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
		n.note("recursive — already in call stack, not expanding")
		return
	}
	if first, done := a.expandedAt[origin]; done && !a.expandAll {
		n.notep(first, "body already traced (at first call site)")
		return
	}
	if len(a.stack) >= a.maxDepth {
		n.note("… depth limit (%d) reached", a.maxDepth)
		return
	}
	a.expandedAt[origin] = at
	a.stack = append(a.stack, origin)
	a.block(def.pkg, def.decl.Body, n)
	a.stack = a.stack[:len(a.stack)-1]
}

// expandLit traces a function literal's body with depth protection
// (literals aren't on the named-function cycle stack).
func (a *analyzer) expandLit(p *packages.Package, lit *ast.FuncLit, at string, n *node) {
	if first, done := a.expandedLits[lit]; done && !a.expandAll {
		n.notep(first, "body already traced (at first call site)")
		return
	}
	if len(a.stack)+a.litDepth >= a.maxDepth {
		n.note("… depth limit (%d) reached", a.maxDepth)
		return
	}
	a.expandedLits[lit] = at
	a.litDepth++
	a.block(p, lit.Body, n)
	a.litDepth--
}

// walkArgs traces calls nested in the arguments and annotates them: for
// local callees with a known body, arguments are shown as parameter
// bindings ("dir ← filepath.Dir(absFile)"); otherwise as origin
// annotations ("absFile ← parameter ...").
func (a *analyzer) walkArgs(p *packages.Package, call *ast.CallExpr, fn *types.Func, n *node) {
	for _, arg := range call.Args {
		a.expr(p, arg, n)
	}
	if fn != nil && a.classify(fn.Pkg()) == "local" {
		if _, ok := a.funcs[fn.Origin()]; ok {
			a.bindParams(p, call, fn, n)
			return
		}
	}
	for _, arg := range call.Args {
		a.annotateArg(p, arg, n)
	}
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

// instanceSuffix renders inferred/explicit type arguments for calls to
// generic functions, e.g. "[int]".
func (a *analyzer) instanceSuffix(p *packages.Package, call *ast.CallExpr) string {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.IndexExpr:
		id = funIdent(f.X)
	case *ast.IndexListExpr:
		id = funIdent(f.X)
	}
	if id == nil {
		return ""
	}
	inst, ok := p.TypesInfo.Instances[id]
	if !ok || inst.TypeArgs == nil || inst.TypeArgs.Len() == 0 {
		return ""
	}
	parts := make([]string, inst.TypeArgs.Len())
	for i := range inst.TypeArgs.Len() {
		parts[i] = types.TypeString(inst.TypeArgs.At(i), types.RelativeTo(p.Types))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func funIdent(e ast.Expr) *ast.Ident {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		return x
	case *ast.SelectorExpr:
		return x.Sel
	}
	return nil
}

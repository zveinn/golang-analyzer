package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

type chanOpKind int

const (
	chanSend chanOpKind = iota
	chanRecv
	chanClose
)

func (k chanOpKind) String() string {
	switch k {
	case chanSend:
		return "send"
	case chanRecv:
		return "recv"
	default:
		return "close"
	}
}

// chanOp is one channel operation somewhere in the module.
type chanOp struct {
	kind  chanOpKind
	key   any // alias key of the channel expression; nil if dynamic
	pos   token.Pos
	fn    string    // enclosing function, e.g. "main.worker"
	fnPos token.Pos // position of the enclosing function (unique identity)
}

// resultKey identifies "result #idx of function fn" so channels returned
// from constructors/generators can be aliased to the variables that
// receive them at call sites.
type resultKey struct {
	fn  *types.Func
	idx int
}

func (a *analyzer) recordChanOp(p *packages.Package, f *ast.File, kind chanOpKind, ch ast.Expr, pos token.Pos) {
	if !isChanType(p.TypesInfo.TypeOf(ch)) {
		return
	}
	fn, fnPos := a.enclosingFuncInfo(p, f, pos)
	a.chanOps = append(a.chanOps, chanOp{
		kind:  kind,
		key:   a.chanKey(p.TypesInfo, ch),
		pos:   pos,
		fn:    fn,
		fnPos: fnPos,
	})
}

// chanKey maps a channel expression to its alias-class key: the variable or
// struct field holding it, or the result slot of the function producing it.
func (a *analyzer) chanKey(info *types.Info, e ast.Expr) any {
	e = ast.Unparen(e)
	if call, ok := e.(*ast.CallExpr); ok {
		if fn, ok := typeutil.Callee(info, call).(*types.Func); ok {
			return resultKey{fn: fn.Origin(), idx: 0}
		}
		return nil
	}
	if o := chanRootObj(info, e); o != nil {
		return o
	}
	return nil
}

// aliasKey maps any expression to its alias-class key, used to track how a
// value propagates through the trace (variable, struct field, or the result
// slot of the call producing it). Result slots of stdlib/module calls are
// not keys: their bodies are never traced, and linking through them would
// merge unrelated variables (every `x := f()` of the same stdlib f).
func (a *analyzer) aliasKey(info *types.Info, e ast.Expr) any {
	e = ast.Unparen(e)
	if isChanType(info.TypeOf(e)) {
		// Channels keep the wider linking so endpoint matching still works.
		return a.chanKey(info, e)
	}
	if call, ok := e.(*ast.CallExpr); ok {
		fn, ok := typeutil.Callee(info, call).(*types.Func)
		if !ok {
			return nil
		}
		if a.classify(fn.Pkg()) == "local" {
			return resultKey{fn: fn.Origin(), idx: 0}
		}
		return a.derivedKey(info, call, fn)
	}
	if o := varRootObj(info, e); o != nil {
		return o
	}
	return nil
}

// derivedKey links the result of an untraced (stdlib/module) call to its
// single variable-rooted argument, when unambiguous: filepath.Dir(absFile)
// is "derived from" absFile, so tracking flows through it. Calls mixing
// several variables (fmt.Sprintf("%s:%d", file, line)) yield nothing —
// merging them would create false hubs. Error-producing calls are excluded
// (fmt.Errorf("… %s", dir) describes dir, it doesn't carry it), as are
// method receivers (x.Pos() would bridge every AST node in this codebase).
func (a *analyzer) derivedKey(info *types.Info, call *ast.CallExpr, fn *types.Func) any {
	res := fn.Signature().Results()
	if res.Len() == 1 && isErrorType(res.At(0).Type()) {
		return nil
	}
	var keys []any
	for _, arg := range call.Args {
		k := a.aliasKey(info, arg)
		if k == nil {
			continue
		}
		dup := false
		for _, have := range keys {
			if a.find(have) == a.find(k) {
				dup = true
				break
			}
		}
		if !dup {
			keys = append(keys, k)
		}
	}
	if len(keys) == 1 {
		return keys[0]
	}
	return nil
}

// isErrorType reports whether t is exactly the built-in error type.
func isErrorType(t types.Type) bool {
	if t == nil {
		return false
	}
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

// varRootObj resolves an expression to the variable or struct field it
// denotes, for propagation tracking.
func varRootObj(info *types.Info, e ast.Expr) types.Object {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		obj := info.Uses[x]
		if obj == nil {
			obj = info.Defs[x]
		}
		if v, ok := obj.(*types.Var); ok {
			return v
		}
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return varRootObj(info, x.X)
		}
	case *ast.StarExpr:
		return varRootObj(info, x.X)
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[x]; ok && sel.Kind() == types.FieldVal {
			return sel.Obj()
		}
	}
	return nil
}

// varID returns the stable ID of a variable's alias class, allocating one
// on first use. Two variables connected by argument passing, assignment or
// return share an ID.
func (a *analyzer) varID(obj types.Object) int {
	root := a.find(obj)
	if id, ok := a.varIDs[root]; ok {
		return id
	}
	id := len(a.varIDs) + 1
	a.varIDs[root] = id
	return id
}

// find/union implement union-find over channel alias keys, connecting the
// two ends of a channel across argument passing, returns and assignments.
func (a *analyzer) find(k any) any {
	if k == nil {
		return nil
	}
	p, ok := a.aliasParent[k]
	if !ok {
		return k
	}
	r := a.find(p)
	a.aliasParent[k] = r
	return r
}

func (a *analyzer) union(x, y any) {
	if x == nil || y == nil {
		return
	}
	rx, ry := a.find(x), a.find(y)
	if rx != ry {
		a.aliasParent[rx] = ry
	}
}

// chanRootObj resolves a channel expression to a stable object identity:
// the variable holding it, or the struct field it lives in. Two operations
// on the same variable/field are treated as endpoints of the same channel.
// Returns nil for dynamic expressions (e.g. a channel returned by a call).
func chanRootObj(info *types.Info, e ast.Expr) types.Object {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if o := info.Uses[x]; o != nil {
			return o
		}
		return info.Defs[x]
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[x]; ok {
			return sel.Obj()
		}
		return info.Uses[x.Sel]
	case *ast.IndexExpr:
		// Channel stored in a map/slice: identify by the container.
		return chanRootObj(info, x.X)
	case *ast.StarExpr:
		return chanRootObj(info, x.X)
	}
	return nil
}

// chanPeers returns the other end(s) of a channel: for a send, the module's
// receives on the same channel alias class; for a receive, the sends and
// closes.
func (a *analyzer) chanPeers(key any, kind chanOpKind, selfPos token.Pos) []chanOp {
	if key == nil {
		return nil
	}
	root := a.find(key)
	var want []chanOpKind
	switch kind {
	case chanSend:
		want = []chanOpKind{chanRecv}
	case chanRecv:
		want = []chanOpKind{chanSend, chanClose}
	case chanClose:
		want = []chanOpKind{chanRecv}
	}
	var out []chanOp
	for _, op := range a.chanOps {
		if op.key == nil || a.find(op.key) != root || op.pos == selfPos {
			continue
		}
		if slices.Contains(want, op.kind) {
			out = append(out, op)
		}
	}
	return out
}

// chanEvent emits a trace node for a channel operation, listing the
// opposite endpoints found anywhere in the module.
func (a *analyzer) chanEvent(p *packages.Package, kind chanOpKind, ch ast.Expr, val ast.Expr, pos token.Pos, parent *node) {
	var nodeKind string
	var spans []span
	switch kind {
	case chanSend:
		nodeKind = "chan-send"
		spans = append(a.exprSpans(p, ch), span{T: " <- "})
		if val != nil {
			spans = append(spans, a.exprSpans(p, val)...)
		}
	case chanRecv:
		nodeKind = "chan-recv"
		spans = append([]span{{T: "<-"}}, a.exprSpans(p, ch)...)
	case chanClose:
		nodeKind = "chan-close"
		spans = append(append([]span{{T: "close("}}, a.exprSpans(p, ch)...), span{T: ")"})
	}
	n := parent.add(nodeWithSpans(a.relPos(pos), nodeKind, "", truncateSpans(spans, 80)))

	key := a.chanKey(p.TypesInfo, ch)
	if key == nil {
		n.note("peers unknown (dynamic channel expression)")
		return
	}
	peers := a.chanPeers(key, kind, pos)
	if len(peers) == 0 {
		role := "readers"
		if kind == chanRecv {
			role = "writers"
		}
		n.note("no %s found in module", role)
		return
	}
	for _, peer := range peers {
		var role string
		switch peer.kind {
		case chanSend:
			role = "writer"
		case chanClose:
			role = "closed by"
		default:
			role = "reader"
		}
		n.add(&node{Pos: a.relPos(peer.pos), Kind: "peer", Text: role + ": " + peer.fn})
	}
}

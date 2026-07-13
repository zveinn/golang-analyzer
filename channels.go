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
	kind chanOpKind
	key  any // alias key of the channel expression; nil if dynamic
	pos  token.Pos
	fn   string // enclosing function, e.g. "main.worker"
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
	a.chanOps = append(a.chanOps, chanOp{
		kind: kind,
		key:  a.chanKey(p.TypesInfo, ch),
		pos:  pos,
		fn:   a.enclosingFuncName(p, f, pos),
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
func (a *analyzer) chanEvent(p *packages.Package, kind chanOpKind, ch ast.Expr, valText string, pos token.Pos, parent *node) {
	var text string
	switch kind {
	case chanSend:
		text = exprStr(ch) + " <- " + valText + "  [CHAN SEND]"
	case chanRecv:
		text = "<-" + exprStr(ch) + "  [CHAN RECV]"
	case chanClose:
		text = "close(" + exprStr(ch) + ")  [CHAN CLOSE]"
	}
	n := parent.add(&node{pos: a.relPos(pos), text: text})

	key := a.chanKey(p.TypesInfo, ch)
	if key == nil {
		n.addf("↳ peers unknown (dynamic channel expression)")
		return
	}
	peers := a.chanPeers(key, kind, pos)
	if len(peers) == 0 {
		role := "readers"
		if kind == chanRecv {
			role = "writers"
		}
		n.addf("↳ no %s found in module", role)
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
		n.addp(a.relPos(peer.pos), "↳ %s: %s", role, peer.fn)
	}
}

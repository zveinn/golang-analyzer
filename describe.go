package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const maxExprLen = 48

// exprStr renders an expression as compact, single-line source text.
func exprStr(e ast.Expr) string {
	s := types.ExprString(e)
	if strings.ContainsAny(s, "\n\t") {
		s = strings.Join(strings.Fields(s), " ")
	}
	if len(s) > maxExprLen {
		s = s[:maxExprLen-1] + "…"
	}
	return s
}

func (a *analyzer) relPos(pos token.Pos) string {
	if !pos.IsValid() {
		return ""
	}
	p := a.fset.Position(pos)
	if rel, err := filepath.Rel(a.cwd, p.Filename); err == nil && !strings.HasPrefix(rel, "..") {
		return fmt.Sprintf("%s:%d", rel, p.Line)
	}
	// Outside the working tree (module cache, GOROOT): keep it short.
	dir := filepath.Base(filepath.Dir(p.Filename))
	return fmt.Sprintf("…/%s/%s:%d", dir, filepath.Base(p.Filename), p.Line)
}

// annotateArg adds an "arg X ← origin" child describing where a call
// argument was allocated or produced. Per the tracing rules, this is only
// done for variables that appear as call parameters.
func (a *analyzer) annotateArg(p *packages.Package, arg ast.Expr, parent *node) {
	e := ast.Unparen(arg)
	prefix := ""
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		prefix = "&"
		e = ast.Unparen(u.X)
	}
	switch x := e.(type) {
	case *ast.Ident:
		if x.Name == "_" {
			return
		}
		switch obj := p.TypesInfo.Uses[x].(type) {
		case *types.Var:
			if desc, at, ok := a.describeVarOrigin(obj); ok {
				parent.add(&node{Pos: at, Kind: "arg",
					Text: prefix + x.Name + " ← " + desc})
			}
		case *types.Func:
			parent.add(&node{Pos: a.relPos(obj.Pos()), Kind: "arg", Label: a.classify(obj.Pkg()),
				Text: x.Name + " ← function reference"})
		}
	case *ast.FuncLit:
		parent.add(&node{Pos: a.relPos(x.Pos()), Kind: "arg",
			Text: prefix + "func{…} ← function literal"})
	case *ast.SelectorExpr:
		if sel, ok := p.TypesInfo.Selections[x]; ok && sel.Kind() == types.FieldVal {
			parent.add(&node{Pos: a.relPos(sel.Obj().Pos()), Kind: "arg",
				Text: prefix + exprStr(x) + " ← struct field " + sel.Obj().Name()})
		}
	}
}

// describeVarOrigin explains where a variable came from: parameter,
// make/new, composite literal, call result, etc.
func (a *analyzer) describeVarOrigin(v *types.Var) (desc, at string, ok bool) {
	site, found := a.defs[v]
	if !found {
		return "", "", false
	}
	at = a.relPos(site.node.Pos())
	switch d := site.node.(type) {
	case *ast.Field:
		if v.IsField() {
			return "struct field " + v.Name(), at, true
		}
		return fmt.Sprintf("parameter %q of %s", v.Name(),
			a.enclosingFuncName(site.pkg, site.file, site.node.Pos())), at, true
	case *ast.AssignStmt:
		return a.describeAssignRHS(site.pkg, d, v), at, true
	case *ast.ValueSpec:
		if len(d.Values) == 0 {
			return "var declaration (zero value " + types.TypeString(v.Type(), types.RelativeTo(v.Pkg())) + ")", at, true
		}
		for i, name := range d.Names {
			if name.Name == v.Name() && i < len(d.Values) {
				return a.describeRHS(site.pkg, d.Values[i]), at, true
			}
		}
		return a.describeRHS(site.pkg, d.Values[0]), at, true
	case *ast.RangeStmt:
		return "range iteration variable over " + exprStr(d.X), at, true
	case *ast.TypeSwitchStmt:
		return "type switch binding", at, true
	}
	return "", "", false
}

func (a *analyzer) describeAssignRHS(p *packages.Package, as *ast.AssignStmt, v *types.Var) string {
	idx := -1
	for i, lhs := range as.Lhs {
		if id, ok := ast.Unparen(lhs).(*ast.Ident); ok {
			if def, isDef := p.TypesInfo.Defs[id]; isDef && def == v {
				idx = i
				break
			}
			if use := p.TypesInfo.Uses[id]; use == v {
				idx = i
				break
			}
		}
	}
	if len(as.Rhs) == len(as.Lhs) && idx >= 0 {
		return a.describeRHS(p, as.Rhs[idx])
	}
	// x, y := f() — one call producing multiple results.
	if idx >= 0 {
		return fmt.Sprintf("result #%d of %s", idx+1, exprStr(as.Rhs[0]))
	}
	return a.describeRHS(p, as.Rhs[0])
}

func (a *analyzer) describeRHS(p *packages.Package, e ast.Expr) string {
	switch x := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		if b, ok := typeutil.Callee(p.TypesInfo, x).(*types.Builtin); ok {
			switch b.Name() {
			case "make", "new", "append":
				return "allocated by " + exprStr(x)
			}
		}
		return "result of call " + exprStr(x.Fun) + "(…)"
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return "pointer allocation &" + exprStr(x.X)
		}
		if x.Op == token.ARROW {
			return "received from channel " + exprStr(x.X)
		}
	case *ast.CompositeLit:
		return "composite literal " + exprStr(x)
	case *ast.BasicLit:
		return "literal " + x.Value
	case *ast.FuncLit:
		return "function literal"
	case *ast.Ident:
		return "copy of " + x.Name
	}
	return "expression " + exprStr(e)
}

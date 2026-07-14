package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const maxExprLen = 48

// fileSrc returns (and caches) the raw contents of a source file.
func (a *analyzer) fileSrc(name string) []byte {
	if b, ok := a.src[name]; ok {
		return b
	}
	b, err := os.ReadFile(name)
	if err != nil {
		b = nil
	}
	a.src[name] = b
	return b
}

// srcRange returns the exact source text between two positions, or "" if
// unavailable.
func (a *analyzer) srcRange(start, end token.Pos) string {
	if !start.IsValid() || !end.IsValid() {
		return ""
	}
	f := a.fset.File(start)
	if f == nil || a.fset.File(end) != f {
		return ""
	}
	src := a.fileSrc(f.Name())
	so, eo := f.Offset(start), f.Offset(end)
	if src == nil || so < 0 || eo > len(src) || so >= eo {
		return ""
	}
	return string(src[so:eo])
}

// spansForRange slices the source text of [start,end) into spans, marking
// every identifier under roots that resolves to a variable or struct field
// with its alias-class ID.
func (a *analyzer) spansForRange(p *packages.Package, start, end token.Pos, roots ...ast.Node) []span {
	text := a.srcRange(start, end)
	if text == "" {
		return nil
	}
	type mark struct{ s, e, id int }
	var marks []mark
	base := int(start)
	for _, root := range roots {
		if root == nil {
			continue
		}
		ast.Inspect(root, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || id.Name == "_" {
				return true
			}
			obj := p.TypesInfo.Uses[id]
			if obj == nil {
				obj = p.TypesInfo.Defs[id]
			}
			if v, ok := obj.(*types.Var); ok {
				marks = append(marks, mark{int(id.Pos()) - base, int(id.End()) - base, a.varID(v)})
			}
			return true
		})
	}
	slices.SortFunc(marks, func(x, y mark) int { return x.s - y.s })
	var out []span
	prev := 0
	for _, m := range marks {
		if m.s < prev || m.e > len(text) {
			continue
		}
		if m.s > prev {
			out = append(out, span{T: flattenWS(text[prev:m.s])})
		}
		out = append(out, span{T: text[m.s:m.e], V: m.id})
		prev = m.e
	}
	if prev < len(text) {
		out = append(out, span{T: flattenWS(text[prev:])})
	}
	return out
}

// exprSpans renders an expression as source text with variable occurrences
// marked. Falls back to a plain unmarked span if the source is unavailable.
func (a *analyzer) exprSpans(p *packages.Package, e ast.Expr) []span {
	if out := a.spansForRange(p, e.Pos(), e.End(), e); out != nil {
		return out
	}
	return []span{{T: exprStr(e)}}
}

// flattenWS collapses whitespace runs (multi-line expressions) to single
// spaces.
func flattenWS(s string) string {
	if !strings.ContainsAny(s, "\n\t") {
		return s
	}
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\t' || r == '\r' })
	out := strings.Join(fields, " ")
	if strings.HasPrefix(s, "\n") || strings.HasPrefix(s, "\t") {
		out = " " + out
	}
	if strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\t") {
		out += " "
	}
	return out
}

// truncateSpans caps the total rendered length, never splitting a variable
// span in half.
func truncateSpans(spans []span, budget int) []span {
	total := 0
	for i, s := range spans {
		if total+len(s.T) > budget {
			out := slices.Clone(spans[:i])
			if keep := budget - total; keep > 3 && s.V == 0 {
				out = append(out, span{T: strings.ToValidUTF8(s.T[:keep], "")})
			}
			return append(out, span{T: "…"})
		}
		total += len(s.T)
	}
	return spans
}

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
				spans := append([]span{{T: prefix}, {T: x.Name, V: a.varID(obj)}, {T: " ← "}}, desc...)
				parent.add(nodeWithSpans(at, "arg", "", spans))
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
			spans := append(a.exprSpans(p, x), span{T: " ← struct field " + sel.Obj().Name()})
			parent.add(nodeWithSpans(a.relPos(sel.Obj().Pos()), "arg", "", spans))
		}
	}
}

// annotateReturn adds an "X ← origin" child under a return row for each
// returned variable, describing where it was allocated — mirroring
// annotateArg. A bare `return` uses the enclosing function's named results.
func (a *analyzer) annotateReturn(p *packages.Package, x *ast.ReturnStmt, parent *node) {
	if len(x.Results) > 0 {
		for _, e := range x.Results {
			a.annotateArg(p, e, parent)
		}
		return
	}
	var results *ast.FieldList
	if n := len(a.resultStack); n > 0 {
		results = a.resultStack[n-1]
	}
	if results == nil {
		return
	}
	for _, field := range results.List {
		for _, name := range field.Names {
			if name.Name == "_" {
				continue
			}
			v, ok := p.TypesInfo.Defs[name].(*types.Var)
			if !ok {
				continue
			}
			if desc, at, ok := a.describeVarOrigin(v); ok {
				spans := append([]span{{T: name.Name, V: a.varID(v)}, {T: " ← "}}, desc...)
				parent.add(nodeWithSpans(at, "arg", "", spans))
			}
		}
	}
}

// describeVarOrigin explains where a variable came from: parameter,
// make/new, composite literal, call result, etc. The description is
// returned as spans so variables inside it stay trackable.
func (a *analyzer) describeVarOrigin(v *types.Var) (desc []span, at string, ok bool) {
	site, found := a.defs[v]
	if !found {
		return nil, "", false
	}
	at = a.relPos(site.node.Pos())
	switch d := site.node.(type) {
	case *ast.Field:
		if v.IsField() {
			return []span{{T: "struct field " + v.Name()}}, at, true
		}
		// A named result is worth labeling (it's the function's declared
		// output), but a plain parameter's provenance is already conveyed by
		// the variable's tracking color — it links to the same-colored
		// argument at the call site — so we don't spell out "parameter X of …".
		if fd := enclosingFuncDecl(site.file, d.Pos()); fd != nil && fd.Type.Results != nil &&
			d.Pos() >= fd.Type.Results.Pos() && d.End() <= fd.Type.Results.End() {
			return []span{{T: fmt.Sprintf("named result %q", v.Name())}}, at, true
		}
		return nil, "", false
	case *ast.AssignStmt:
		return a.describeAssignRHS(site.pkg, d, v), at, true
	case *ast.ValueSpec:
		if len(d.Values) == 0 {
			return []span{{T: "var declaration (zero value " +
				types.TypeString(v.Type(), types.RelativeTo(v.Pkg())) + ")"}}, at, true
		}
		for i, name := range d.Names {
			if name.Name == v.Name() && i < len(d.Values) {
				return a.describeRHS(site.pkg, d.Values[i]), at, true
			}
		}
		return a.describeRHS(site.pkg, d.Values[0]), at, true
	case *ast.RangeStmt:
		return append([]span{{T: "range iteration variable over "}},
			truncateSpans(a.exprSpans(site.pkg, d.X), 40)...), at, true
	case *ast.TypeSwitchStmt:
		return []span{{T: "type switch binding"}}, at, true
	}
	return nil, "", false
}

func (a *analyzer) describeAssignRHS(p *packages.Package, as *ast.AssignStmt, v *types.Var) []span {
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
		return append([]span{{T: fmt.Sprintf("result #%d of ", idx+1)}},
			truncateSpans(a.exprSpans(p, as.Rhs[0]), 45)...)
	}
	return a.describeRHS(p, as.Rhs[0])
}

func (a *analyzer) describeRHS(p *packages.Package, e ast.Expr) []span {
	with := func(head string, expr ast.Expr, budget int) []span {
		return append([]span{{T: head}}, truncateSpans(a.exprSpans(p, expr), budget)...)
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		if b, ok := typeutil.Callee(p.TypesInfo, x).(*types.Builtin); ok {
			switch b.Name() {
			case "make", "new", "append":
				return with("allocated by ", x, 50)
			}
		}
		return append(with("result of call ", x.Fun, 40), span{T: "(…)"})
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return with("pointer allocation ", x, 50)
		}
		if x.Op == token.ARROW {
			return with("received from channel ", x.X, 40)
		}
	case *ast.CompositeLit:
		return with("composite literal ", x, 50)
	case *ast.BasicLit:
		return []span{{T: "literal " + x.Value}}
	case *ast.FuncLit:
		return []span{{T: "function literal"}}
	case *ast.Ident:
		return with("copy of ", x, 40)
	}
	return with("expression ", e, 50)
}

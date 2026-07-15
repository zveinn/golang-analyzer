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


package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	defaultMaxDepth = 40
	maxNodes        = 200000
)

type analyzer struct {
	fset      *token.FileSet
	pkgs      []*packages.Package
	localPkgs map[*types.Package]bool
	modPath   string
	cwd       string

	// funcs maps a (generic-origin) function object to its declaration.
	funcs map[*types.Func]funcDef
	// defs maps a variable to the syntax that defines it, for allocation tracing.
	defs map[types.Object]defSite
	// chanOps is every channel send/recv/close/range in the module.
	chanOps []chanOp
	// aliasParent is union-find state connecting channel aliases across
	// argument passing, returns and assignments.
	aliasParent map[any]any
	// named is every non-generic named type in the module, for interface dispatch.
	named []*types.Named

	stack     []*types.Func
	litDepth  int
	nodeCount int
	truncated bool

	// maxDepth and expandAll are tunable per request ("depth" and "expand"
	// parameters on the TCP intake).
	maxDepth  int
	expandAll bool

	// expandedAt records where each function body was first expanded in the
	// trace; later call sites reference it instead of re-printing the body,
	// so shared helpers (and their loops) appear exactly once.
	expandedAt   map[*types.Func]string
	expandedLits map[*ast.FuncLit]string
}

type funcDef struct {
	decl *ast.FuncDecl
	pkg  *packages.Package
}

type defSite struct {
	node ast.Node
	file *ast.File
	pkg  *packages.Package
}

type target struct {
	fn  *types.Func
	def funcDef
}

func newAnalyzer(absFile string) (*analyzer, error) {
	modRoot, err := findModuleRoot(filepath.Dir(absFile))
	if err != nil {
		return nil, err
	}
	cwd, _ := os.Getwd()
	a := &analyzer{
		fset:         token.NewFileSet(),
		localPkgs:    map[*types.Package]bool{},
		funcs:        map[*types.Func]funcDef{},
		defs:         map[types.Object]defSite{},
		aliasParent:  map[any]any{},
		expandedAt:   map[*types.Func]string{},
		expandedLits: map[*ast.FuncLit]string{},
		maxDepth:     defaultMaxDepth,
		cwd:          cwd,
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
			packages.NeedDeps | packages.NeedModule,
		Dir:  modRoot,
		Fset: a.fset,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loading packages: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found under %s", modRoot)
	}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			fmt.Fprintf(os.Stderr, "warning: %v\n", e)
		}
	}
	a.pkgs = pkgs
	for _, p := range pkgs {
		if p.Types != nil {
			a.localPkgs[p.Types] = true
		}
		if a.modPath == "" && p.Module != nil {
			a.modPath = p.Module.Path
		}
	}
	a.buildIndexes()
	return a, nil
}

// applyParams applies request parameters received on the TCP intake.
func (a *analyzer) applyParams(params map[string]string) error {
	for k, v := range params {
		switch k {
		case "depth":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return fmt.Errorf("invalid depth %q (want a positive integer)", v)
			}
			a.maxDepth = n
		case "expand":
			switch v {
			case "all":
				a.expandAll = true
			case "once":
				a.expandAll = false
			default:
				return fmt.Errorf(`invalid expand %q (want "all" or "once")`, v)
			}
		default:
			return fmt.Errorf("unknown parameter %q (supported: depth, expand)", k)
		}
	}
	return nil
}

func findModuleRoot(dir string) (string, error) {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("no go.mod found above %s", dir)
		}
		d = parent
	}
}

func (a *analyzer) buildIndexes() {
	for _, p := range a.pkgs {
		// Named types for interface dispatch resolution.
		if p.Types != nil {
			scope := p.Types.Scope()
			for _, name := range scope.Names() {
				tn, ok := scope.Lookup(name).(*types.TypeName)
				if !ok || tn.IsAlias() {
					continue
				}
				if n, ok := tn.Type().(*types.Named); ok {
					a.named = append(a.named, n)
				}
			}
		}
		for _, f := range p.Syntax {
			a.indexFile(p, f)
		}
	}
}

func (a *analyzer) indexFile(p *packages.Package, f *ast.File) {
	info := p.TypesInfo

	// Function declarations.
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn, ok := info.Defs[fd.Name].(*types.Func); ok {
			a.funcs[fn] = funcDef{decl: fd, pkg: p}
		}
	}

	// Variable definition sites (for allocation tracing) and channel ops.
	// The stack tracks ancestry so each defined ident can be tied to its
	// enclosing statement/field.
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch x := n.(type) {
		case *ast.Ident:
			if v, ok := info.Defs[x].(*types.Var); ok {
				if site := nearestDefSite(stack); site != nil {
					a.defs[v] = defSite{node: site, file: f, pkg: p}
				}
			}
		case *ast.SendStmt:
			a.recordChanOp(p, f, chanSend, x.Chan, x.Arrow)
		case *ast.UnaryExpr:
			if x.Op == token.ARROW {
				a.recordChanOp(p, f, chanRecv, x.X, x.OpPos)
			}
		case *ast.RangeStmt:
			if isChanType(info.TypeOf(x.X)) {
				a.recordChanOp(p, f, chanRecv, x.X, x.For)
			}
		case *ast.CallExpr:
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && id.Name == "close" {
				if _, isBuiltin := info.Uses[id].(*types.Builtin); isBuiltin && len(x.Args) == 1 {
					a.recordChanOp(p, f, chanClose, x.Args[0], x.Pos())
				}
			}
			// Channel argument → callee parameter aliasing.
			if fn, ok := typeutil.Callee(info, x).(*types.Func); ok {
				sig := fn.Origin().Signature()
				for i, arg := range x.Args {
					if i >= sig.Params().Len() {
						break
					}
					if isChanType(info.TypeOf(arg)) {
						a.union(a.chanKey(info, arg), sig.Params().At(i))
					}
				}
			}
		case *ast.AssignStmt:
			a.assignAliases(info, x.Lhs, x.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, len(x.Names))
			for i, name := range x.Names {
				lhs[i] = name
			}
			a.assignAliases(info, lhs, x.Values)
		case *ast.ReturnStmt:
			a.returnAliases(info, stack, x)
		case *ast.CompositeLit:
			a.compositeAliases(info, x)
		}
		stack = append(stack, n)
		return true
	})
}

// assignAliases unions channel identities across assignments, including
// multi-value binds from a single call: ch := makeCh() / a, b := twoCh().
func (a *analyzer) assignAliases(info *types.Info, lhs, rhs []ast.Expr) {
	if len(rhs) == 1 && len(lhs) > 1 {
		if call, ok := ast.Unparen(rhs[0]).(*ast.CallExpr); ok {
			if fn, ok := typeutil.Callee(info, call).(*types.Func); ok {
				for i, l := range lhs {
					if isChanType(info.TypeOf(l)) {
						a.union(chanRootObj(info, l), resultKey{fn: fn.Origin(), idx: i})
					}
				}
			}
		}
		return
	}
	for i := range min(len(lhs), len(rhs)) {
		if isChanType(info.TypeOf(rhs[i])) {
			a.union(chanRootObj(info, lhs[i]), a.chanKey(info, rhs[i]))
		}
	}
}

// returnAliases unions "result #i of fn" with the channel expressions the
// function actually returns. Returns inside function literals are skipped —
// they belong to the literal, not the declared function.
func (a *analyzer) returnAliases(info *types.Info, stack []ast.Node, ret *ast.ReturnStmt) {
	for i := len(stack) - 1; i >= 0; i-- {
		switch d := stack[i].(type) {
		case *ast.FuncLit:
			return
		case *ast.FuncDecl:
			fn, ok := info.Defs[d.Name].(*types.Func)
			if !ok {
				return
			}
			for ri, res := range ret.Results {
				if isChanType(info.TypeOf(res)) {
					a.union(resultKey{fn: fn.Origin(), idx: ri}, a.chanKey(info, res))
				}
			}
			return
		}
	}
}

// compositeAliases unions struct fields with the channels they are
// initialized to in composite literals.
func (a *analyzer) compositeAliases(info *types.Info, lit *ast.CompositeLit) {
	t := info.TypeOf(lit)
	if t == nil {
		return
	}
	st, _ := t.Underlying().(*types.Struct)
	for i, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if !isChanType(info.TypeOf(kv.Value)) {
				continue
			}
			if id, ok := kv.Key.(*ast.Ident); ok {
				a.union(info.Uses[id], a.chanKey(info, kv.Value))
			}
		} else if st != nil && i < st.NumFields() && isChanType(info.TypeOf(elt)) {
			a.union(st.Field(i), a.chanKey(info, elt))
		}
	}
}

// nearestDefSite walks up the ancestry to the statement or field that
// defines a variable.
func nearestDefSite(stack []ast.Node) ast.Node {
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i].(type) {
		case *ast.AssignStmt, *ast.ValueSpec, *ast.Field,
			*ast.RangeStmt, *ast.TypeSwitchStmt:
			return stack[i]
		}
	}
	return nil
}

func isChanType(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

// findFunc locates the function declaration spanning the given file:line.
func (a *analyzer) findFunc(absFile string, line int) (*target, error) {
	for _, p := range a.pkgs {
		for _, f := range p.Syntax {
			tf := a.fset.File(f.Pos())
			if tf == nil || tf.Name() != absFile {
				continue
			}
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				start := a.fset.Position(fd.Pos()).Line
				end := a.fset.Position(fd.End()).Line
				if line < start || line > end {
					continue
				}
				fn, ok := p.TypesInfo.Defs[fd.Name].(*types.Func)
				if !ok {
					return nil, fmt.Errorf("no type information for %s", fd.Name.Name)
				}
				return &target{fn: fn, def: funcDef{decl: fd, pkg: p}}, nil
			}
			return nil, fmt.Errorf("no function declaration spans %s:%d", absFile, line)
		}
	}
	return nil, fmt.Errorf("%s is not part of the loaded module (root: %s)", absFile, a.modPath)
}

// enclosingFuncName names the function containing pos, for channel endpoint
// reporting. Nested function literals are suffixed with ".func".
func (a *analyzer) enclosingFuncName(p *packages.Package, f *ast.File, pos token.Pos) string {
	name := p.Name + ".<init>"
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if pos < n.Pos() || pos > n.End() {
			return false
		}
		switch d := n.(type) {
		case *ast.FuncDecl:
			name = p.Name + "." + funcDeclName(d)
		case *ast.FuncLit:
			name += ".func"
		}
		return true
	})
	return name
}

func funcDeclName(d *ast.FuncDecl) string {
	if d.Recv != nil && len(d.Recv.List) > 0 {
		return "(" + exprStr(d.Recv.List[0].Type) + ")." + d.Name.Name
	}
	return d.Name.Name
}

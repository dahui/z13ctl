package daemon

// astguard_test.go — the parser behind the guards that are rules about where
// a call may appear (undervolt resets gated on applied, the SMU probe only in
// writers, platform_profile writes only through the recorder). They used to
// scan source lines with strings.Contains and strip "//" by hand, which read a
// call inside a string or a block comment as code and depended on line
// layout; parsing makes comments and strings irrelevant by construction.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// callSite is one call found by findCalls.
type callSite struct {
	pos  token.Position
	fn   *ast.FuncDecl // the function the call is in; nil at package level
	call *ast.CallExpr
}

// fnName is the enclosing function as "Recv.name" or "name".
func (c callSite) fnName() string {
	if c.fn == nil {
		return ""
	}
	if c.fn.Recv != nil && len(c.fn.Recv.List) == 1 {
		t := types.ExprString(c.fn.Recv.List[0].Type)
		return strings.TrimPrefix(t, "*") + "." + c.fn.Name.Name
	}
	return c.fn.Name.Name
}

// callsBefore reports whether the enclosing function calls any of names
// (matched as for findCalls) before this site.
func (c callSite) callsBefore(names ...string) bool {
	if c.fn == nil || c.fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(c.fn.Body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if ok && ce.Pos() < c.call.Pos() && matchesAny(ce.Fun, names) {
			found = true
		}
		return !found
	})
	return found
}

// matchesAny reports whether the called expression is one of names: "f"
// matches f() and x.f(), "X.f" matches x.X.f() and X.f().
func matchesAny(fun ast.Expr, names []string) bool {
	s := types.ExprString(fun)
	for _, n := range names {
		if s == n || strings.HasSuffix(s, "."+n) {
			return true
		}
	}
	return false
}

// findCalls parses every non-test Go file in dirs and returns each call to
// any of names, with the function it sits in.
func findCalls(t *testing.T, dirs []string, names ...string) []callSite {
	t.Helper()
	var out []callSite
	fset := token.NewFileSet()
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f, err := parser.ParseFile(fset, path, src, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", path, err)
			}
			for _, decl := range f.Decls {
				fn, _ := decl.(*ast.FuncDecl)
				ast.Inspect(decl, func(n ast.Node) bool {
					if ce, ok := n.(*ast.CallExpr); ok && matchesAny(ce.Fun, names) {
						out = append(out, callSite{pos: fset.Position(ce.Pos()), fn: fn, call: ce})
					}
					return true
				})
			}
		}
	}
	return out
}

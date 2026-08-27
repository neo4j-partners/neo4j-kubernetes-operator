package oracle

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The compiler guarantees one direction: no reason reaches a condition or an Event unless it is
// declared here. These tests cover the other one, which no compiler can see — a row that survives
// in the catalog and the documentation after the code that emitted it is gone.
func TestEveryCatalogedReasonIsEmitted(t *testing.T) {
	declared := declaredReasonIdents(t)
	used := oracleSelectorsUsedInProduction(t)

	for ident, value := range declared {
		if want := "Reason" + value; ident != want {
			t.Errorf("%s declares reason %q — name it %s so the projections and the greps line up", ident, value, want)
		}
		if !used[ident] {
			t.Errorf("oracle.%s is catalogued and documented but no production code emits it — delete the row or emit it", ident)
		}
	}
}

// The EventRecorder API takes a plain string, so it is the one door the type cannot close.
func TestNoRawReasonInEventCalls(t *testing.T) {
	forEachProductionFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "Event" && sel.Sel.Name != "Eventf") || len(call.Args) < 3 {
				return true
			}
			if lit, ok := call.Args[2].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				t.Errorf("%s: Event reason %s is a literal — declare it in internal/oracle and pass oracle.Reason",
					fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	})
}

// declaredReasonIdents maps each exported Reason variable of catalog.go to the reason it declares.
func declaredReasonIdents(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	path := filepath.Join(moduleRoot(t), "src", "internal", "oracle", "catalog.go")
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse catalog: %v", err)
	}

	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			name := value.Names[0].Name
			if !strings.HasPrefix(name, "Reason") {
				continue
			}
			call, ok := value.Values[0].(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				continue
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			unquoted, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			out[name] = unquoted
		}
	}
	if len(out) == 0 {
		t.Fatal("no reason declaration found in catalog.go — has the declare() shape changed?")
	}
	return out
}

// oracleSelectorsUsedInProduction collects every oracle.X referenced outside this package, tests
// excluded: a reason only a test mentions is a reason no user will ever see.
func oracleSelectorsUsedInProduction(t *testing.T) map[string]bool {
	used := map[string]bool{}
	forEachProductionFile(t, func(_ string, file *ast.File, _ *token.FileSet) {
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "oracle" {
				used[sel.Sel.Name] = true
			}
			return true
		})
	})
	return used
}

func forEachProductionFile(t *testing.T, visit func(path string, file *ast.File, fset *token.FileSet)) {
	t.Helper()
	root := moduleRoot(t)
	srcDir := filepath.Join(root, "src")
	oracleDir := filepath.Join(srcDir, "internal", "oracle")
	fset := token.NewFileSet()

	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == oracleDir {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		visit(path, file, fset)
		return nil
	})
	if err != nil {
		t.Fatalf("walk src: %v", err)
	}
}

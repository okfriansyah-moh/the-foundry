package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// checkTestSourceWrite implements docs/PLAN.md Task 131 (DOC-01): no test may
// write into a package source directory (the class of bug that committed
// internal/spec/mockup/data/visual-* fixture trees).
func checkTestSourceWrite(roots []string) ([]string, error) {
	var violations []string
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if skipDirNames[info.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel := filepath.ToSlash(strings.TrimPrefix(path, "./"))
			scanSeeds := strings.Contains(rel, "test/fitness_seeds/")
			if !scanSeeds && !strings.HasPrefix(rel, "internal/") && !strings.HasPrefix(rel, "cmd/") {
				return nil
			}

			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return fmt.Errorf("test-source-write: parse %s: %w", path, perr)
			}

			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "os" {
					return true
				}
				switch sel.Sel.Name {
				case "WriteFile", "MkdirAll", "Create", "OpenFile":
				default:
					return true
				}
				if len(call.Args) == 0 {
					return true
				}
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				p := strings.Trim(lit.Value, `"`)
				if isAllowedTestWritePath(p) {
					return true
				}
				pos := fset.Position(call.Pos())
				violations = append(violations, fmt.Sprintf(
					"%s:%d: test writes to relative path %q inside package source tree — use t.TempDir() (docs/PLAN.md Task 131)",
					rel, pos.Line, p))
				return true
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(violations)
	return violations, nil
}

func isAllowedTestWritePath(p string) bool {
	if p == "" {
		return true
	}
	if filepath.IsAbs(p) {
		return true
	}
	if strings.HasPrefix(p, "/tmp/") || strings.HasPrefix(p, "/var/") {
		return true
	}
	// Relative writes into the package tree are the bug class; absolute or
	// temp-root paths are fine.
	return false
}

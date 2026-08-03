package packaging

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

func TestPackagingAndRuntimeAdaptersDoNotImportAuthorityPackages(t *testing.T) {
	forbidden := []string{
		"/internal/kernel",
		"/internal/scm/write",
		"/internal/deploy",
		"/internal/policy/compiler",
	}
	for _, root := range []string{".", "../../adapters/agent-runtime"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				return err
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Decls {
				declaration, ok := spec.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, item := range declaration.Specs {
					importSpec, ok := item.(*ast.ImportSpec)
					if !ok {
						continue
					}
					importPath, err := strconv.Unquote(importSpec.Path.Value)
					if err != nil {
						return err
					}
					for _, banned := range forbidden {
						if strings.Contains(importPath, banned) {
							t.Errorf("%s imports forbidden authority package %s", path, importPath)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

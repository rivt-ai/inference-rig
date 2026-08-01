package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConstructorRule(t *testing.T) {
	constructors := map[string][]string{}
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		return collectConstructors(t, constructors, path, entry, err)
	})
	if err != nil {
		t.Fatal(err)
	}
	for typ, funcs := range constructors {
		if len(funcs) > 1 {
			t.Errorf("%s has multiple exported constructors: %s", typ, strings.Join(funcs, ", "))
		}
	}
}

func collectConstructors(t *testing.T, constructors map[string][]string, path string, entry os.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	if entry.IsDir() {
		return constructorDirResult(entry.Name())
	}
	if !isGoImplementationFile(path) {
		return nil
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return err
	}
	for _, decl := range file.Decls {
		addConstructor(t, constructors, path, file.Name.Name, decl)
	}
	return nil
}

func constructorDirResult(name string) error {
	switch name {
	case ".git", ".idea", "local":
		return filepath.SkipDir
	default:
		return nil
	}
}

func isGoImplementationFile(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}

func addConstructor(t *testing.T, constructors map[string][]string, path string, packageName string, decl ast.Decl) {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "New") || !fn.Name.IsExported() {
		return
	}
	if strings.Contains(fn.Name.Name, "With") {
		t.Errorf("%s: exported constructor %s uses a With variant", path, fn.Name.Name)
	}
	returned := directConcreteReturn(fn)
	if returned == "" {
		return
	}
	key := packageName + "." + returned
	constructors[key] = append(constructors[key], path+":"+fn.Name.Name)
}

func directConcreteReturn(fn *ast.FuncDecl) string {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return ""
	}
	switch expr := fn.Type.Results.List[0].Type.(type) {
	case *ast.Ident:
		if expr.IsExported() {
			return expr.Name
		}
	case *ast.StarExpr:
		if ident, ok := expr.X.(*ast.Ident); ok && ident.IsExported() {
			return ident.Name
		}
	}
	return ""
}

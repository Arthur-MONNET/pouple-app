package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fieldCopyIn returns what copyIn leaves out: the message of connector.Invalid (its first literal is the field id) and
// the help texts of the descriptor fields.
func fieldCopyIn(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	literal := func(expr ast.Expr) string {
		if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			value, _ := strconv.Unquote(lit.Value)
			return value
		}
		return ""
	}
	var found []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			if fun, ok := n.Fun.(*ast.SelectorExpr); ok && fun.Sel.Name == "Invalid" && len(n.Args) >= 2 {
				found = append(found, literal(n.Args[1]))
			}
		case *ast.KeyValueExpr:
			if key, ok := n.Key.(*ast.Ident); ok && (key.Name == "Help" || key.Name == "PatternHint") {
				found = append(found, literal(n.Value))
			}
		}
		return true
	})
	return found
}

// The GitHub connector, its watcher and its context: every English text the user can see must have a French one.
func TestEveryGitHubTextHasAFrenchTranslation(t *testing.T) {
	files := []string{
		"../connectors/github/github.go",
		"../connectors/github/url.go",
		"../connectors/github/client.go",
		"../githubwatch/githubwatch.go",
		"../servicectx/github.go",
	}
	checked := 0
	for _, path := range files {
		for _, text := range append(copyIn(t, path), fieldCopyIn(t, path)...) {
			checked++
			if _, ok := fr[text]; !ok && strings.ContainsAny(text, " ") {
				t.Errorf("%s: no French for %q", filepath.Base(path), text)
			}
		}
	}
	if checked < 30 {
		t.Fatalf("the scan found only %d texts: it no longer reads the files", checked)
	}
}

// Every French text of GitHub must be one the code can ask for, so a rename cannot leave a dead entry behind.
func TestNoFrenchGitHubTextIsOrphaned(t *testing.T) {
	inCode := map[string]bool{}
	for _, pattern := range []string{"../connectors/github/*.go", "../githubwatch/*.go", "../servicectx/*.go"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if !strings.HasSuffix(path, "_test.go") {
				for _, text := range append(copyIn(t, path), fieldCopyIn(t, path)...) {
					inCode[text] = true
				}
			}
		}
	}
	for english := range frGitHub {
		if !inCode[english] {
			t.Errorf("French text that no code asks for: %q", english)
		}
	}
}

package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/providers"
)

// translated lists the functions whose first string argument is app copy: the key of a translation.
var translated = map[string]bool{"T": true, "NewError": true, "NewMessage": true, "Wrap": true, "Invalid": true, "log": true}

// copyFields are the struct fields of descriptors, profiles and suggestions that hold app copy.
var copyFields = map[string]bool{"Tagline": true, "Title": true, "Body": true, "Label": true, "Detail": true, "Subtitle": true}

// copyIn returns the English app copy written in a Go file: the literal passed to the translation functions, and
// the literals of the fields that are shown to the user.
func copyIn(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	literal := func(expr ast.Expr) (string, bool) {
		if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			value, err := strconv.Unquote(lit.Value)
			return value, err == nil && value != ""
		}
		return "", false
	}
	var found []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			name := ""
			switch fun := n.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			}
			if translated[name] {
				for _, arg := range n.Args[:min(len(n.Args), 3)] {
					if value, ok := literal(arg); ok {
						found = append(found, value)
						break
					}
				}
			}
		case *ast.KeyValueExpr:
			if key, ok := n.Key.(*ast.Ident); ok && copyFields[key.Name] {
				if value, ok := literal(n.Value); ok {
					found = append(found, value)
				}
			}
		}
		return true
	})
	return found
}

// The files that carry the copy of the AI providers: every English text in them must have a French one.
func TestEveryProviderTextHasAFrenchTranslation(t *testing.T) {
	files := []string{
		"../connectors/claude/claude.go",
		"../engine/claude.go",
		"../engine/process.go",
		"../engine/providertest.go",
		"../engine/service.go",
		"../engine/flows.go",
		"../engine/outcome.go",
		"../engine/plan.go",
		"../engine/usage.go",
		"../engine/claude_usage.go",
		"../api/providers_api.go",
		"../api/settings.go",
		"../providers/providers.go",
		"../../cmd/pouple-core/assistant.go",
	}
	checked := 0
	for _, path := range files {
		for _, text := range copyIn(t, path) {
			checked++
			// A text without letters, or kept in English on purpose (a product name), needs no translation.
			if _, ok := fr[text]; !ok && strings.ContainsAny(text, " ") {
				t.Errorf("%s: no French for %q", filepath.Base(path), text)
			}
		}
	}
	if checked < 40 {
		t.Fatalf("the scan found only %d texts: it no longer reads the files", checked)
	}
}

// Every French text of the providers must be one the code can ask for, so a rename cannot leave a dead entry behind.
func TestNoFrenchProviderTextIsOrphaned(t *testing.T) {
	inCode := map[string]bool{}
	for _, pattern := range []string{"../*/*.go", "../connectors/*/*.go", "../../cmd/pouple-core/*.go"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			for _, text := range copyIn(t, path) {
				inCode[text] = true
			}
		}
	}
	// The suggestions and the notes are data of the providers package, translated by the API.
	for _, id := range providers.All() {
		inCode[providers.ModelNote(id)] = true
		for _, suggestion := range providers.Suggestions(id) {
			inCode[suggestion.Label] = true
		}
	}
	for english := range frProviders {
		if !inCode[english] {
			t.Errorf("French text that no code asks for: %q", english)
		}
	}
}

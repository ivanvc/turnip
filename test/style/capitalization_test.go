// Package style holds repository-wide checks on how turnip writes text.
//
// Feature: fix-rendered-capitalization, Decision 1.
package style

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// midSentence matches lock, project, operation or plan (singular, plural or
// possessive) capitalized after a lowercase letter and a space or hyphen, or
// after a comma, semicolon or colon and a space.
var midSentence = regexp.MustCompile(`(?:[a-z][ -]|[,;:] )(?:Lock|Project|Operation|Plan)(?:s|'s|\x{2019}s)?\b`)

// logMethods are the slog call names whose arguments are log text, which
// operators read in logs and turnip does not render.
var logMethods = map[string]bool{
	"Debug": true, "Info": true, "Warn": true, "Error": true,
	"DebugContext": true, "InfoContext": true, "WarnContext": true, "ErrorContext": true,
}

// logReceivers are the names turnip calls slog through: the slog package
// itself and the *slog.Logger values held as log or logger. Any other
// receiver, such as status.Error or http.Error, renders its text.
var logReceivers = map[string]bool{"slog": true, "log": true, "logger": true}

// isLogCall reports whether call is a slog call, so its arguments are log text.
func isLogCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !logMethods[sel.Sel.Name] {
		return false
	}
	recv, ok := sel.X.(*ast.Ident)
	return ok && logReceivers[recv.Name]
}

// allowlist names literals that are not rendered text and that the shape
// above cannot tell apart. Each entry: slash path suffix, literal, reason.
var allowlist = []struct{ file, literal, reason string }{
	{
		"internal/orchestrator/carry.go", "completing a Project",
		"retryTrigger's label, only logged and never rendered",
	},
}

type finding struct {
	File    string
	Line    int
	Literal string
}

func allowed(path, lit string) bool {
	for _, a := range allowlist {
		if strings.HasSuffix(filepath.ToSlash(path), a.file) && a.literal == lit {
			return true
		}
	}
	return false
}

// scanDir parses every non-test Go file under dir and returns each string
// literal with a mid-sentence capitalized noun, skipping slog arguments and
// allowlisted literals. Files ending in .go.txt are parsed too, so a fixture
// need not compile.
func scanDir(dir string) ([]finding, error) {
	var out []finding
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		isGo := strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
		if !isGo && !strings.HasSuffix(name, ".go.txt") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		var walk func(n ast.Node, inLog bool)
		walk = func(n ast.Node, inLog bool) {
			ast.Inspect(n, func(c ast.Node) bool {
				switch x := c.(type) {
				case *ast.CallExpr:
					if !inLog && isLogCall(x) {
						for _, a := range x.Args {
							walk(a, true)
						}
						return false
					}
				case *ast.BasicLit:
					if inLog || x.Kind != token.STRING {
						return true
					}
					lit, err := strconv.Unquote(x.Value)
					if err != nil || !midSentence.MatchString(lit) || allowed(path, lit) {
						return true
					}
					out = append(out, finding{path, fset.Position(x.Pos()).Line, lit})
				}
				return true
			})
		}
		walk(f, false)
		return nil
	})
	return out, err
}

func TestMidSentenceMatcher(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"narrower than the whole Project.", true},
		{"The Lock is still held", true},
		{"act on this Project's state", true},
		{"Unlocked the following Projects since", true},
		{"Total number of Operations dispatched", true},
		{"Webhook-to-Operation-result latency", true},
		{"note: Plan failed", true},
		{"Lock released: the project is free", false},
		{"Project web is free", false},
		{"the whole project.", false},
		{"No Job/Pod found for this operation", false},
		{"the Server and the Runner read the Secret", false},
		{"a Planet or Locked door", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, midSentence.MatchString(c.in), c.in)
	}
}

func TestScanFixture(t *testing.T) {
	got, err := scanDir("testdata")
	require.NoError(t, err)
	require.Len(t, got, 3, "the slog and logger messages must be skipped, the other literals reported")
	for _, f := range got {
		assert.Equal(t, "bad.go.txt", filepath.Base(f.File))
	}
	assert.Equal(t, 10, got[0].Line)
	assert.Equal(t, "denied for this Project", got[0].Literal)
	assert.Equal(t, 11, got[1].Line)
	assert.Equal(t, "the Lock is held", got[1].Literal)
	assert.Equal(t, 12, got[2].Line)
	assert.Equal(t, "The result is narrower than the whole Project.", got[2].Literal)
}

func TestRenderedStringsAreLowercase(t *testing.T) {
	var all []string
	for _, root := range []string{"../../internal", "../../cmd"} {
		got, err := scanDir(root)
		require.NoError(t, err)
		for _, f := range got {
			all = append(all, f.File+":"+strconv.Itoa(f.Line)+": "+strconv.Quote(f.Literal))
		}
	}
	assert.Empty(t, all, "rendered text writes lock, project, operation and plan in lowercase mid-sentence:\n%s", strings.Join(all, "\n"))
}

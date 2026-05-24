package tflocalexpand

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpand_Golden(t *testing.T) {
	cases := []struct {
		name   string
		prune  bool
		eval   bool
		only   []string
		except []string
	}{
		{name: "basic"},
		{name: "chained"},
		{name: "interp"},
		{name: "literal-interp"},
		{name: "nested"},
		{name: "no-refs"},
		{name: "undefined-ref"},
		{name: "unresolved"},
		{name: "prune", prune: true},
		{name: "prune-partial", prune: true},
		{name: "eval", eval: true},
		{name: "eval-ternary", eval: true},
		{name: "eval-binop", eval: true},
		{name: "only", only: []string{"region", "name"}},
		{name: "except", except: []string{"secret"}},
		{name: "only-prune", prune: true, only: []string{"region"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := copyInputToTemp(t, filepath.Join("testdata", tc.name, "input"))
			e := NewExpander(tmp)
			e.Verbose = true
			e.Prune = tc.prune
			e.Eval = tc.eval
			e.Only = tc.only
			e.Except = tc.except
			require.NoError(t, e.Expand(true))
			compareDir(t, tmp, filepath.Join("testdata", tc.name, "expected"))
		})
	}
}

func TestExpand_Cycle(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/cycle/input")
	e := NewExpander(tmp)
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func TestExpand_ParseError(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/parse-error/input")
	e := NewExpander(tmp)
	err := e.Expand(true)
	require.Error(t, err)
}

func TestExpand_DuplicateLocal(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/duplicate/input")
	e := NewExpander(tmp)
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate local")
}

func TestExpand_StdoutMode(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/basic/input")
	var buf bytes.Buffer
	e := NewExpander(tmp)
	e.Out = &buf
	require.NoError(t, e.Expand(false))
	assert.Contains(t, buf.String(), `region = "us-east-1"`)
	got, err := os.ReadFile(filepath.Join(tmp, "main.tf"))
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/basic/input/main.tf")
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got), "file must not be modified in stdout mode")
}

func TestExpand_StdoutWriteError(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/basic/input")
	e := NewExpander(tmp)
	e.Out = failingWriter{}
	err := e.Expand(false)
	require.Error(t, err)
}

func TestExpand_GlobError(t *testing.T) {
	// A pattern with an unclosed character class makes filepath.Glob fail.
	e := NewExpander("[invalid")
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "glob")
}

func TestExpand_LoadReadError(t *testing.T) {
	tmp := t.TempDir()
	// A directory named "trap.tf" makes os.ReadFile fail when load() iterates it.
	require.NoError(t, os.Mkdir(filepath.Join(tmp, "trap.tf"), 0o755))
	e := NewExpander(tmp)
	err := e.Expand(true)
	require.Error(t, err)
}

// ----------------- unit tests for internal helpers -----------------

func TestIsLocalRefStart_PrecededByDot(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("foo")},
		{Type: hclsyntax.TokenDot, Bytes: []byte(".")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("local")},
		{Type: hclsyntax.TokenDot, Bytes: []byte(".")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("x")},
	}
	assert.False(t, isLocalRefStart(toks, 2))
}

func TestIsLocalRefStart_NonDotMiddle(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("local")},
		{Type: hclsyntax.TokenStar, Bytes: []byte("*")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("x")},
	}
	assert.False(t, isLocalRefStart(toks, 0))
}

func TestIsLocalRefStart_NonIdentEnd(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("local")},
		{Type: hclsyntax.TokenDot, Bytes: []byte(".")},
		{Type: hclsyntax.TokenStar, Bytes: []byte("*")},
	}
	assert.False(t, isLocalRefStart(toks, 0))
}

func TestNeedsParens_Cases(t *testing.T) {
	assert.False(t, needsParens(nil), "empty tokens => no parens")

	single := hclwrite.Tokens{{Type: hclsyntax.TokenStar, Bytes: []byte("*")}}
	assert.True(t, needsParens(single), "single non-primitive token => parens")

	singleIdent := hclwrite.Tokens{{Type: hclsyntax.TokenIdent, Bytes: []byte("x")}}
	assert.False(t, needsParens(singleIdent))

	// `(a)b` is wrapped but inner closes before the end → still needs parens
	notBalanced := hclwrite.Tokens{
		{Type: hclsyntax.TokenOParen, Bytes: []byte("(")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("a")},
		{Type: hclsyntax.TokenCParen, Bytes: []byte(")")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("b")},
	}
	assert.True(t, needsParens(notBalanced))
}

func TestGroupBalanced_NoOpener(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("a")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("b")},
	}
	assert.False(t, groupBalanced(toks, hclsyntax.TokenOParen, hclsyntax.TokenCParen))
}

func TestMatchTemplateSeqEnd_Nested(t *testing.T) {
	// ${ ${x} } : nested TemplateInterp drives depth++.
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenTemplateInterp, Bytes: []byte("${")},
		{Type: hclsyntax.TokenTemplateInterp, Bytes: []byte("${")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("x")},
		{Type: hclsyntax.TokenTemplateSeqEnd, Bytes: []byte("}")},
		{Type: hclsyntax.TokenTemplateSeqEnd, Bytes: []byte("}")},
	}
	assert.Equal(t, 4, matchTemplateSeqEnd(toks, 0))
}

func TestMatchTemplateSeqEnd_Unclosed(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenTemplateInterp, Bytes: []byte("${")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("x")},
	}
	assert.Equal(t, -1, matchTemplateSeqEnd(toks, 0))
}

func TestFlattenStringTemplates_Unbalanced(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenTemplateInterp, Bytes: []byte("${")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("x")},
	}
	got := flattenStringTemplates(toks)
	assert.Equal(t, toks, got)
}

func TestFlattenStringTemplates_NonFlattenableInner(t *testing.T) {
	// `${x + y}`: inner is neither a single literal nor a wrapped string template.
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenTemplateInterp, Bytes: []byte("${")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("x")},
		{Type: hclsyntax.TokenPlus, Bytes: []byte("+")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("y")},
		{Type: hclsyntax.TokenTemplateSeqEnd, Bytes: []byte("}")},
	}
	got := flattenStringTemplates(toks)
	assert.Equal(t, toks, got)
}

func TestLiteralToQuotedLit_Number(t *testing.T) {
	tok, ok := literalToQuotedLit(hclwrite.Tokens{
		{Type: hclsyntax.TokenNumberLit, Bytes: []byte("42")},
	})
	require.True(t, ok)
	assert.Equal(t, hclsyntax.TokenQuotedLit, tok.Type)
	assert.Equal(t, "42", string(tok.Bytes))
}

func TestLiteralToQuotedLit_Bools(t *testing.T) {
	for _, lit := range []string{"true", "false"} {
		tok, ok := literalToQuotedLit(hclwrite.Tokens{
			{Type: hclsyntax.TokenIdent, Bytes: []byte(lit)},
		})
		require.True(t, ok, lit)
		assert.Equal(t, lit, string(tok.Bytes))
	}
}

func TestLiteralToQuotedLit_NonMatching(t *testing.T) {
	_, ok := literalToQuotedLit(hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("a")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("b")},
	})
	assert.False(t, ok)

	_, ok = literalToQuotedLit(hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("nope")},
	})
	assert.False(t, ok)

	_, ok = literalToQuotedLit(hclwrite.Tokens{
		{Type: hclsyntax.TokenStar, Bytes: []byte("*")},
	})
	assert.False(t, ok)
}

func TestAccessChainExtent_NestedBrackets(t *testing.T) {
	// Tokens for `[[0,1][0]]` — exercise the nested-bracket depth++ path.
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenOBrack, Bytes: []byte("[")},
		{Type: hclsyntax.TokenOBrack, Bytes: []byte("[")},
		{Type: hclsyntax.TokenNumberLit, Bytes: []byte("0")},
		{Type: hclsyntax.TokenComma, Bytes: []byte(",")},
		{Type: hclsyntax.TokenNumberLit, Bytes: []byte("1")},
		{Type: hclsyntax.TokenCBrack, Bytes: []byte("]")},
		{Type: hclsyntax.TokenOBrack, Bytes: []byte("[")},
		{Type: hclsyntax.TokenNumberLit, Bytes: []byte("0")},
		{Type: hclsyntax.TokenCBrack, Bytes: []byte("]")},
		{Type: hclsyntax.TokenCBrack, Bytes: []byte("]")},
	}
	assert.Equal(t, len(toks), accessChainExtent(toks, 0))
}

func TestAccessChainExtent_UnbalancedBreaks(t *testing.T) {
	// `[ 0` with no closing bracket — should stop at the unclosed `[`.
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenOBrack, Bytes: []byte("[")},
		{Type: hclsyntax.TokenNumberLit, Bytes: []byte("0")},
	}
	assert.Equal(t, 0, accessChainExtent(toks, 0))
}

func TestFoldStaticExprs_Empty(t *testing.T) {
	assert.Nil(t, foldStaticExprs(nil, false))
}

func TestFoldOneExpr_ParseFailure(t *testing.T) {
	// `?` alone is not a valid expression — parse fails, no fold.
	src := []byte("?")
	out, ok := foldOneExpr(src)
	assert.False(t, ok)
	assert.Equal(t, src, out)
}

func TestTokensFromExprSource_ParseFailure(t *testing.T) {
	// `}` breaks the wrapped `x = }` parse.
	_, ok := tokensFromExprSource([]byte("}"))
	assert.False(t, ok)
}

func TestTryFoldAccess_ParseFailure(t *testing.T) {
	// Construct base/chain that produce unparseable source: a bare `)` in the
	// chain breaks the surrounding parens.
	base := hclwrite.Tokens{{Type: hclsyntax.TokenNumberLit, Bytes: []byte("1")}}
	chain := hclwrite.Tokens{{Type: hclsyntax.TokenCParen, Bytes: []byte(")")}}
	_, ok := tryFoldAccess(base, chain)
	assert.False(t, ok)
}

func TestRemoveUnusedLocalsFromBody_KeepsReferenced(t *testing.T) {
	src := []byte(`locals {
  keep = "k"
  drop = "d"
}
`)
	f, diags := hclwrite.ParseConfig(src, "test.tf", hcl.Pos{Line: 1, Column: 1})
	require.False(t, diags.HasErrors())

	used := map[string]bool{"keep": true}
	changed := removeUnusedLocalsFromBody(f.Body(), used, false)
	assert.True(t, changed)

	assert.Equal(t, `locals {
  keep = "k"
}
`, string(f.Bytes()))
}

func TestRemoveUnusedLocalsFromBody_RecursesIntoNonLocalsBlocks(t *testing.T) {
	// A `locals` block nested inside another block is unusual for Terraform
	// but the HCL parser accepts it; the prune pass should still reach it.
	src := []byte(`module "m" {
  locals {
    drop = "d"
  }
}
`)
	f, diags := hclwrite.ParseConfig(src, "test.tf", hcl.Pos{Line: 1, Column: 1})
	require.False(t, diags.HasErrors())

	changed := removeUnusedLocalsFromBody(f.Body(), map[string]bool{}, false)
	assert.True(t, changed)
	assert.Equal(t, `module "m" {
}
`, string(f.Bytes()))
}

// ----------------- test helpers -----------------

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) { return 0, errors.New("boom") }

func copyInputToTemp(t *testing.T, srcDir string) string {
	t.Helper()
	tmp := t.TempDir()
	entries, err := os.ReadDir(srcDir)
	require.NoError(t, err)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(srcDir, ent.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(tmp, ent.Name()), data, 0o644))
	}
	return tmp
}

func compareDir(t *testing.T, gotDir, wantDir string) {
	t.Helper()
	entries, err := os.ReadDir(wantDir)
	require.NoError(t, err)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		got, err := os.ReadFile(filepath.Join(gotDir, ent.Name()))
		require.NoError(t, err)
		want, err := os.ReadFile(filepath.Join(wantDir, ent.Name()))
		require.NoError(t, err)
		assert.Equal(t, string(want), string(got), "file %s", ent.Name())
	}
}

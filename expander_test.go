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
		vars   bool
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
		{name: "only", only: []string{"local.region", "local.name"}},
		{name: "except", except: []string{"local.secret"}},
		{name: "only-prune", prune: true, only: []string{"local.region"}},
		{name: "vars", vars: true},
		{name: "vars-mixed", vars: true},
		{name: "vars-eval", vars: true, eval: true},
		{name: "vars-prune", vars: true, prune: true},
		{name: "vars-only", vars: true, only: []string{"local.region", "var.port"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := copyInputToTemp(t, filepath.Join("testdata", tc.name, "input"))
			e := NewExpander(tmp)
			e.Verbose = true
			e.Prune = tc.prune
			e.Eval = tc.eval
			e.Vars = tc.vars
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

func TestExpand_DuplicateVariable(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/duplicate-var/input")
	e := NewExpander(tmp)
	e.Vars = true
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate variable")
}

func TestExpand_DuplicateVariableWithoutDefault(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "a.tf"), []byte(`variable "dup" {
  type = string
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "b.tf"), []byte(`variable "dup" {
  default = "x"
}
`), 0o644))
	e := NewExpander(tmp)
	e.Vars = true
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate variable")
}

func TestExpand_OnlyBareName(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/basic/input")
	e := NewExpander(tmp)
	e.Only = []string{"region"}
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local.")
}

func TestExpand_OnlyVarWithoutVarsFlag(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/basic/input")
	e := NewExpander(tmp)
	e.Only = []string{"var.foo"}
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--vars")
}

func TestExpand_ExceptBareName(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/basic/input")
	e := NewExpander(tmp)
	e.Except = []string{"region"}
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--except")
}

func TestExpand_OnlyEmptySuffix(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/basic/input")
	e := NewExpander(tmp)
	e.Only = []string{"local."}
	err := e.Expand(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no name after the prefix")
}

func TestExpand_OnlyTrimsWhitespace(t *testing.T) {
	tmp := copyInputToTemp(t, "testdata/only/input")
	e := NewExpander(tmp)
	e.Only = []string{" local.region", "local.name "}
	require.NoError(t, e.Expand(true))
	compareDir(t, tmp, "testdata/only/expected")
}

func TestCollectVariables_SkipsBlocksWithUnexpectedLabels(t *testing.T) {
	f := hclwrite.NewEmptyFile()
	f.Body().AppendNewBlock("variable", nil)
	f.Body().AppendNewBlock("variable", []string{"a", "b"})
	e := NewExpander("")
	e.Vars = true
	e.files["test.tf"] = f
	require.NoError(t, e.collectVariables())
	assert.Empty(t, e.varsRaw)
}

func TestCollectAllRefs_SkipsVariableBlocks(t *testing.T) {
	src := []byte(`variable "a" {
  default = var.b
}
resource "foo" "bar" {
  name = local.x
}
`)
	f, diags := hclwrite.ParseConfig(src, "test.tf", hcl.Pos{Line: 1, Column: 1})
	require.False(t, diags.HasErrors())
	usedLocals := map[string]bool{}
	usedVars := map[string]bool{}
	collectAllRefs(f.Body(), usedLocals, usedVars)
	assert.True(t, usedLocals["x"])
	assert.False(t, usedVars["b"], "var.b inside a variable block should not count as used")
}

func TestRemoveUnusedFromBody_VariableBlockWithUnexpectedLabels(t *testing.T) {
	f := hclwrite.NewEmptyFile()
	f.Body().AppendNewBlock("variable", nil)
	f.Body().AppendNewBlock("variable", []string{"a", "b"})
	changed := removeUnusedFromBody(f.Body(), nil, nil, map[string]hclwrite.Tokens{}, false)
	assert.False(t, changed)
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
	// A directory named "trap.tf" makes os.ReadFile fail in load().
	require.NoError(t, os.Mkdir(filepath.Join(tmp, "trap.tf"), 0o755))
	e := NewExpander(tmp)
	err := e.Expand(true)
	require.Error(t, err)
}

// ----------------- unit tests for internal helpers -----------------

func TestIsRefStart_PrecededByDot(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("foo")},
		{Type: hclsyntax.TokenDot, Bytes: []byte(".")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("local")},
		{Type: hclsyntax.TokenDot, Bytes: []byte(".")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("x")},
	}
	_, _, ok := isRefStart(toks, 2)
	assert.False(t, ok)
}

func TestIsRefStart_NonDotMiddle(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("local")},
		{Type: hclsyntax.TokenStar, Bytes: []byte("*")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("x")},
	}
	_, _, ok := isRefStart(toks, 0)
	assert.False(t, ok)
}

func TestIsRefStart_NonIdentEnd(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("local")},
		{Type: hclsyntax.TokenDot, Bytes: []byte(".")},
		{Type: hclsyntax.TokenStar, Bytes: []byte("*")},
	}
	_, _, ok := isRefStart(toks, 0)
	assert.False(t, ok)
}

func TestIsRefStart_Var(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("var")},
		{Type: hclsyntax.TokenDot, Bytes: []byte(".")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("foo")},
	}
	name, prefix, ok := isRefStart(toks, 0)
	assert.True(t, ok)
	assert.Equal(t, "foo", name)
	assert.Equal(t, "var", prefix)
}

func TestIsRefStart_OtherIdent(t *testing.T) {
	toks := hclwrite.Tokens{
		{Type: hclsyntax.TokenIdent, Bytes: []byte("data")},
		{Type: hclsyntax.TokenDot, Bytes: []byte(".")},
		{Type: hclsyntax.TokenIdent, Bytes: []byte("foo")},
	}
	_, _, ok := isRefStart(toks, 0)
	assert.False(t, ok)
}

func TestNeedsParens_Cases(t *testing.T) {
	assert.False(t, needsParens(nil), "empty tokens => no parens")

	single := hclwrite.Tokens{{Type: hclsyntax.TokenStar, Bytes: []byte("*")}}
	assert.True(t, needsParens(single), "single non-primitive token => parens")

	singleIdent := hclwrite.Tokens{{Type: hclsyntax.TokenIdent, Bytes: []byte("x")}}
	assert.False(t, needsParens(singleIdent))

	// `(a)b` is wrapped but the inner group closes before the end, so it still needs parens.
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
	// Tokens for `[[0,1][0]]`, exercising the nested-bracket depth++ path.
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
	// `[ 0` with no closing bracket should stop at the unclosed `[`.
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
	// `?` alone is not a valid expression, so parse fails and no fold happens.
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

func TestRemoveUnusedFromBody_KeepsReferenced(t *testing.T) {
	src := []byte(`locals {
  keep = "k"
  drop = "d"
}
`)
	f, diags := hclwrite.ParseConfig(src, "test.tf", hcl.Pos{Line: 1, Column: 1})
	require.False(t, diags.HasErrors())

	usedLocals := map[string]bool{"keep": true}
	changed := removeUnusedFromBody(f.Body(), usedLocals, nil, nil, false)
	assert.True(t, changed)

	assert.Equal(t, `locals {
  keep = "k"
}
`, string(f.Bytes()))
}

func TestRemoveUnusedFromBody_RecursesIntoNonLocalsBlocks(t *testing.T) {
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

	changed := removeUnusedFromBody(f.Body(), map[string]bool{}, nil, nil, false)
	assert.True(t, changed)
	assert.Equal(t, `module "m" {
}
`, string(f.Bytes()))
}

func TestRemoveUnusedFromBody_PrunesVariable(t *testing.T) {
	src := []byte(`variable "drop" {
  default = "d"
}
variable "keep_no_default" {
  type = string
}
variable "keep_used" {
  default = "u"
}
`)
	f, diags := hclwrite.ParseConfig(src, "test.tf", hcl.Pos{Line: 1, Column: 1})
	require.False(t, diags.HasErrors())

	varsWithDefault := map[string]hclwrite.Tokens{
		"drop":      {{Type: hclsyntax.TokenQuotedLit, Bytes: []byte("d")}},
		"keep_used": {{Type: hclsyntax.TokenQuotedLit, Bytes: []byte("u")}},
	}
	usedVars := map[string]bool{"keep_used": true}
	changed := removeUnusedFromBody(f.Body(), map[string]bool{}, usedVars, varsWithDefault, false)
	assert.True(t, changed)

	assert.Equal(t, `variable "keep_no_default" {
  type = string
}
variable "keep_used" {
  default = "u"
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

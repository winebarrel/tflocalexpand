package tflocalexpand

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

type Expander struct {
	Dir     string
	Out     io.Writer
	Verbose bool
	Prune   bool
	Eval    bool
	Vars    bool
	Only    []string
	Except  []string

	files     map[string]*hclwrite.File
	localsRaw map[string]hclwrite.Tokens
	localsDef map[string]string
	varsRaw   map[string]hclwrite.Tokens
	varsDef   map[string]string
	resolved  map[string]hclwrite.Tokens
}

func NewExpander(dir string) *Expander {
	return &Expander{
		Dir:       dir,
		Out:       os.Stdout,
		files:     map[string]*hclwrite.File{},
		localsRaw: map[string]hclwrite.Tokens{},
		localsDef: map[string]string{},
		varsRaw:   map[string]hclwrite.Tokens{},
		varsDef:   map[string]string{},
		resolved:  map[string]hclwrite.Tokens{},
	}
}

func (e *Expander) Expand(inPlace bool) error {
	if err := e.load(); err != nil {
		return err
	}
	if err := e.collectLocals(); err != nil {
		return err
	}
	if err := e.collectVariables(); err != nil {
		return err
	}
	if err := e.resolveLocals(); err != nil {
		return err
	}
	return e.rewriteAll(inPlace)
}

func (e *Expander) load() error {
	pattern := filepath.Join(e.Dir, "*.tf")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob: %w", err)
	}
	sort.Strings(matches)
	var diags hcl.Diagnostics
	for _, path := range matches {
		src, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		f, parseDiags := hclwrite.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
		if parseDiags.HasErrors() {
			diags = append(diags, parseDiags...)
			continue
		}
		e.files[path] = f
	}
	if diags.HasErrors() {
		return diags
	}
	return nil
}

func (e *Expander) collectLocals() error {
	for path, f := range e.files {
		for _, block := range f.Body().Blocks() {
			if block.Type() != "locals" {
				continue
			}
			for name, attr := range block.Body().Attributes() {
				if existing, dup := e.localsDef[name]; dup {
					return fmt.Errorf("duplicate local %q (defined in %s and %s)", name, existing, path)
				}
				e.localsRaw[name] = attr.Expr().BuildTokens(nil)
				e.localsDef[name] = path
			}
		}
	}
	return nil
}

// collectVariables records the default expression of each `variable "<name>"`
// block. Variables without a `default` are skipped and stay as `var.<name>`
// references. No-op when Vars is false.
func (e *Expander) collectVariables() error {
	if !e.Vars {
		return nil
	}
	for path, f := range e.files {
		for _, block := range f.Body().Blocks() {
			if block.Type() != "variable" {
				continue
			}
			labels := block.Labels()
			if len(labels) != 1 {
				continue
			}
			name := labels[0]
			def := block.Body().GetAttribute("default")
			if def == nil {
				continue
			}
			if existing, dup := e.varsDef[name]; dup {
				return fmt.Errorf("duplicate variable %q (defined in %s and %s)", name, existing, path)
			}
			e.varsRaw[name] = def.Expr().BuildTokens(nil)
			e.varsDef[name] = path
		}
	}
	return nil
}

func (e *Expander) resolveLocals() error {
	visiting := map[string]bool{}
	var resolve func(name string) error
	resolve = func(name string) error {
		if _, ok := e.resolved[name]; ok {
			return nil
		}
		raw, ok := e.localsRaw[name]
		if !ok {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("circular reference involving local.%s", name)
		}
		visiting[name] = true
		defer delete(visiting, name)

		for _, ref := range collectLocalRefs(raw) {
			if err := resolve(ref); err != nil {
				return err
			}
		}
		e.resolved[name] = replaceRefs(raw, e.resolved, e.varsRaw, e.Verbose, e.Eval)
		return nil
	}
	names := make([]string, 0, len(e.localsRaw))
	for name := range e.localsRaw {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := resolve(name); err != nil {
			return err
		}
	}
	return nil
}

func (e *Expander) rewriteAll(inPlace bool) error {
	paths := make([]string, 0, len(e.files))
	for p := range e.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	locals, vars, err := e.filteredResolved()
	if err != nil {
		return err
	}
	changedFiles := map[string]bool{}
	for _, path := range paths {
		f := e.files[path]
		if rewriteBody(f.Body(), locals, vars, e.Verbose, e.Prune, e.Eval) {
			changedFiles[path] = true
		}
	}

	if e.Prune {
		e.pruneUnused(changedFiles)
	}

	for _, path := range paths {
		if !changedFiles[path] {
			continue
		}
		f := e.files[path]
		body := f.Bytes()
		if !inPlace {
			if _, err := fmt.Fprintf(e.Out, "### %s ###\n%s", path, body); err != nil {
				return err
			}
			continue
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		if e.Verbose {
			log.Printf("rewrote %s", path)
		}
	}
	return nil
}

func rewriteBody(body *hclwrite.Body, locals, vars map[string]hclwrite.Tokens, verbose, includeDefs, eval bool) bool {
	changed := false
	for name, attr := range body.Attributes() {
		orig := attr.Expr().BuildTokens(nil)
		repl := replaceRefs(orig, locals, vars, verbose, eval)
		if !tokensEqual(orig, repl) {
			body.SetAttributeRaw(name, repl)
			changed = true
		}
	}
	for _, blk := range body.Blocks() {
		if blk.Type() == "locals" && !includeDefs {
			continue
		}
		if blk.Type() == "variable" {
			continue
		}
		if rewriteBody(blk.Body(), locals, vars, verbose, includeDefs, eval) {
			changed = true
		}
	}
	return changed
}

// filteredResolved returns the resolved locals and variables narrowed by
// Only/Except. Names absent from the returned maps stay as `local.<name>` or
// `var.<name>` references after rewriting. Chain resolution in resolveLocals
// uses the unfiltered maps, so expanded values are complete even when
// dependencies are excluded from the user-facing rewrite.
func (e *Expander) filteredResolved() (map[string]hclwrite.Tokens, map[string]hclwrite.Tokens, error) {
	if len(e.Only) == 0 && len(e.Except) == 0 {
		return e.resolved, e.varsRaw, nil
	}
	onlyLocals, onlyVars, err := splitPrefixedNames(e.Only)
	if err != nil {
		return nil, nil, fmt.Errorf("--only: %w", err)
	}
	exceptLocals, exceptVars, err := splitPrefixedNames(e.Except)
	if err != nil {
		return nil, nil, fmt.Errorf("--except: %w", err)
	}
	if !e.Vars && (len(onlyVars) > 0 || len(exceptVars) > 0) {
		return nil, nil, fmt.Errorf("var.<name> in --only/--except requires --vars")
	}
	useAllow := len(e.Only) > 0
	return filterRefs(e.resolved, onlyLocals, exceptLocals, useAllow),
		filterRefs(e.varsRaw, onlyVars, exceptVars, useAllow),
		nil
}

// splitPrefixedNames parses `local.X` / `var.X` names into separate sets.
// Bare names (without prefix) return an error.
func splitPrefixedNames(names []string) (locals, vars map[string]bool, err error) {
	locals = map[string]bool{}
	vars = map[string]bool{}
	for _, n := range names {
		switch {
		case strings.HasPrefix(n, "local."):
			locals[strings.TrimPrefix(n, "local.")] = true
		case strings.HasPrefix(n, "var."):
			vars[strings.TrimPrefix(n, "var.")] = true
		default:
			return nil, nil, fmt.Errorf("%q must be prefixed with \"local.\" or \"var.\"", n)
		}
	}
	return locals, vars, nil
}

func filterRefs(in map[string]hclwrite.Tokens, allow, deny map[string]bool, useAllow bool) map[string]hclwrite.Tokens {
	out := map[string]hclwrite.Tokens{}
	if useAllow {
		for name, tokens := range in {
			if allow[name] {
				out[name] = tokens
			}
		}
		return out
	}
	for name, tokens := range in {
		if !deny[name] {
			out[name] = tokens
		}
	}
	return out
}

// pruneUnused removes local definitions whose `local.<name>` is not
// referenced after expansion, drops empty `locals` blocks, and (when Vars is
// enabled) removes `variable` blocks with a default whose `var.<name>` is no
// longer referenced. Variables without a default are kept.
func (e *Expander) pruneUnused(changedFiles map[string]bool) {
	usedLocals := map[string]bool{}
	usedVars := map[string]bool{}
	for _, f := range e.files {
		collectAllRefs(f.Body(), usedLocals, usedVars)
	}
	for path, f := range e.files {
		if removeUnusedFromBody(f.Body(), usedLocals, usedVars, e.varsRaw, e.Verbose) {
			changedFiles[path] = true
		}
	}
}

func collectAllRefs(body *hclwrite.Body, usedLocals, usedVars map[string]bool) {
	for _, attr := range body.Attributes() {
		tokens := attr.Expr().BuildTokens(nil)
		for i := 0; i < len(tokens); i++ {
			name, kind, ok := isRefStart(tokens, i)
			if !ok {
				continue
			}
			switch kind {
			case "local":
				usedLocals[name] = true
			case "var":
				usedVars[name] = true
			}
			i += 2
		}
	}
	for _, blk := range body.Blocks() {
		collectAllRefs(blk.Body(), usedLocals, usedVars)
	}
}

func removeUnusedFromBody(body *hclwrite.Body, usedLocals, usedVars map[string]bool, varsWithDefault map[string]hclwrite.Tokens, verbose bool) bool {
	changed := false
	for _, blk := range body.Blocks() {
		switch blk.Type() {
		case "locals":
			for name := range blk.Body().Attributes() {
				if usedLocals[name] {
					continue
				}
				blk.Body().RemoveAttribute(name)
				if verbose {
					log.Printf("  - pruning unused local.%s", name)
				}
				changed = true
			}
			if len(blk.Body().Attributes()) == 0 && len(blk.Body().Blocks()) == 0 {
				body.RemoveBlock(blk)
				changed = true
			}
		case "variable":
			labels := blk.Labels()
			if len(labels) != 1 {
				continue
			}
			name := labels[0]
			if usedVars[name] {
				continue
			}
			if _, hadDefault := varsWithDefault[name]; !hadDefault {
				continue
			}
			body.RemoveBlock(blk)
			if verbose {
				log.Printf("  - pruning unused var.%s", name)
			}
			changed = true
		default:
			if removeUnusedFromBody(blk.Body(), usedLocals, usedVars, varsWithDefault, verbose) {
				changed = true
			}
		}
	}
	return changed
}

func tokensEqual(a, b hclwrite.Tokens) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || string(a[i].Bytes) != string(b[i].Bytes) || a[i].SpacesBefore != b[i].SpacesBefore {
			return false
		}
	}
	return true
}

// collectLocalRefs returns names referenced via `local.<name>` in tokens.
// `var.<name>` references are ignored because variables do not chain into the
// locals resolution graph.
func collectLocalRefs(tokens hclwrite.Tokens) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(tokens); i++ {
		name, kind, ok := isRefStart(tokens, i)
		if !ok || kind != "local" {
			continue
		}
		if !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
		i += 2
	}
	return out
}

// isRefStart reports whether tokens[i:i+3] is `local.<ident>` or `var.<ident>`
// and is not preceded by `.` (which would make it part of an outer attribute
// access). Returns the bare name and the matched prefix ("local" or "var").
func isRefStart(tokens hclwrite.Tokens, i int) (string, string, bool) {
	if i+2 >= len(tokens) {
		return "", "", false
	}
	t0, t1, t2 := tokens[i], tokens[i+1], tokens[i+2]
	if t0.Type != hclsyntax.TokenIdent {
		return "", "", false
	}
	prefix := string(t0.Bytes)
	if prefix != "local" && prefix != "var" {
		return "", "", false
	}
	if t1.Type != hclsyntax.TokenDot {
		return "", "", false
	}
	if t2.Type != hclsyntax.TokenIdent {
		return "", "", false
	}
	if i > 0 && tokens[i-1].Type == hclsyntax.TokenDot {
		return "", "", false
	}
	return string(t2.Bytes), prefix, true
}

// replaceRefs returns a new token slice with `local.X` and `var.X` references
// replaced by their resolved tokens, parenthesized when needed. References
// whose name is absent from the corresponding map are left as-is. When eval
// is true and a substituted reference is followed by a chain of `.attr` /
// `[idx]` accessors that can be evaluated in an empty context, the chain is
// folded to a literal.
func replaceRefs(in hclwrite.Tokens, locals, vars map[string]hclwrite.Tokens, verbose, eval bool) hclwrite.Tokens {
	in = cloneTokens(in)
	out := make(hclwrite.Tokens, 0, len(in))
	i := 0
	for i < len(in) {
		if name, prefix, ok := isRefStart(in, i); ok {
			var repl hclwrite.Tokens
			var has bool
			switch prefix {
			case "local":
				repl, has = locals[name]
			case "var":
				repl, has = vars[name]
			}
			if !has {
				out = append(out, in[i], in[i+1], in[i+2])
				i += 3
				continue
			}
			leadSpaces := in[i].SpacesBefore
			replCopy := cloneTokens(repl)
			if needsParens(replCopy) {
				replCopy = wrapParens(replCopy)
			}

			chainEnd := i + 3
			if eval {
				chainEnd = accessChainExtent(in, i+3)
			}
			if eval && chainEnd > i+3 {
				if folded, ok := tryFoldAccess(replCopy, in[i+3:chainEnd]); ok {
					if len(folded) > 0 {
						folded[0].SpacesBefore = leadSpaces
					}
					if verbose {
						log.Printf("  - expanding %s.%s (folded)", prefix, name)
					}
					out = append(out, folded...)
					i = chainEnd
					continue
				}
			}

			if len(replCopy) > 0 {
				replCopy[0].SpacesBefore = leadSpaces
			}
			if verbose {
				log.Printf("  - expanding %s.%s", prefix, name)
			}
			out = append(out, replCopy...)
			i += 3
			continue
		}
		out = append(out, in[i])
		i++
	}
	out = mergeQuotedLits(flattenStringTemplates(out))
	if eval {
		out = foldStaticExprs(out, verbose)
		// Folding inside a template (e.g. `${true ? "b" : "c"}` or
		// `${100 > 0}`) can produce new `${"..."}` or adjacent QuotedLit
		// sequences that the earlier flatten/merge pass did not see.
		out = mergeQuotedLits(flattenStringTemplates(out))
	}
	return out
}

// accessChainExtent returns the exclusive end index of the chain of
// `.<ident>` and `[...]` accessors starting at `start`, or `start` if no
// chain is present.
func accessChainExtent(in hclwrite.Tokens, start int) int {
	i := start
	for i < len(in) {
		if i+1 < len(in) && in[i].Type == hclsyntax.TokenDot && in[i+1].Type == hclsyntax.TokenIdent {
			i += 2
			continue
		}
		if in[i].Type == hclsyntax.TokenOBrack {
			depth := 1
			j := i + 1
			for j < len(in) && depth > 0 {
				switch in[j].Type {
				case hclsyntax.TokenOBrack:
					depth++
				case hclsyntax.TokenCBrack:
					depth--
				}
				j++
			}
			if depth != 0 {
				break
			}
			i = j
			continue
		}
		break
	}
	return i
}

// tryFoldAccess builds `(<base>)<chain>` as source, parses it as an HCL
// expression, evaluates it with a nil context (constants only; no variables
// or functions), and returns the resulting value's literal tokens. Returns
// (nil, false) if parsing fails, evaluation fails, or the result is not
// wholly known.
func tryFoldAccess(base, chain hclwrite.Tokens) (hclwrite.Tokens, bool) {
	var buf bytes.Buffer
	buf.WriteByte('(')
	buf.Write(base.Bytes())
	buf.WriteByte(')')
	buf.Write(chain.Bytes())
	expr, diags := hclsyntax.ParseExpression(buf.Bytes(), "", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, false
	}
	val, diags := expr.Value(nil)
	if diags.HasErrors() || !val.IsWhollyKnown() {
		return nil, false
	}
	return hclwrite.TokensForValue(val), true
}

// foldStaticExprs re-parses the given tokens as an HCL expression and folds:
//   - ConditionalExpr whose condition evaluates to a known boolean. The chosen
//     branch's source is copied verbatim; the other branch may still reference
//     unknowns.
//   - BinaryOpExpr or UnaryOpExpr whose result evaluates to a known boolean,
//     replaced by `true` or `false`. Comparison and logical operators are the
//     practical match; arithmetic stays as-is because its result is not bool.
//
// Loops until no more folds are possible. Expressions that still reference
// `var.X`, functions, or unresolved `local.X` are left untouched.
func foldStaticExprs(in hclwrite.Tokens, verbose bool) hclwrite.Tokens {
	if len(in) == 0 || !hasFoldableToken(in) {
		return in
	}
	src := in.Bytes()
	changed := false
	for {
		next, didFold := foldOneExpr(src)
		if !didFold {
			break
		}
		src = next
		changed = true
		if verbose {
			log.Printf("  - folding static expression")
		}
	}
	if !changed {
		return in
	}
	out, ok := tokensFromExprSource(src)
	if !ok {
		return in
	}
	if len(out) > 0 {
		out[0].SpacesBefore = in[0].SpacesBefore
	}
	return out
}

// hasFoldableToken reports whether the token slice contains any operator that
// could anchor a foldable expression (ternary `?`, comparison, logical AND/OR,
// or unary `!`). Used as a pre-check to skip the parse/walk work when there
// is nothing to fold.
func hasFoldableToken(in hclwrite.Tokens) bool {
	for _, t := range in {
		switch t.Type {
		case hclsyntax.TokenQuestion,
			hclsyntax.TokenEqualOp,
			hclsyntax.TokenNotEqual,
			hclsyntax.TokenLessThan,
			hclsyntax.TokenLessThanEq,
			hclsyntax.TokenGreaterThan,
			hclsyntax.TokenGreaterThanEq,
			hclsyntax.TokenAnd,
			hclsyntax.TokenOr,
			hclsyntax.TokenBang:
			return true
		}
	}
	return false
}

// foldOneExpr finds the first foldable expression and rewrites its source
// range. Returns the original src unchanged when none is found.
func foldOneExpr(src []byte) ([]byte, bool) {
	expr, diags := hclsyntax.ParseExpression(src, "", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return src, false
	}
	finder := &foldFinder{}
	hclsyntax.Walk(expr, finder)
	if finder.target == nil {
		return src, false
	}
	tr := finder.target.Range()
	var replacement []byte
	if finder.chosen != nil {
		cr := finder.chosen.Range()
		replacement = src[cr.Start.Byte:cr.End.Byte]
	} else {
		replacement = hclwrite.TokensForValue(finder.value).Bytes()
	}
	out := make([]byte, 0, len(src)-(tr.End.Byte-tr.Start.Byte)+len(replacement))
	out = append(out, src[:tr.Start.Byte]...)
	out = append(out, replacement...)
	out = append(out, src[tr.End.Byte:]...)
	return out, true
}

// foldFinder walks an HCL AST and captures the first foldable expression.
// For ConditionalExpr, `chosen` is the branch whose source is copied verbatim.
// For BinaryOpExpr and UnaryOpExpr, `value` is the evaluated bool to emit.
type foldFinder struct {
	target hclsyntax.Expression
	chosen hclsyntax.Expression
	value  cty.Value
}

func (f *foldFinder) Enter(n hclsyntax.Node) hcl.Diagnostics {
	if f.target != nil {
		return nil
	}
	switch e := n.(type) {
	case *hclsyntax.ConditionalExpr:
		v, d := e.Condition.Value(nil)
		if d.HasErrors() || !v.IsWhollyKnown() || v.IsNull() || v.Type() != cty.Bool {
			return nil
		}
		f.target = e
		if v.True() {
			f.chosen = e.TrueResult
		} else {
			f.chosen = e.FalseResult
		}
	case *hclsyntax.BinaryOpExpr:
		v, d := e.Value(nil)
		if d.HasErrors() || !v.IsWhollyKnown() || v.IsNull() || v.Type() != cty.Bool {
			return nil
		}
		f.target = e
		f.value = v
	case *hclsyntax.UnaryOpExpr:
		v, d := e.Value(nil)
		if d.HasErrors() || !v.IsWhollyKnown() || v.IsNull() || v.Type() != cty.Bool {
			return nil
		}
		f.target = e
		f.value = v
	}
	return nil
}

func (f *foldFinder) Exit(hclsyntax.Node) hcl.Diagnostics { return nil }

// tokensFromExprSource re-tokenizes an expression source by parsing it as an
// attribute value and lifting its expression tokens. Returns (nil, false) if
// the source is not a valid HCL expression.
func tokensFromExprSource(src []byte) (hclwrite.Tokens, bool) {
	wrapped := append([]byte("x = "), src...)
	f, diags := hclwrite.ParseConfig(wrapped, "", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, false
	}
	attr := f.Body().GetAttribute("x")
	if attr == nil {
		return nil, false
	}
	return attr.Expr().BuildTokens(nil), true
}

// flattenStringTemplates rewrites `${"..."}` sequences to inline their
// content, so a substituted `"a${"xxx"}b"` collapses to `"axxxb"`. Only
// applies when the inner expression is a balanced quoted string template.
func flattenStringTemplates(in hclwrite.Tokens) hclwrite.Tokens {
	out := make(hclwrite.Tokens, 0, len(in))
	i := 0
	for i < len(in) {
		if in[i].Type != hclsyntax.TokenTemplateInterp {
			out = append(out, in[i])
			i++
			continue
		}
		j := matchTemplateSeqEnd(in, i)
		if j < 0 {
			out = append(out, in[i])
			i++
			continue
		}
		inner := in[i+1 : j]
		if len(inner) >= 2 &&
			inner[0].Type == hclsyntax.TokenOQuote &&
			inner[len(inner)-1].Type == hclsyntax.TokenCQuote &&
			groupBalanced(inner, hclsyntax.TokenOQuote, hclsyntax.TokenCQuote) {
			body := inner[1 : len(inner)-1]
			if len(body) > 0 {
				body[0].SpacesBefore = 0
			}
			out = append(out, body...)
			i = j + 1
			continue
		}
		if lit, ok := literalToQuotedLit(inner); ok {
			out = append(out, lit)
			i = j + 1
			continue
		}
		out = append(out, in[i])
		i++
	}
	return out
}

// literalToQuotedLit converts a single primitive literal token (number, true,
// or false) into a QuotedLit so it can be inlined into a surrounding string
// template. Returns (nil, false) if the inner is not such a literal.
func literalToQuotedLit(inner hclwrite.Tokens) (*hclwrite.Token, bool) {
	if len(inner) != 1 {
		return nil, false
	}
	t := inner[0]
	switch t.Type {
	case hclsyntax.TokenNumberLit:
		// ok
	case hclsyntax.TokenIdent:
		s := string(t.Bytes)
		if s != "true" && s != "false" {
			return nil, false
		}
	default:
		return nil, false
	}
	return &hclwrite.Token{
		Type:  hclsyntax.TokenQuotedLit,
		Bytes: append([]byte(nil), t.Bytes...),
	}, true
}

// matchTemplateSeqEnd returns the index of the TemplateSeqEnd matching the
// TemplateInterp at index i, or -1 if unbalanced.
func matchTemplateSeqEnd(in hclwrite.Tokens, i int) int {
	depth := 1
	for j := i + 1; j < len(in); j++ {
		switch in[j].Type {
		case hclsyntax.TokenTemplateInterp:
			depth++
		case hclsyntax.TokenTemplateSeqEnd:
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// mergeQuotedLits collapses consecutive QuotedLit tokens into a single token.
func mergeQuotedLits(in hclwrite.Tokens) hclwrite.Tokens {
	out := make(hclwrite.Tokens, 0, len(in))
	for _, t := range in {
		if len(out) > 0 &&
			out[len(out)-1].Type == hclsyntax.TokenQuotedLit &&
			t.Type == hclsyntax.TokenQuotedLit {
			prev := out[len(out)-1]
			merged := make([]byte, 0, len(prev.Bytes)+len(t.Bytes))
			merged = append(merged, prev.Bytes...)
			merged = append(merged, t.Bytes...)
			prev.Bytes = merged
			continue
		}
		out = append(out, t)
	}
	return out
}

func cloneTokens(in hclwrite.Tokens) hclwrite.Tokens {
	out := make(hclwrite.Tokens, len(in))
	for i, t := range in {
		nt := *t
		nt.Bytes = append([]byte(nil), t.Bytes...)
		out[i] = &nt
	}
	return out
}

func wrapParens(t hclwrite.Tokens) hclwrite.Tokens {
	open := &hclwrite.Token{Type: hclsyntax.TokenOParen, Bytes: []byte("(")}
	closeT := &hclwrite.Token{Type: hclsyntax.TokenCParen, Bytes: []byte(")")}
	if len(t) > 0 {
		t[0].SpacesBefore = 0
	}
	out := make(hclwrite.Tokens, 0, len(t)+2)
	out = append(out, open)
	out = append(out, t...)
	out = append(out, closeT)
	return out
}

// needsParens reports whether `t` must be parenthesized when substituted into
// a larger expression to preserve precedence.
func needsParens(t hclwrite.Tokens) bool {
	if len(t) == 0 {
		return false
	}
	if len(t) == 1 {
		switch t[0].Type {
		case hclsyntax.TokenIdent, hclsyntax.TokenNumberLit:
			return false
		}
		return true
	}
	pairs := []struct{ o, c hclsyntax.TokenType }{
		{hclsyntax.TokenOParen, hclsyntax.TokenCParen},
		{hclsyntax.TokenOBrack, hclsyntax.TokenCBrack},
		{hclsyntax.TokenOBrace, hclsyntax.TokenCBrace},
		{hclsyntax.TokenOQuote, hclsyntax.TokenCQuote},
		{hclsyntax.TokenOHeredoc, hclsyntax.TokenCHeredoc},
	}
	for _, p := range pairs {
		if t[0].Type == p.o && t[len(t)-1].Type == p.c && groupBalanced(t, p.o, p.c) {
			return false
		}
	}
	return true
}

// groupBalanced reports whether the opener at index 0 closes at the last
// index, i.e. the whole slice is one balanced group.
func groupBalanced(t hclwrite.Tokens, opener, closer hclsyntax.TokenType) bool {
	depth := 0
	for i, tok := range t {
		switch tok.Type {
		case opener:
			depth++
		case closer:
			depth--
			if depth == 0 {
				return i == len(t)-1
			}
		}
	}
	return false
}

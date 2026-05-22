package tflocalexpand

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

type Expander struct {
	Dir     string
	Out     io.Writer
	Verbose bool

	files     map[string]*hclwrite.File
	localsRaw map[string]hclwrite.Tokens
	localsDef map[string]string
	resolved  map[string]hclwrite.Tokens
}

func NewExpander(dir string) *Expander {
	return &Expander{
		Dir:       dir,
		Out:       os.Stdout,
		files:     map[string]*hclwrite.File{},
		localsRaw: map[string]hclwrite.Tokens{},
		localsDef: map[string]string{},
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
		e.resolved[name] = replaceLocalRefs(raw, e.resolved, e.Verbose)
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
	for _, path := range paths {
		f := e.files[path]
		changed := rewriteBody(f.Body(), e.resolved, e.Verbose)
		if !changed {
			continue
		}
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

func rewriteBody(body *hclwrite.Body, locals map[string]hclwrite.Tokens, verbose bool) bool {
	changed := false
	for name, attr := range body.Attributes() {
		orig := attr.Expr().BuildTokens(nil)
		repl := replaceLocalRefs(orig, locals, verbose)
		if !tokensEqual(orig, repl) {
			body.SetAttributeRaw(name, repl)
			changed = true
		}
	}
	for _, blk := range body.Blocks() {
		if blk.Type() == "locals" {
			continue
		}
		if rewriteBody(blk.Body(), locals, verbose) {
			changed = true
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
func collectLocalRefs(tokens hclwrite.Tokens) []string {
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(tokens); i++ {
		if !isLocalRefStart(tokens, i) {
			continue
		}
		name := string(tokens[i+2].Bytes)
		if !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
		i += 2
	}
	return out
}

// isLocalRefStart reports whether tokens[i:i+3] is `local.<ident>` and is not
// preceded by `.` (which would make it part of a longer attribute access).
func isLocalRefStart(tokens hclwrite.Tokens, i int) bool {
	if i+2 >= len(tokens) {
		return false
	}
	t0, t1, t2 := tokens[i], tokens[i+1], tokens[i+2]
	if t0.Type != hclsyntax.TokenIdent || string(t0.Bytes) != "local" {
		return false
	}
	if t1.Type != hclsyntax.TokenDot {
		return false
	}
	if t2.Type != hclsyntax.TokenIdent {
		return false
	}
	if i > 0 && tokens[i-1].Type == hclsyntax.TokenDot {
		return false
	}
	return true
}

// replaceLocalRefs returns a new token slice with `local.X` references
// replaced by the resolved tokens, parenthesized when needed.
func replaceLocalRefs(in hclwrite.Tokens, locals map[string]hclwrite.Tokens, verbose bool) hclwrite.Tokens {
	in = cloneTokens(in)
	out := make(hclwrite.Tokens, 0, len(in))
	i := 0
	for i < len(in) {
		if isLocalRefStart(in, i) {
			name := string(in[i+2].Bytes)
			repl, ok := locals[name]
			if !ok {
				out = append(out, in[i], in[i+1], in[i+2])
				i += 3
				continue
			}
			replCopy := cloneTokens(repl)
			if needsParens(replCopy) {
				replCopy = wrapParens(replCopy)
			}
			if len(replCopy) > 0 {
				replCopy[0].SpacesBefore = in[i].SpacesBefore
			}
			if verbose {
				log.Printf("  - expanding local.%s", name)
			}
			out = append(out, replCopy...)
			i += 3
			continue
		}
		out = append(out, in[i])
		i++
	}
	return mergeQuotedLits(flattenStringTemplates(out))
}

// flattenStringTemplates rewrites `${"..."}` sequences to inline their content,
// so a substituted `"a${"xxx"}b"` collapses to `"axxxb"`. Only applies when the
// inner expression is exactly a balanced quoted string template.
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

// literalToQuotedLit converts a single primitive literal token (number / true /
// false) into a QuotedLit so it can be inlined into a surrounding string
// template. Returns (nil, false) if the inner isn't such a literal.
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

// needsParens reports whether `t` must be parenthesized when substituted into a
// larger expression to preserve precedence.
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

// groupBalanced reports whether the opener at index 0 closes exactly at the
// last index (i.e., the entire slice is one balanced group).
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

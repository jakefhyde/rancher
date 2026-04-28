package render

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// preprocess rewrites the role-DSL sugar
//
//	{{ if etcd & !controlplane }} ... {{ fi }}
//
// into legal Go text/template syntax
//
//	{{- if and (hasRole .Node "etcd") (not (hasRole .Node "controlplane")) }} ... {{- end }}
//
// Sugar triggers only on `if` expressions whose body uses solely the role-DSL
// grammar (identifiers + & | ! and parentheses). Anything else passes through
// unchanged so authors may freely mix sugar and stdlib syntax. `{{ end }}` is
// always the standard Go template form; `{{ fi }}` is the sugar terminator
// and only ever matched alongside a sugar `if`.
func preprocess(body string) (string, error) {
	body, err := rewriteIfs(body)
	if err != nil {
		return "", err
	}
	return rewriteFi(body), nil
}

var (
	ifRE = regexp.MustCompile(`\{\{-?\s*if\s+([^}]+?)\s*-?\}\}`)
	fiRE = regexp.MustCompile(`\{\{-?\s*fi\s*-?\}\}`)
)

func rewriteIfs(body string) (string, error) {
	var firstErr error
	out := ifRE.ReplaceAllStringFunc(body, func(match string) string {
		if firstErr != nil {
			return match
		}
		// Extract the expression body.
		sub := ifRE.FindStringSubmatch(match)
		if len(sub) != 2 {
			return match
		}
		expr := sub[1]
		if !looksLikeRoleDSL(expr) {
			return match
		}
		parsed, err := parseRoleExpr(expr)
		if err != nil {
			firstErr = fmt.Errorf("preprocess: cannot parse role expression %q: %w", expr, err)
			return match
		}
		return "{{- if " + parsed + " }}"
	})
	return out, firstErr
}

func rewriteFi(body string) string {
	return fiRE.ReplaceAllString(body, "{{- end }}")
}

// looksLikeRoleDSL is a fast pre-check: returns true when the expression
// is non-empty and uses only the DSL grammar — identifiers, parentheses,
// whitespace, and the &|! operators. The presence of any other character
// (a quote, a dot, a digit-run that touches a non-identifier char, etc.)
// indicates the author is using stdlib template syntax (`eq`, dot
// expressions, quoted literals) and the expression passes through
// unchanged.
//
// A single identifier like `{{ if etcd }}` qualifies as sugar: stdlib
// `{{ if etcd }}` would fail at execution since `etcd` isn't a valid
// template ident, so the sugar interpretation is the only sensible one.
func looksLikeRoleDSL(expr string) bool {
	hasIdent := false
	for _, r := range expr {
		switch {
		case r == '&' || r == '|' || r == '!':
		case r == '(' || r == ')':
		case unicode.IsSpace(r):
		case isIdentRune(r):
			hasIdent = true
		default:
			return false
		}
	}
	return hasIdent
}

func isIdentRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// --- DSL parser (precedence climbing) ---------------------------------------

type dslTokenKind int

const (
	tokIdent dslTokenKind = iota
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
	tokEOF
)

type dslToken struct {
	kind dslTokenKind
	text string
}

// parseRoleExpr lexes and parses an expression in the role DSL and emits
// the equivalent Go template expression as a parenthesised string.
func parseRoleExpr(expr string) (string, error) {
	toks, err := lexRoleExpr(expr)
	if err != nil {
		return "", err
	}
	p := &dslParser{toks: toks}
	out, err := p.parseOr()
	if err != nil {
		return "", err
	}
	if p.peek().kind != tokEOF {
		return "", fmt.Errorf("trailing input at %q", p.peek().text)
	}
	return out, nil
}

func lexRoleExpr(expr string) ([]dslToken, error) {
	var toks []dslToken
	i := 0
	for i < len(expr) {
		r := rune(expr[i])
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '&':
			toks = append(toks, dslToken{kind: tokAnd, text: "&"})
			i++
		case r == '|':
			toks = append(toks, dslToken{kind: tokOr, text: "|"})
			i++
		case r == '!':
			toks = append(toks, dslToken{kind: tokNot, text: "!"})
			i++
		case r == '(':
			toks = append(toks, dslToken{kind: tokLParen, text: "("})
			i++
		case r == ')':
			toks = append(toks, dslToken{kind: tokRParen, text: ")"})
			i++
		case isIdentRune(r):
			j := i
			for j < len(expr) && isIdentRune(rune(expr[j])) {
				j++
			}
			toks = append(toks, dslToken{kind: tokIdent, text: expr[i:j]})
			i = j
		default:
			return nil, fmt.Errorf("unexpected rune %q", r)
		}
	}
	toks = append(toks, dslToken{kind: tokEOF})
	return toks, nil
}

type dslParser struct {
	toks []dslToken
	pos  int
}

func (p *dslParser) peek() dslToken { return p.toks[p.pos] }
func (p *dslParser) next() dslToken {
	t := p.toks[p.pos]
	p.pos++
	return t
}

func (p *dslParser) parseOr() (string, error) {
	left, err := p.parseAnd()
	if err != nil {
		return "", err
	}
	for p.peek().kind == tokOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return "", err
		}
		left = "(or " + left + " " + right + ")"
	}
	return left, nil
}

func (p *dslParser) parseAnd() (string, error) {
	left, err := p.parseNot()
	if err != nil {
		return "", err
	}
	for p.peek().kind == tokAnd {
		p.next()
		right, err := p.parseNot()
		if err != nil {
			return "", err
		}
		left = "(and " + left + " " + right + ")"
	}
	return left, nil
}

func (p *dslParser) parseNot() (string, error) {
	if p.peek().kind == tokNot {
		p.next()
		inner, err := p.parseNot()
		if err != nil {
			return "", err
		}
		return "(not " + inner + ")", nil
	}
	return p.parseAtom()
}

func (p *dslParser) parseAtom() (string, error) {
	tok := p.next()
	switch tok.kind {
	case tokIdent:
		// Quote-safe: identifiers are letters/digits/underscore/hyphen
		// only (enforced by the lexer), so plain string interpolation
		// is fine.
		return `(hasRole .Node "` + strings.ToLower(tok.text) + `")`, nil
	case tokLParen:
		inner, err := p.parseOr()
		if err != nil {
			return "", err
		}
		if p.peek().kind != tokRParen {
			return "", fmt.Errorf("expected ')' got %q", p.peek().text)
		}
		p.next()
		return inner, nil
	default:
		return "", fmt.Errorf("unexpected token %q", tok.text)
	}
}

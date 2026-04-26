package oscilloscope

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// expr is a compiled expression node.
type exprNode interface {
	eval(env map[string]float64) float64
}

type litNode struct{ v float64 }

func (e litNode) eval(_ map[string]float64) float64 { return e.v }

type varNode struct{ name string }

func (e varNode) eval(env map[string]float64) float64 { return env[e.name] }

type binNode struct {
	op   byte
	l, r exprNode
}

func (e binNode) eval(env map[string]float64) float64 {
	l, r := e.l.eval(env), e.r.eval(env)
	switch e.op {
	case '+':
		return l + r
	case '-':
		return l - r
	case '*':
		return l * r
	case '/':
		if r == 0 {
			return 0
		}
		return l / r
	}
	return 0
}

type negNode struct{ e exprNode }

func (u negNode) eval(env map[string]float64) float64 { return -u.e.eval(env) }

type fnNode struct {
	name string
	arg  exprNode
}

func (e fnNode) eval(env map[string]float64) float64 {
	v := e.arg.eval(env)
	switch e.name {
	case "sin":
		return math.Sin(v)
	case "cos":
		return math.Cos(v)
	case "tan":
		return math.Tan(v)
	case "abs":
		return math.Abs(v)
	case "sqrt":
		return math.Sqrt(v)
	case "exp":
		return math.Exp(v)
	case "log":
		return math.Log(v)
	case "floor":
		return math.Floor(v)
	case "ceil":
		return math.Ceil(v)
	}
	return 0
}

// parser is a recursive descent parser for the expression language.
type exprParser struct {
	s   string
	pos int
}

// compile parses src and returns a compiled expression tree, or an error.
// Supported: t, a, b, PI; sin, cos, tan, abs, sqrt, exp, log, floor, ceil;
// +, -, *, /; parentheses; numeric literals.
func compile(src string) (exprNode, error) {
	p := &exprParser{s: strings.TrimSpace(src)}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.pos != len(p.s) {
		return nil, fmt.Errorf("unexpected %q at position %d", p.s[p.pos:], p.pos)
	}
	return e, nil
}

func (p *exprParser) skipWS() {
	for p.pos < len(p.s) && unicode.IsSpace(rune(p.s[p.pos])) {
		p.pos++
	}
}

func (p *exprParser) peek() byte {
	p.skipWS()
	if p.pos >= len(p.s) {
		return 0
	}
	return p.s[p.pos]
}

// parseExpr: term (('+' | '-') term)*
func (p *exprParser) parseExpr() (exprNode, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peek()
		if op != '+' && op != '-' {
			break
		}
		p.pos++
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		left = binNode{op: op, l: left, r: right}
	}
	return left, nil
}

// parseTerm: factor (('*' | '/') factor)*
func (p *exprParser) parseTerm() (exprNode, error) {
	left, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peek()
		if op != '*' && op != '/' {
			break
		}
		p.pos++
		right, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		left = binNode{op: op, l: left, r: right}
	}
	return left, nil
}

// parseFactor: '-' factor | '(' expr ')' | number | ident | ident '(' expr ')'
func (p *exprParser) parseFactor() (exprNode, error) {
	p.skipWS()
	if p.pos >= len(p.s) {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	ch := p.s[p.pos]

	if ch == '-' {
		p.pos++
		e, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		return negNode{e}, nil
	}

	if ch == '(' {
		p.pos++
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.pos >= len(p.s) || p.s[p.pos] != ')' {
			return nil, fmt.Errorf("expected ')'")
		}
		p.pos++
		return e, nil
	}

	if ch >= '0' && ch <= '9' || ch == '.' {
		start := p.pos
		for p.pos < len(p.s) {
			c := p.s[p.pos]
			if c >= '0' && c <= '9' || c == '.' {
				p.pos++
			} else if (c == 'e' || c == 'E') && p.pos > start {
				p.pos++
				if p.pos < len(p.s) && (p.s[p.pos] == '+' || p.s[p.pos] == '-') {
					p.pos++
				}
			} else {
				break
			}
		}
		v, err := strconv.ParseFloat(p.s[start:p.pos], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", p.s[start:p.pos])
		}
		return litNode{v}, nil
	}

	if unicode.IsLetter(rune(ch)) || ch == '_' {
		start := p.pos
		for p.pos < len(p.s) && (unicode.IsLetter(rune(p.s[p.pos])) || unicode.IsDigit(rune(p.s[p.pos])) || p.s[p.pos] == '_') {
			p.pos++
		}
		name := p.s[start:p.pos]
		p.skipWS()
		if p.pos < len(p.s) && p.s[p.pos] == '(' {
			p.pos++
			arg, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			p.skipWS()
			if p.pos >= len(p.s) || p.s[p.pos] != ')' {
				return nil, fmt.Errorf("expected ')' after argument to %s", name)
			}
			p.pos++
			return fnNode{name: name, arg: arg}, nil
		}
		return varNode{name: name}, nil
	}

	return nil, fmt.Errorf("unexpected %q at position %d", ch, p.pos)
}

// signalGenerator produces X/Y sample pairs from compiled math expressions.
type signalGenerator struct {
	xExpr, yExpr exprNode
	t             float64 // current time in seconds
	sampleRate    float64
	aSmooth       float64 // low-pass filtered 'a' parameter
	bSmooth       float64 // low-pass filtered 'b' parameter
	env           map[string]float64
}

func newSignalGenerator(cfg Config, sampleRate float64) (*signalGenerator, error) {
	xe, err := compile(cfg.XExpression)
	if err != nil {
		return nil, fmt.Errorf("x-expression: %w", err)
	}
	ye, err := compile(cfg.YExpression)
	if err != nil {
		return nil, fmt.Errorf("y-expression: %w", err)
	}
	aTarget := cfg.AValue * math.Pow(10, float64(cfg.AExponent))
	bTarget := cfg.BValue * math.Pow(10, float64(cfg.BExponent))
	return &signalGenerator{
		xExpr:      xe,
		yExpr:      ye,
		sampleRate: sampleRate,
		aSmooth:    aTarget,
		bSmooth:    bTarget,
		env:        map[string]float64{"PI": math.Pi},
	}, nil
}

// generate produces n samples using the current expressions and parameters.
// Parameter changes are smoothed with a first-order low-pass filter.
func (g *signalGenerator) generate(n int, cfg Config) [][2]float64 {
	aTarget := cfg.AValue * math.Pow(10, float64(cfg.AExponent))
	bTarget := cfg.BValue * math.Pow(10, float64(cfg.BExponent))
	dt := 1.0 / g.sampleRate
	result := make([][2]float64, n)
	for i := range result {
		g.aSmooth += (aTarget - g.aSmooth) * 0.01
		g.bSmooth += (bTarget - g.bSmooth) * 0.01
		g.env["t"] = g.t
		g.env["a"] = g.aSmooth
		g.env["b"] = g.bSmooth
		result[i][0] = g.xExpr.eval(g.env)
		result[i][1] = g.yExpr.eval(g.env)
		g.t += dt
	}
	return result
}

// setExprs recompiles the expression strings from cfg.
func (g *signalGenerator) setExprs(cfg Config) error {
	xe, err := compile(cfg.XExpression)
	if err != nil {
		return fmt.Errorf("x-expression: %w", err)
	}
	ye, err := compile(cfg.YExpression)
	if err != nil {
		return fmt.Errorf("y-expression: %w", err)
	}
	g.xExpr = xe
	g.yExpr = ye
	return nil
}

// Package callexpr parses the call expression accepted by `climcp call`.
//
// Two argument styles are supported inside the parentheses:
//
//	server.operation({"foo": 1, "bar": "hej"})   // a JSON object
//	server.operation(foo: 1, bar: 'hej')         // collapsed function-call form
//
// Both produce the same arguments map. The collapsed form is lenient: keys may
// be bare identifiers or quoted strings, strings may use single or double
// quotes, and values may be objects, arrays, numbers, booleans, or null.
package callexpr

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Call is a parsed invocation.
type Call struct {
	Server    string
	Operation string
	Arguments map[string]interface{}
}

// Parse splits "server.operation(args)" and parses the argument body.
func Parse(expr string) (*Call, error) {
	expr = strings.TrimSpace(expr)
	open := strings.IndexByte(expr, '(')
	if open < 0 || !strings.HasSuffix(expr, ")") {
		return nil, fmt.Errorf("expected the form server.operation(args), got %q", expr)
	}

	head := strings.TrimSpace(expr[:open])
	body := expr[open+1 : len(expr)-1]

	dot := strings.IndexByte(head, '.')
	if dot < 0 {
		return nil, fmt.Errorf("missing '.' between server and operation in %q", head)
	}
	server := strings.TrimSpace(head[:dot])
	operation := strings.TrimSpace(head[dot+1:])
	if server == "" || operation == "" {
		return nil, fmt.Errorf("both server and operation must be non-empty in %q", head)
	}

	args, err := parseArguments(body)
	if err != nil {
		return nil, err
	}
	return &Call{Server: server, Operation: operation, Arguments: args}, nil
}

// parseArguments parses the body between the parentheses into a map.
func parseArguments(body string) (map[string]interface{}, error) {
	p := &parser{src: []rune(body)}
	p.skipSpace()
	if p.eof() {
		return map[string]interface{}{}, nil
	}

	// A leading '{' means the whole body is a single JSON-ish object.
	if p.peek() == '{' {
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if !p.eof() {
			return nil, fmt.Errorf("unexpected trailing input after object at position %d", p.pos)
		}
		m, ok := v.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("top-level arguments must be an object")
		}
		return m, nil
	}

	// Otherwise it's the collapsed `key: value, key: value` form.
	return p.parseObjectBody(false)
}

type parser struct {
	src []rune
	pos int
}

func (p *parser) eof() bool  { return p.pos >= len(p.src) }
func (p *parser) peek() rune { return p.src[p.pos] }
func (p *parser) next() rune { r := p.src[p.pos]; p.pos++; return r }

func (p *parser) skipSpace() {
	for !p.eof() && unicode.IsSpace(p.peek()) {
		p.pos++
	}
}

func (p *parser) parseValue() (interface{}, error) {
	p.skipSpace()
	if p.eof() {
		return nil, fmt.Errorf("unexpected end of input, expected a value")
	}
	switch c := p.peek(); {
	case c == '{':
		p.next()
		return p.parseObjectBody(true)
	case c == '[':
		return p.parseArray()
	case c == '\'' || c == '"':
		return p.parseString()
	default:
		return p.parseLiteral()
	}
}

// parseObjectBody parses key:value pairs. When braced is true it consumes the
// closing '}'. When false it runs until end of input (the collapsed form).
func (p *parser) parseObjectBody(braced bool) (map[string]interface{}, error) {
	obj := map[string]interface{}{}
	p.skipSpace()
	if braced && !p.eof() && p.peek() == '}' {
		p.next()
		return obj, nil
	}
	for {
		p.skipSpace()
		if p.eof() {
			if braced {
				return nil, fmt.Errorf("unterminated object, missing '}'")
			}
			break
		}

		key, err := p.parseKey()
		if err != nil {
			return nil, err
		}

		p.skipSpace()
		if p.eof() || p.next() != ':' {
			return nil, fmt.Errorf("expected ':' after key %q", key)
		}

		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		obj[key] = val

		p.skipSpace()
		if p.eof() {
			if braced {
				return nil, fmt.Errorf("unterminated object, missing '}'")
			}
			break
		}
		switch p.peek() {
		case ',':
			p.next()
			// Allow a trailing comma before a closing brace / end.
			p.skipSpace()
			if braced && !p.eof() && p.peek() == '}' {
				p.next()
				return obj, nil
			}
			if !braced && p.eof() {
				return obj, nil
			}
			continue
		case '}':
			if !braced {
				return nil, fmt.Errorf("unexpected '}' at position %d", p.pos)
			}
			p.next()
			return obj, nil
		default:
			return nil, fmt.Errorf("expected ',' or '}' at position %d, got %q", p.pos, string(p.peek()))
		}
	}
	return obj, nil
}

func (p *parser) parseArray() ([]interface{}, error) {
	p.next() // consume '['
	arr := []interface{}{}
	p.skipSpace()
	if !p.eof() && p.peek() == ']' {
		p.next()
		return arr, nil
	}
	for {
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
		p.skipSpace()
		if p.eof() {
			return nil, fmt.Errorf("unterminated array, missing ']'")
		}
		switch p.next() {
		case ',':
			p.skipSpace()
			if !p.eof() && p.peek() == ']' {
				p.next()
				return arr, nil
			}
			continue
		case ']':
			return arr, nil
		default:
			return nil, fmt.Errorf("expected ',' or ']' in array at position %d", p.pos)
		}
	}
}

func (p *parser) parseKey() (string, error) {
	if c := p.peek(); c == '\'' || c == '"' {
		return p.parseString()
	}
	// Bare identifier key.
	start := p.pos
	for !p.eof() {
		c := p.peek()
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '-' || c == '.' {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return "", fmt.Errorf("expected a key at position %d", p.pos)
	}
	return string(p.src[start:p.pos]), nil
}

func (p *parser) parseString() (string, error) {
	quote := p.next()
	var sb strings.Builder
	for !p.eof() {
		c := p.next()
		if c == '\\' {
			if p.eof() {
				return "", fmt.Errorf("unterminated escape in string")
			}
			esc := p.next()
			switch esc {
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case 'r':
				sb.WriteRune('\r')
			case '\\', '\'', '"', '/':
				sb.WriteRune(esc)
			default:
				sb.WriteRune('\\')
				sb.WriteRune(esc)
			}
			continue
		}
		if c == quote {
			return sb.String(), nil
		}
		sb.WriteRune(c)
	}
	return "", fmt.Errorf("unterminated string literal")
}

// parseLiteral handles numbers, true/false/null.
func (p *parser) parseLiteral() (interface{}, error) {
	start := p.pos
	for !p.eof() {
		c := p.peek()
		if unicode.IsSpace(c) || c == ',' || c == '}' || c == ']' || c == ':' {
			break
		}
		p.pos++
	}
	tok := string(p.src[start:p.pos])
	if tok == "" {
		return nil, fmt.Errorf("expected a value at position %d", start)
	}
	switch tok {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if i, err := strconv.ParseInt(tok, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(tok, 64); err == nil {
		return f, nil
	}
	return nil, fmt.Errorf("invalid value %q (strings must be quoted)", tok)
}

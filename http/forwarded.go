package http

import (
	"net/netip"
	"strings"
)

// maxForwardedParameters bounds parameters in one element; more is malformed.
const maxForwardedParameters = 16

type forwardedParser struct {
	input    string
	position int
}

// forwardedElement is one RFC 7239 hop. valid is false for syntactically
// malformed elements; such an element is a trust boundary, like for=unknown.
type forwardedElement struct {
	address netip.Addr
	scheme  string
	host    string
	valid   bool
}

// forwardedLineElements splits one header line at every comma and returns at
// most the last keep element texts in order. Quotes do not protect commas: a
// client-supplied unterminated quote must not merge the hop a trusted proxy
// appended to the same line into the client's element. No for, by, proto or
// host value contains a comma, so a quoted comma only splits an element into
// invalid parts, which stop the trusted walk.
func forwardedLineElements(line string, keep int) []string {
	if keep <= 0 {
		return nil
	}
	ring := make([]string, 0, min(keep, 8))
	next := 0
	add := func(text string) {
		if strings.Trim(text, " \t") == "" {
			return
		}
		if len(ring) < keep {
			ring = append(ring, text)
			return
		}
		ring[next] = text
		next = (next + 1) % keep
	}
	start := 0
	for i := 0; i < len(line); i++ {
		if line[i] == ',' {
			add(line[start:i])
			start = i + 1
		}
	}
	add(line[start:])
	if len(ring) == keep && next != 0 {
		ordered := make([]string, 0, keep)
		ordered = append(ordered, ring[next:]...)
		ring = append(ordered, ring[:next]...)
	}
	return ring
}

// parseForwardedElement parses one element's parameters. It never fails the
// request: malformed syntax, repeated parameters or an unusable for value make
// the element invalid so the trusted walk stops there.
func parseForwardedElement(text string) forwardedElement {
	p := forwardedParser{input: text}
	var element forwardedElement
	var names [maxForwardedParameters]string
	count := 0
	p.space()
	for p.position < len(p.input) {
		if p.input[p.position] == ';' {
			p.position++
			p.space()
			continue
		}
		start := p.position
		for p.position < len(p.input) && httpTokenByte(p.input[p.position]) {
			p.position++
		}
		name := p.input[start:p.position]
		if HeaderName(name).Validate() != nil || count == len(names) {
			return forwardedElement{}
		}
		// RFC 7239 forbids repeating any parameter within one element.
		for _, seen := range names[:count] {
			if strings.EqualFold(seen, name) {
				return forwardedElement{}
			}
		}
		names[count] = name
		count++
		p.space()
		if p.position == len(p.input) || p.input[p.position] != '=' {
			return forwardedElement{}
		}
		p.position++
		p.space()
		value, ok := p.value()
		if !ok {
			return forwardedElement{}
		}
		switch {
		case strings.EqualFold(name, "for"):
			address, ok := forwardedNode(value)
			if !ok {
				return forwardedElement{}
			}
			element.address = address
		case strings.EqualFold(name, "proto"):
			element.scheme = value
		case strings.EqualFold(name, "host"):
			element.host = value
		}
		p.space()
		if p.position < len(p.input) && p.input[p.position] != ';' {
			return forwardedElement{}
		}
	}
	element.valid = true
	return element
}

func (p *forwardedParser) space() {
	for p.position < len(p.input) && (p.input[p.position] == ' ' || p.input[p.position] == '\t') {
		p.position++
	}
}

func (p *forwardedParser) value() (string, bool) {
	if p.position == len(p.input) {
		return "", false
	}
	if p.input[p.position] != '"' {
		start := p.position
		for p.position < len(p.input) && httpTokenByte(p.input[p.position]) {
			p.position++
		}
		if start == p.position {
			return "", false
		}
		return p.input[start:p.position], true
	}
	p.position++
	var value strings.Builder
	for p.position < len(p.input) {
		c := p.input[p.position]
		p.position++
		if c == '"' {
			return value.String(), true
		}
		if c == '\\' {
			if p.position == len(p.input) {
				return "", false
			}
			c = p.input[p.position]
			p.position++
		}
		if c != '\t' && (c < 0x20 || c == 0x7f) || value.Len() >= maxOriginBytes {
			return "", false
		}
		value.WriteByte(c)
	}
	return "", false
}

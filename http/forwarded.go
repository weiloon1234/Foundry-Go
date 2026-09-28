package http

import (
	"net/netip"
	"strings"
)

type forwardedParser struct {
	input    string
	position int
}

type forwardedElement struct {
	address netip.Addr
	scheme  string
	host    string
}

func parseForwardedIPs(input string) ([]netip.Addr, error) {
	elements, err := parseForwarded(input)
	if err != nil {
		return nil, err
	}
	addresses := make([]netip.Addr, len(elements))
	for i, element := range elements {
		addresses[i] = element.address
	}
	return addresses, nil
}

func parseForwarded(input string) ([]forwardedElement, error) {
	p := forwardedParser{input: input}
	p.space()
	if p.position == len(input) {
		return nil, BadRequest
	}
	var chain []forwardedElement
	for {
		if len(chain) == maxProxyHops {
			return nil, BadRequest
		}
		seen := make(map[string]bool)
		var element forwardedElement
		for p.position < len(input) && input[p.position] != ',' {
			if input[p.position] == ';' {
				p.position++
				p.space()
				continue
			}
			start := p.position
			for p.position < len(input) && httpTokenByte(input[p.position]) {
				p.position++
			}
			name := input[start:p.position]
			if err := HeaderName(name).Validate(); err != nil {
				return nil, BadRequest
			}
			name = strings.ToLower(name)
			if seen[name] {
				return nil, BadRequest
			}
			seen[name] = true
			p.space()
			if p.position == len(input) || input[p.position] != '=' {
				return nil, BadRequest
			}
			p.position++
			p.space()
			value, err := p.value()
			if err != nil {
				return nil, err
			}
			if name == "for" {
				element.address, err = forwardedNode(value)
				if err != nil {
					return nil, err
				}
			}
			if name == "proto" {
				element.scheme = value
			}
			if name == "host" {
				element.host = value
			}
			p.space()
			if p.position < len(input) && input[p.position] != ';' && input[p.position] != ',' {
				return nil, BadRequest
			}
		}
		chain = append(chain, element)
		if p.position == len(input) {
			return chain, nil
		}
		p.position++
		p.space()
	}
}

func (p *forwardedParser) space() {
	for p.position < len(p.input) && (p.input[p.position] == ' ' || p.input[p.position] == '\t') {
		p.position++
	}
}

func (p *forwardedParser) value() (string, error) {
	if p.position == len(p.input) {
		return "", BadRequest
	}
	if p.input[p.position] != '"' {
		start := p.position
		for p.position < len(p.input) && httpTokenByte(p.input[p.position]) {
			p.position++
		}
		if start == p.position {
			return "", BadRequest
		}
		return p.input[start:p.position], nil
	}
	p.position++
	var value strings.Builder
	for p.position < len(p.input) {
		c := p.input[p.position]
		p.position++
		if c == '"' {
			return value.String(), nil
		}
		if c == '\\' {
			if p.position == len(p.input) {
				return "", BadRequest
			}
			c = p.input[p.position]
			p.position++
		}
		if c != '\t' && (c < 0x20 || c == 0x7f) {
			return "", BadRequest
		}
		value.WriteByte(c)
	}
	return "", BadRequest
}

package httpclient

import (
	"net/url"
	"strings"
)

const maxURLBytes = 16 << 10

func parseURL(text string) (*url.URL, error) {
	if text == "" || len(text) > maxURLBytes {
		return nil, invalid()
	}
	u, err := url.Parse(text)
	if err != nil || u.User != nil || u.Fragment != "" || u.RawFragment != "" || u.Opaque != "" {
		return nil, invalid()
	}
	for _, r := range text {
		if r < 33 || r == 127 || r == '\\' {
			return nil, invalid()
		}
	}
	if _, err := url.ParseQuery(u.RawQuery); err != nil {
		return nil, invalid()
	}
	return u, nil
}
func baseURL(text string) (*url.URL, error) {
	u, err := parseURL(text)
	if err != nil {
		return nil, err
	}
	if u.Hostname() == "" || u.Scheme != "http" && u.Scheme != "https" || u.RawQuery != "" || u.ForceQuery {
		return nil, invalid()
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	if u.RawPath != "" {
		u.RawPath = strings.TrimRight(u.RawPath, "/") + "/"
	}
	return u, nil
}
func requestURL(base *url.URL, text string) (*url.URL, error) {
	u, err := parseURL(text)
	if err != nil {
		return nil, err
	}
	if base == nil {
		if u.Hostname() == "" || u.Scheme != "http" && u.Scheme != "https" {
			return nil, invalid()
		}
		return u, nil
	}
	// A named upstream never carries default credentials to another origin.
	if u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "//") {
		return nil, invalid()
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return nil, invalid()
		}
	}
	escaped := u.EscapedPath()
	if strings.HasPrefix(u.Path, "/") && !strings.HasPrefix(escaped, "/") {
		return nil, invalid()
	}
	u.Path = strings.TrimLeft(u.Path, "/")
	u.RawPath = strings.TrimLeft(escaped, "/")
	result := base.ResolveReference(u)
	if len(result.String()) > maxURLBytes {
		return nil, invalid()
	}
	return result, nil
}

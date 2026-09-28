package http

import (
	"mime"
	"path"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// SPAConfig declares an unmatched-route fallback. Prefix and Exclude are
// absolute URL prefixes. Index is relative to Assets. Only explicit text/html
// navigation to extensionless missing paths falls back to Index. Existing files
// still serve normally. Declared API routes and native method errors take precedence.
type SPAConfig struct {
	Prefix       string
	Index        AssetPath
	Exclude      []string
	CacheControl HeaderValue
}

func DefaultSPAConfig() SPAConfig {
	return SPAConfig{Prefix: "/", Index: "index.html", CacheControl: "no-cache"}
}
func (c SPAConfig) Validate() error {
	if _, err := assetPrefix(c.Prefix); err != nil {
		return err
	}
	if err := assetFilePath(c.Index); err != nil {
		return err
	}
	if err := c.CacheControl.Validate(); err != nil {
		return err
	}
	if len(c.Exclude) > 64 {
		return fault.New(fault.Invalid, "SPA exclusions exceed limit")
	}
	seen := make(map[string]bool, len(c.Exclude))
	for _, prefix := range c.Exclude {
		if _, err := assetPrefix(prefix); err != nil {
			return err
		}
		if seen[prefix] {
			return fault.New(fault.Duplicate, "duplicate SPA exclusion")
		}
		seen[prefix] = true
	}
	return nil
}
func (c SPAConfig) snapshot() SPAConfig { c.Exclude = slices.Clone(c.Exclude); return c }
func matchesAssetPrefix(location, prefix string) bool {
	return prefix == "" || prefix == "/" || location == prefix || strings.HasPrefix(location, prefix+"/")
}
func htmlNavigation(values []string, name string) bool {
	if path.Ext(strings.TrimSuffix(name, "/")) != "" || len(values) > 32 {
		return false
	}
	total := 0
	for _, line := range values {
		if len(line) > 4096-total {
			return false
		}
		total += len(line)
	}
	count := 0
	eligible := false
	for _, line := range values {
		start := 0
		quoted := false
		escaped := false
		for i := 0; i <= len(line); i++ {
			if i < len(line) {
				c := line[i]
				if escaped {
					escaped = false
					continue
				}
				if quoted && c == '\\' {
					escaped = true
					continue
				}
				if c == '"' {
					quoted = !quoted
					continue
				}
				if c != ',' || quoted {
					continue
				}
			}
			if quoted || escaped {
				return false
			}
			count++
			if count > 32 {
				return false
			}
			media, params, err := mime.ParseMediaType(strings.TrimSpace(line[start:i]))
			start = i + 1
			if err != nil || media != "text/html" {
				continue
			}
			quality, exists := params["q"]
			if !exists || positiveHTMLQuality(quality) {
				eligible = true
			}
		}
	}
	return eligible
}
func positiveHTMLQuality(text string) bool {
	if text == "1" {
		return true
	}
	if text == "0" {
		return false
	}
	if len(text) < 2 || len(text) > 5 || text[1] != '.' || (text[0] != '0' && text[0] != '1') {
		return false
	}
	positive := text[0] == '1'
	for _, digit := range text[2:] {
		if digit < '0' || digit > '9' || text[0] == '1' && digit != '0' {
			return false
		}
		positive = positive || digit != '0'
	}
	return positive
}

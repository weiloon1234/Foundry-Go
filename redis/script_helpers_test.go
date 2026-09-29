package redis

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

func scriptDigest(source string) string {
	sum := sha1.Sum([]byte(source))
	return hex.EncodeToString(sum[:])
}

// runsScript reports whether hooked command arguments execute source through
// EVAL or its EVALSHA digest.
func runsScript(args []any, source string) bool {
	if len(args) < 2 {
		return false
	}
	name, _ := args[0].(string)
	body, _ := args[1].(string)
	switch strings.ToLower(name) {
	case "eval":
		return body == source
	case "evalsha":
		return body == scriptDigest(source)
	}
	return false
}

// forceEval rewrites a hooked EVALSHA/EVAL into EVAL of replacement, so a test
// fixture can instrument the production script regardless of script caching.
func forceEval(args []any, replacement string) {
	args[0], args[1] = "eval", replacement
}

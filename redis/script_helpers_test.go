package redis

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	driver "github.com/redis/go-redis/v9"
)

// Hooks see raw Redis replies before the EVAL wrapper normalizes NOSCRIPT.
// Match Script.Run's raw-prefix and sentinel handling: neither ran the script.
func scriptDidNotRun(err error) bool {
	return errors.Is(err, driver.ErrNoScript) || driver.HasErrorPrefix(err, "NOSCRIPT")
}

type scriptReplyError string

func (e scriptReplyError) Error() string { return string(e) }
func (scriptReplyError) RedisError()     {}

func TestScriptDidNotRun(t *testing.T) {
	for _, err := range []error{driver.ErrNoScript, scriptReplyError("NOSCRIPT uncached digest"), scriptReplyError("ERR NOSCRIPT uncached digest"), fmt.Errorf("wrapped: %w", driver.ErrNoScript)} {
		if !scriptDidNotRun(err) {
			t.Fatalf("script miss was counted as execution: %v", err)
		}
	}
	for _, err := range []error{nil, errors.New("acknowledgement lost"), scriptReplyError("WRONGTYPE invalid value")} {
		if scriptDidNotRun(err) {
			t.Fatalf("reply did not prove non-execution: %v", err)
		}
	}
}

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

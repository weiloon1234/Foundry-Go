package capturedisclosure

import (
	"strings"
	"unicode"

	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/http"
)

// Names are extra credential names from deployment settings, matched exactly
// and case-insensitively.
type Names map[string]struct{}

// NewNames builds the extra-name set. Settings validation bounds the input.
func NewNames(names []string) Names {
	if len(names) == 0 {
		return nil
	}
	set := make(Names, len(names))
	for _, name := range names {
		set[strings.ToLower(name)] = struct{}{}
	}
	return set
}

// keyCompounds are joined names that end in the word key and hold a secret.
var keyCompounds = []string{"apikey", "accesskey", "secretkey", "privatekey", "subscriptionkey", "signingkey"}

// CredentialName decides header and body field names in both capture
// directions. It errs toward masking: a false positive hides one value, a
// false negative stores a vendor key for every developer to read.
//
//	Authorization, Set-Cookie, X-Api-Key, client_secret -> record.SensitiveName
//	auth, X-Auth-Token, x_auth, authToken               -> the word auth
//	key, X-RapidAPI-Key, Ocp-Apim-Subscription-Key      -> a separated final word key
//	subscriptionKey, accesskey                          -> a listed key compound
//	Idempotency-Key, monkey, author, oauth_state        -> visible
func CredentialName(name string, extra Names) bool {
	if record.SensitiveName(name) {
		return true
	}
	lower := strings.ToLower(name)
	if _, ok := extra[lower]; ok {
		return true
	}
	return keyName(lower) || hasWord(name, "auth")
}

// CredentialQueryName adds the framework's own bearer parameters (a ticket or
// a signed-URL signature) and the words sig and signature, so presigned URLs
// such as X-Amz-Signature cannot be replayed from a viewer. Signature headers
// stay visible: an HMAC over one body grants nothing.
func CredentialQueryName(name string, extra Names) bool {
	return CredentialName(name, extra) || http.CredentialQueryName(name) || hasWord(name, "sig") || hasWord(name, "signature")
}

func keyName(lower string) bool {
	if lower == "key" {
		return true
	}
	for _, separator := range []string{"-", "_", "."} {
		if before, found := strings.CutSuffix(lower, separator+"key"); found {
			return !strings.HasSuffix(before, "idempotency")
		}
	}
	for _, compound := range keyCompounds {
		if strings.HasSuffix(lower, compound) {
			return true
		}
	}
	return false
}

// hasWord splits name on non-letters and lower-to-upper case boundaries and
// reports whether any word equals word.
func hasWord(name, word string) bool {
	runes := []rune(name)
	start := -1
	for i := 0; i <= len(runes); i++ {
		boundary := i == len(runes) || !unicode.IsLetter(runes[i]) || i > 0 && unicode.IsUpper(runes[i]) && unicode.IsLower(runes[i-1])
		if boundary {
			if start >= 0 && strings.EqualFold(string(runes[start:i]), word) {
				return true
			}
			start = -1
		}
		if i < len(runes) && unicode.IsLetter(runes[i]) && start < 0 {
			start = i
		}
	}
	return false
}

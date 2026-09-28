package i18n

import (
	"strings"
	"testing"
)

func TestMessageKeysUseSemanticIdentityIndependentlyOfLocale(t *testing.T) {
	for _, key := range []MessageKey{"reports.members.name", "checkout.line-item_1", MessageKey(strings.Repeat("a", 128))} {
		if err := key.Validate(); err != nil {
			t.Fatal("valid UI message identity rejected", err)
		}
	}
	for _, key := range []MessageKey{"", "Report.Name", "en US", "label/name", "field\x00name", "名", MessageKey(strings.Repeat("a", 129))} {
		if key.Validate() == nil {
			t.Fatal("invalid UI message identity accepted")
		}
	}
}

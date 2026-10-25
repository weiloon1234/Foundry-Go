package http

import (
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/testkit"
)

func TestSigningSourceDiagnosticsSurviveContainment(t *testing.T) {
	clock := testkit.NewClock(urlTestTime)
	signer := cookieTestSigner(t, cookieTestKeys(t, "signing"), clock)
	urlSigner := urlTestSigner(t, clock)
	for _, source := range []struct {
		name string
		kind error
		read func() error
	}{
		{"signed cookie", ErrInvalidSignedCookie, func() error {
			_, err := signer.open("preference", DefaultCookieOptions(), "invalid")
			return err
		}},
		{"encrypted cookie", ErrInvalidEncryptedCookie, func() error {
			_, err := (CookieEncrypter{}).open(t.Context(), "preference", DefaultCookieOptions(), "invalid")
			return err
		}},
		{"signed URL", ErrInvalidSignedURL, func() error {
			_, err := urlSigner.open(t.Context(), urlTestSpec(), "/download", urlTestOrigin, "/download")
			return err
		}},
	} {
		t.Run(source.name, func(t *testing.T) {
			original := source.read()
			if original == nil {
				t.Fatal("invalid signing envelope succeeded")
			}
			result := callback.Isolated("HTTP signing", func() error { return original })
			if !errors.Is(result, original) || !errors.Is(result, source.kind) {
				t.Fatal("signing source lost typed sentinel or original identity")
			}
			want := "HTTP signing: " + source.kind.Error()
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, result); got != want {
					t.Fatalf("signing diagnostic = %q, want %q", got, want)
				}
			}
			for _, exit := range []bool{false, true} {
				cause := &privateTransportSourceDiagnostic{exit: exit}
				private := errors.Join(source.kind, cause)
				redacted := callback.Isolated("HTTP signing", func() error { return private })
				var retained *privateTransportSourceDiagnostic
				if !errors.Is(redacted, private) || !errors.Is(redacted, source.kind) || !errors.As(redacted, &retained) || retained != cause {
					t.Fatal("signing boundary lost application identity or classification")
				}
				for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
					if got := fmt.Sprintf(format, redacted); got != "HTTP signing failed" {
						t.Fatalf("application signing diagnostic = %q", got)
					}
				}
				if cause.calls.Load() != 0 {
					t.Fatal("signing boundary formatted an application cause")
				}
			}
		})
	}
}

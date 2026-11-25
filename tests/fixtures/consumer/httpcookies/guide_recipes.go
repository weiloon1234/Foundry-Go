package httpcookies

import (
	"context"
	"net/http"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/encryption"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func rememberPreference(ctx context.Context, writer http.ResponseWriter, request *http.Request) (bool, bool, error) {
	// doc-example:begin http-cookies-1
	options := foundryhttp.DefaultCookieOptions()
	options.MaxAge = time.Hour
	remember := foundryhttp.DefineCookie(
		"remember",
		foundryhttp.BoolCookie[bool](),
		options,
	)

	choice, err := remember.Read(request)
	if err != nil {
		return false, false, err
	}
	enabled, present := choice.Get() // false can be a present choice.
	if err := remember.Set(ctx, writer, true); err != nil {
		return false, false, err
	}
	// doc-example:end http-cookies-1
	return enabled, present, nil
}

func setSignedPreference(ctx context.Context, writer http.ResponseWriter, configuredKey secret.String, applicationClock clock.Clock, remember foundryhttp.Cookie[bool]) error {
	// doc-example:begin http-cookies-2
	keys, err := foundryhttp.NewSigningKeys(
		foundryhttp.SigningKey{ID: "current", Secret: configuredKey},
		// Optional previous keys remain valid during rotation.
	)
	if err != nil {
		return err
	}
	signer, err := foundryhttp.NewCookieSigner(keys, applicationClock)
	if err != nil {
		return err
	}
	signedRemember := remember.Signed(signer)
	if err := signedRemember.Set(ctx, writer, true); err != nil {
		return err
	}
	// doc-example:end http-cookies-2
	return nil
}

func setEncryptedPreference(ctx context.Context, writer http.ResponseWriter, keyring *encryption.Keyring, applicationClock clock.Clock, remember foundryhttp.Cookie[bool]) error {
	// doc-example:begin http-cookies-3
	encrypter, err := foundryhttp.NewCookieEncrypter(keyring, applicationClock)
	if err != nil {
		return err
	}
	private := remember.Encrypted(encrypter)
	if err := private.Set(ctx, writer, true); err != nil {
		return err
	}
	// doc-example:end http-cookies-3
	return nil
}

// Package httpcookies demonstrates typed preference cookies over native HTTP.
// These are client preferences, not authenticated identity or authorization.
package httpcookies

import (
	"encoding/json"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
	stdhttp "net/http"
	"time"
)

type Locale string

func options() foundryhttp.CookieOptions {
	result := foundryhttp.DefaultCookieOptions()
	result.MaxAge = time.Hour
	return result
}

var SelectedUser = foundryhttp.DefineCookie("__Host-selected-user", foundryhttp.ModelIDCookie[models.User](), options())
var Language = foundryhttp.DefineCookie("locale", foundryhttp.StringCookie[Locale](), options())
var State = foundryhttp.DefineCookie("state", foundryhttp.TextCookie[models.Status, *models.Status](), options())
var Select = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "preferences.select", Method: foundryhttp.POST, Access: foundryhttp.Public}, httpkernel.UserPathDescriptor())
var Read = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "preferences.read", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/preferences"))
var Clear = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "preferences.clear", Method: foundryhttp.DELETE, Access: foundryhttp.Public}, foundryhttp.StaticPath("/preferences"))

type Preferences struct {
	User   value.Optional[model.ID[models.User]] `json:"user,omitzero"`
	Locale value.Optional[Locale]                `json:"locale,omitzero"`
	State  value.Optional[models.Status]         `json:"state,omitzero"`
}

func Handler(keys foundryhttp.SigningKeys, applicationClock clock.Clock) (stdhttp.Handler, error) {
	signer, err := foundryhttp.NewCookieSigner(keys, applicationClock)
	if err != nil {
		return nil, err
	}
	selected := SelectedUser.Signed(signer)
	if err := selected.Validate(); err != nil {
		return nil, err
	}
	return foundryhttp.NewRouter(
		Select.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, path httpkernel.UserPath) {
			if err := selected.Set(r.Context(), w, path.User); err != nil {
				_ = foundryhttp.WriteError(w, r, err)
				return
			}
			if err := Language.Set(r.Context(), w, Locale("en-MY")); err != nil {
				_ = foundryhttp.WriteError(w, r, err)
				return
			}
			if err := State.Set(r.Context(), w, models.StatusActive); err != nil {
				_ = foundryhttp.WriteError(w, r, err)
				return
			}
			w.WriteHeader(stdhttp.StatusNoContent)
		}),
		Read.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ foundryhttp.NoPath) {
			user, err := selected.Read(r)
			if err != nil {
				_ = foundryhttp.WriteError(w, r, err)
				return
			}
			locale, err := Language.Read(r)
			if err != nil {
				_ = foundryhttp.WriteError(w, r, err)
				return
			}
			state, err := State.Read(r)
			if err != nil {
				_ = foundryhttp.WriteError(w, r, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Preferences{User: user, Locale: locale, State: state})
		}),
		Clear.HandleRaw(func(w stdhttp.ResponseWriter, r *stdhttp.Request, _ foundryhttp.NoPath) {
			for _, err := range []error{selected.Clear(w), Language.Clear(w), State.Clear(w)} {
				if err != nil {
					_ = foundryhttp.WriteError(w, r, err)
					return
				}
			}
			w.WriteHeader(stdhttp.StatusNoContent)
		}),
	)
}

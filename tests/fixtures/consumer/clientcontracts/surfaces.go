package clientcontracts

import (
	"github.com/weiloon1234/Foundry-Go/auth"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/typescript"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

// doc-example:begin portal-surfaces
var PortalSurfaces = []typescript.Surface{
	{Name: "browser", Routes: []foundryhttp.RouteID{"browser"}, Auth: &typescript.SurfaceAuth{Guard: "browser_accounts", Login: "browser.login", Refresh: "browser.refresh", Logout: "browser.logout", Profile: "browser.profile", Identifier: "login_id"}},
	{Name: "members", Paths: []string{"/members"}},
	{Name: "live", Routes: []foundryhttp.RouteID{"account.show"}, Channels: []websocket.ChannelID{"updates"}, Guards: []auth.GuardName{"client_accounts"}},
}

// doc-example:end portal-surfaces

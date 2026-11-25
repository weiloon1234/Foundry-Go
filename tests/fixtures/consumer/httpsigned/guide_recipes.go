package httpsigned

import (
	"context"
	"time"

	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin signed-preview-inputs

// UserPath identifies the user whose preview link is signed.
//
//foundry:path pattern=/users/{user}
type UserPath struct {
	User model.ID[models.User]
}

// SearchInput retains the query values included in the signature.
//
//foundry:query
type SearchInput struct {
	User     model.ID[models.User]
	Search   value.Optional[string] `query:"q"`
	Statuses []models.Status        `query:"status"`
}

// doc-example:end signed-preview-inputs

type AvatarPath = httpkernel.AssetPath

func previewURL(ctx context.Context, keys foundryhttp.SigningKeys, applicationClock clock.Clock, publicOrigin foundryhttp.Origin, userID model.ID[models.User], expiresAt time.Time, Preview foundryhttp.Endpoint[UserPath, SearchInput, foundryhttp.NoBody, foundryhttp.NoContent]) (string, error) {
	// doc-example:begin http-signed-urls-1
	signer, err := foundryhttp.NewURLSigner(keys, applicationClock)
	if err != nil {
		return "", err
	}
	preview := foundryhttp.Signed(Preview, signer)
	link, err := preview.URL(ctx, publicOrigin,
		UserPath{User: userID},
		SearchInput{
			User:     userID,
			Search:   value.Set("中文 + a/b"),
			Statuses: []models.Status{models.StatusActive},
		},
		expiresAt,
	)
	// doc-example:end http-signed-urls-1
	return link, err
}

func avatarURL(ctx context.Context, signer foundryhttp.URLSigner, publicOrigin foundryhttp.Origin, path AvatarPath, AvatarRoute foundryhttp.Route[AvatarPath]) (string, error) {
	// doc-example:begin http-signed-urls-2
	avatar := foundryhttp.SignRoute(AvatarRoute, signer)
	avatar = foundryhttp.ConfigureSignedRoute(avatar, foundryhttp.PermanentLinks())
	link, err := foundryhttp.PermanentRouteURL(avatar, ctx, publicOrigin, path)
	// doc-example:end http-signed-urls-2
	return link, err
}

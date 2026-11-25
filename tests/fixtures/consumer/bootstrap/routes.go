package bootstrap

import (
	"context"
	"image/color"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cache"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// doc-example:begin profile-response

//foundry:dto http=response
type Profile struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// doc-example:end profile-response

//foundry:multipart http=body
type UploadInput struct{ Image foundryhttp.UploadedFile }

//foundry:dto http=response
type UploadResult struct {
	Key   storage.ObjectKey `json:"key"`
	Bytes int64             `json:"bytes"`
	Width int               `json:"width"`
}

var profiles = cache.Define("bootstrap.profiles", keyspace.StringKeys[string](), cache.JSON[Profile]())

// The configured engine applies the same declaration to ordinary HTTP uploads.
// Contain enlarges small uploads, preserves aspect ratio and pads a square avatar.
var avatar = imaging.NewPlan().
	Contain(32, 32, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, imaging.Center).
	Format(imaging.PNG)

type MemberProfileInput = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]

func profileEndpoint(id foundryhttp.RouteID, path string, method foundryhttp.Method) foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, Profile] {
	idRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: id, Method: method, Access: foundryhttp.Guarded}, foundryhttp.StaticPath(path))

	return foundryhttp.DefineEndpoint(idRoute, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), ProfileResponse(200))
}
func Routes(settings Settings, services application.Services) ([]foundryhttp.RouteRegistration, error) {
	db, err := services.Database()
	if err != nil {
		return nil, err
	}
	store, err := services.Cache()
	if err != nil {
		return nil, err
	}
	disk, err := services.Disk()
	if err != nil {
		return nil, err
	}
	images, err := services.Image()
	if err != nil {
		return nil, err
	}
	cached, err := profiles.Bind(store)
	if err != nil {
		return nil, err
	}
	members, operators, err := actors(settings, db)
	if err != nil {
		return nil, err
	}

	bootstrapMemberEndpoint := profileEndpoint("bootstrap.member", "/api/member", foundryhttp.GET)

	member := foundryhttp.Authenticated(bootstrapMemberEndpoint, members).Handle(func(ctx context.Context, actor Member, _ MemberProfileInput) (Profile, error) {
		return cached.Remember(ctx, actor.ID.String(), cache.Forever(), func(context.Context) (Profile, error) {
			return Profile{Name: settings.Greeting + " " + actor.Name, Kind: "member"}, nil
		})
	})

	bootstrapOperatorEndpoint := profileEndpoint("bootstrap.operator", "/api/operator", foundryhttp.POST)

	operator := foundryhttp.Authenticated(bootstrapOperatorEndpoint, operators).Handle(func(_ context.Context, actor Operator, _ MemberProfileInput) (Profile, error) {
		return Profile{Name: actor.Department, Kind: "operator"}, nil
	})

	bootstrapAvatarRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "bootstrap.avatar", Method: foundryhttp.POST, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/api/avatar"))
	upload := foundryhttp.DefineEndpoint(bootstrapAvatarRoute, foundryhttp.EmptyQuery(), UploadInputBody(""), UploadResultResponse(201))
	upload = foundryhttp.ValidateBody(upload, validation.Rules(UploadInputValidationFields().Image, validation.FileMaxSize[foundryhttp.UploadedFile](1<<20), validation.FileContentTypes[foundryhttp.UploadedFile]("image/png")))

	saved := foundryhttp.Authenticated(upload, members).Handle(func(ctx context.Context, actor Member, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, UploadInput]) (UploadResult, error) {
		reader, err := input.Body.Image.Open(ctx)
		if err != nil {
			return UploadResult{}, err
		}
		defer reader.Close()
		result, err := images.Process(ctx, reader, avatar)
		if err != nil {
			return UploadResult{}, err
		}
		key, err := storage.ParseKey("avatars/" + actor.ID.String() + ".png")
		if err != nil {
			return UploadResult{}, err
		}
		if _, err = disk.Put(ctx, key, result.Reader(), storage.PutOptions{ContentType: storage.MediaType("image/png"), Condition: storage.IfAbsent()}); err != nil {
			return UploadResult{}, err
		}
		return UploadResult{Key: key, Bytes: result.Size(), Width: result.Info().Width}, nil
	})
	return []foundryhttp.RouteRegistration{
		member,
		operator,
		saved,
	}, nil
}

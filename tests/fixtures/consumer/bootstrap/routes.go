package bootstrap

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/imaging"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/validation"
)

//foundry:dto
type Profile struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

//foundry:multipart
type UploadInput struct{ Image http.UploadedFile }

//foundry:dto
type UploadResult struct {
	Key   storage.ObjectKey `json:"key"`
	Bytes int64             `json:"bytes"`
	Width int               `json:"width"`
}

var profiles = cache.Define("bootstrap.profiles", cache.StringKeys[string](), cache.JSON[Profile]())
var avatar = imaging.NewPlan().Fit(32, 32, false).Format(imaging.PNG)

type MemberProfileInput = http.Input[http.NoPath, http.NoQuery, http.NoBody]

func profileEndpoint(id http.RouteID, path string, method http.Method) http.Endpoint[http.NoPath, http.NoQuery, http.NoBody, Profile] {
	return http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: id, Method: method, Access: http.Guarded}, http.StaticPath(path)), http.EmptyQuery(), http.EmptyBody(), http.JSONResponse(200, ProfileJSON()))
}
func Routes(settings Settings, services application.Services) ([]http.RouteRegistration, error) {
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
	member := http.Authenticated(profileEndpoint("bootstrap.member", "/api/member", http.GET), members).Handle(func(ctx context.Context, actor Member, _ MemberProfileInput) (Profile, error) {
		return cached.Remember(ctx, actor.ID.String(), cache.Forever(), func(context.Context) (Profile, error) {
			return Profile{Name: settings.Greeting + " " + actor.Name, Kind: "member"}, nil
		})
	})
	operator := http.Authenticated(profileEndpoint("bootstrap.operator", "/api/operator", http.POST), operators).Handle(func(_ context.Context, actor Operator, _ MemberProfileInput) (Profile, error) {
		return Profile{Name: actor.Department, Kind: "operator"}, nil
	})
	upload := http.DefineEndpoint(http.DefineRoute(http.RouteSpec{ID: "bootstrap.avatar", Method: http.POST, Access: http.Guarded}, http.StaticPath("/api/avatar")), http.EmptyQuery(), http.MultipartBody(UploadInputDescriptor()), http.JSONResponse(201, UploadResultJSON())).WithBodyValidation(UploadInputValidationFields().Image.Rules(validation.FileMaxSize[http.UploadedFile](1<<20), validation.FileContentTypes[http.UploadedFile]("image/png")))
	saved := http.Authenticated(upload, members).Handle(func(ctx context.Context, actor Member, input http.Input[http.NoPath, http.NoQuery, UploadInput]) (UploadResult, error) {
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
	return []http.RouteRegistration{member, operator, saved}, nil
}

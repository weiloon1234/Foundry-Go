package clientcontracts

import (
	"context"
	"os"
	"time"

	"foundry.test/consumer/genericdto"
	"foundry.test/consumer/httppagination"
	"foundry.test/consumer/httpuploads"
	"foundry.test/consumer/localization"
	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"foundry.test/consumer/reporting"
	"foundry.test/consumer/requestflow"
	"foundry.test/consumer/unions"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/enum"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/typescript"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

type Account struct {
	ID               int64
	Display, Private string
}

func (a Account) FoundryReference() model.Reference[Account, int64] {
	return model.NewReference[Account]("client_accounts", a.ID, codec.Signed[int64]())
}
func (a Account) FoundryIdentity() (model.Identity, error) { return a.FoundryReference().Identity() }

type PublicOwner struct{}
type PrivateOwner struct{}

var Changed = foundryhttp.DefineError("consumer_changed", 409, "The consumer changed.")
var Manage = auth.DefinePermission("consumer.manage", func(context.Context, Account) (bool, error) { return true, nil }).WithLabelKey("permissions.manage")
var Updates = websocket.Public[PublicOwner]("updates", websocket.DefineRooms(foundryhttp.IntegerPath[int64]())).WithReplay(websocket.ReplayConfig{Messages: 4, Bytes: 64 << 10, TTL: time.Minute})
var Updated = websocket.DefineOutgoing(Updates, "updated", PayloadJSON())
var Relay = websocket.DefineIncoming(Updates, "relay", PayloadJSON()).AcknowledgeAccepted()

var Echo = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "items.echo", Method: foundryhttp.POST, Access: foundryhttp.Public}, ItemPathDescriptor()),
	FiltersDescriptor(), foundryhttp.JSONBody(PayloadJSON()), foundryhttp.JSONResponse(201, PayloadJSON()),
).WithBodyValidation(validation.Parallel(PayloadValidationFields().Natural.WithLabel("Country").WithLabelKey("fields.country").Rules(validation.NonBlank[models.CountryCode](), validation.MaxLength[models.CountryCode](64), validation.Uppercase[models.CountryCode]())))

type Fixture struct {
	Router         *foundryhttp.Router
	Registry       *websocket.Registry
	Authentication *foundryhttp.Authentication
	Config         websocket.Config
	Guard          auth.Guard[Account]
	Notifications  *notifications.Registry
	Tables         *datatable.Registry
	signer         foundryhttp.URLSigner
}

// NewFixture binds all exported descriptors to real handlers. The sample
// credential and account exist only in this independent test fixture.
func NewFixture(ctx context.Context, root *os.Root, temporary string) (Fixture, error) {
	var result Fixture
	provider := auth.DefineProvider("client_accounts", (Account{}).FoundryReference(), func(_ context.Context, id int64) (value.Optional[Account], error) {
		return value.Set(Account{ID: id, Display: "Visible member", Private: "must-not-be-exported"}), nil
	}, func(context.Context, Account) (bool, error) { return true, nil })
	proof, err := auth.NewProof(Account{ID: 7}.FoundryReference(), auth.Authenticated)
	if err != nil {
		return result, err
	}
	strategy := auth.DefineStrategy("client_cookie", func(_ context.Context, input secret.String) (value.Optional[auth.Proof[Account, int64]], error) {
		if input.Reveal() != "fixture-only" {
			return value.Optional[auth.Proof[Account, int64]]{}, nil
		}
		return value.Set(proof), nil
	})
	guard := auth.DefineGuard("client_accounts", provider, strategy)
	recipient := notifications.DefineRecipient("client_accounts", provider, guard, func(context.Context, Account, notifications.Name, notifications.ChannelID) (bool, error) {
		return true, nil
	})
	inbox := notifications.Database("inbox", MemberJSON(), func(_ context.Context, account Account, _ notifications.DeliveryContext, _ Payload) (Member, error) {
		return Member{Display: account.Display}, nil
	})
	notices, err := notifications.NewRegistry(notifications.Bind(notifications.Define("client.notice", 1, PayloadJSON()), recipient, inbox.Channel()).Registration())
	if err != nil {
		return result, err
	}
	tables, err := datatable.NewRegistry(reporting.Members.Registration())
	if err != nil {
		return result, err
	}
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration(), Manage.Registration())
	if err != nil {
		return result, err
	}
	cookie := foundryhttp.DefineCookie("fixture_session", foundryhttp.SecretCookie(), foundryhttp.DefaultCookieOptions())
	transport, err := foundryhttp.NewAuthentication(registry, foundryhttp.CookieCredential("client_cookie", cookie).WithoutOriginProtection())
	if err != nil {
		return result, err
	}
	pageBinding, err := foundryhttp.BindGuard(transport, guard)
	if err != nil {
		return result, err
	}
	private := websocket.OwnedRooms[PrivateOwner]("accounts", websocket.DefineRooms(foundryhttp.IntegerPath[int64]()), guard, (Account{}).FoundryReference())
	presence := websocket.WithPresence(private, MemberJSON(), func(_ context.Context, account Account) (Member, error) { return Member{Display: account.Display}, nil })
	channels, err := websocket.NewRegistry(websocket.Register(Updates, Updated.Registration(), Relay.Authorize(func(context.Context, websocket.MessageContext[int64, websocket.Anonymous], Payload) error { return nil }).Relay(Updated)), websocket.Register(presence.Channel()))
	if err != nil {
		return result, err
	}
	secure := foundryhttp.DefineEndpoint(AccountRoute, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, MemberJSON()))
	broken := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "failure", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/failure")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, contract.StringJSON[string]())).WithErrors(Changed)
	form := httpuploads.ProfileInputDescriptor().WithTempDirectory(temporary)
	upload := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "uploads.profile", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/upload")), foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(form), foundryhttp.JSONResponse(201, httpuploads.UploadReplyJSON()))
	download := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "download", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/download")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.DownloadResponse("text/plain"))
	empty := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "empty", Method: foundryhttp.DELETE, Access: foundryhttp.Public}, foundryhttp.StaticPath("/empty")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	pluralRoutes, err := pluralValidationRoutes()
	if err != nil {
		return result, err
	}
	signer, err := signingFixture()
	if err != nil {
		return result, err
	}
	router, err := foundryhttp.NewRouter(append(append(pluralRoutes, transportRoutes(signer)...),
		requestflow.Submit.Handle(requestflow.Handle),
		unions.Echo.Handle(unions.Handle),
		genericdto.Echo.Handle(genericdto.HandleEcho),
		genericdto.ShowProject.Handle(genericdto.HandleProject),
		Echo.Handle(func(_ context.Context, input foundryhttp.Input[ItemPath, Filters, Payload]) (Payload, error) {
			if input.Path.Key != input.Body.Large {
				return Payload{}, foundryhttp.BadRequest
			}
			return input.Body, nil
		}),
		foundryhttp.RequireAuthentication(secure, transport, guard).WithPermissions(Manage).Handle(func(_ context.Context, subject Account, _ foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (Member, error) {
			return Member{Display: subject.Display}, nil
		}),
		broken.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (string, error) {
			return "", Changed
		}),
		upload.Handle(func(ctx context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, httpuploads.ProfileInput]) (httpuploads.UploadReply, error) {
			size, err := httpuploads.ReadContents(ctx, input.Body.Attachment)
			return httpuploads.UploadReply{Name: "fixture.txt", Bytes: size, DetectedType: "text/plain"}, err
		}),
		download.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Download, error) {
			return foundryhttp.LocalDownload(root, "sample.txt").WithMediaType("text/plain"), nil
		}),
		empty.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		pagination.Authenticated(httppagination.GuardedList, pageBinding).WithPermissions(Manage).Handle(func(_ context.Context, actor Account, input httppagination.ListRequest) (query.Page[httppagination.MemberResponse], error) {
			return query.Page[httppagination.MemberResponse]{Items: []httppagination.MemberResponse{{Email: mutatorqueries.DisplayEmail(actor.Display), ID: modelIDForPage()}}, Number: input.Page.Number, Size: input.Page.Size, Total: 1, Pages: 1}, nil
		}),
		pagination.Authenticated(httppagination.GuardedSimpleList, pageBinding).WithPermissions(Manage).Handle(func(_ context.Context, actor Account, input httppagination.ListRequest) (query.SimplePage[httppagination.MemberResponse], error) {
			return query.SimplePage[httppagination.MemberResponse]{Items: []httppagination.MemberResponse{{Email: mutatorqueries.DisplayEmail(actor.Display), ID: modelIDForPage()}}, Number: input.Page.Number, Size: input.Page.Size}, nil
		}),
		pagination.Authenticated(httppagination.GuardedCursorList, pageBinding).WithPermissions(Manage).Handle(func(ctx context.Context, actor Account, input httppagination.CursorListRequest) (httppagination.CursorListResult, error) {
			return pagination.MapCursorPage(ctx, query.CursorPage[mutatorqueries.Member]{Size: input.Page.Size}, httppagination.PresentMember)
		}),
		httppagination.List.Handle(func(_ context.Context, input httppagination.ListRequest) (query.Page[httppagination.MemberResponse], error) {
			return query.Page[httppagination.MemberResponse]{Items: []httppagination.MemberResponse{}, Number: input.Page.Number, Size: input.Page.Size, Total: 0, Pages: 0}, nil
		}),
	)...)
	if err != nil {
		return result, err
	}
	config := websocket.DefaultConfig()
	config.AllowOriginless = true
	return Fixture{Router: router, Registry: channels, Authentication: transport, Config: config, Guard: guard, Notifications: notices, Tables: tables, signer: signer}, nil
}

func (f Fixture) Manifest(ctx context.Context) (*manifest.Manifest, error) {
	realtime, err := websocket.DescribeClient(f.Registry, f.Config)
	if err != nil {
		return nil, err
	}
	catalog, err := localization.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	enumeration, err := localization.Draft.EnumDescriptor().Definition()
	if err != nil {
		return nil, err
	}
	permission, err := Manage.Description()
	if err != nil {
		return nil, err
	}
	dynamic, err := contract.DynamicJSON().Description()
	if err != nil {
		return nil, err
	}
	labels, err := ExplicitLabelsJSON()
	if err != nil {
		return nil, err
	}
	labelSchema, err := labels.Description()
	if err != nil {
		return nil, err
	}
	ownerIndex, err := OwnerIndexJSON().Description()
	if err != nil {
		return nil, err
	}
	return manifest.Build(ctx, manifest.Sources{HTTP: f.Router, Realtime: &realtime, Notifications: f.Notifications, Tables: f.Tables, Catalog: catalog, Enums: []enum.Definition{enumeration}, Permissions: []auth.PermissionDescription{permission}, Schemas: []contract.Schema{dynamic, labelSchema, ownerIndex}})
}

func Export(ctx context.Context, source *manifest.Manifest, options typescript.Options) (typescript.Report, error) {
	return typescript.Generate(ctx, source, options)
}

// modelIDForPage is a fixed, non-secret response identity for this transport fixture.
func modelIDForPage() model.ID[mutatorqueries.Member] {
	id, err := model.ParseID[mutatorqueries.Member]("0193fd8c-2075-7000-8000-000000000001")
	if err != nil {
		panic(err)
	}
	return id
}

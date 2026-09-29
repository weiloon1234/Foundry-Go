package clientcontracts

import (
	"context"
	"io"
	"mime"
	"strconv"
	"time"

	"foundry.test/consumer/httpuploads"
	"github.com/weiloon1234/Foundry-Go/clock"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// AccountRoute is the guarded account page; Continue redirects to it by name.
var AccountRoute = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "account.show", Method: foundryhttp.GET, Access: foundryhttp.Guarded}, foundryhttp.StaticPath("/account"))

// Upsert declares two success statuses; the handler selects one of them.
var Upsert = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "members.upsert", Method: foundryhttp.PUT, Access: foundryhttp.Public}, foundryhttp.StaticPath("/members/upsert")),
	foundryhttp.EmptyQuery(), foundryhttp.JSONBody(MemberJSON()), foundryhttp.JSONResponses(MemberJSON(), 200, 201),
)

// Continue answers a form submission with a redirect to a named route.
var Continue = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "session.continue", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/continue")),
	foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.RedirectResponse(303),
)

// RawUpload streams a raw body of a declared media type, bounded to 64 KiB.
var RawUpload = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "files.raw", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/raw")),
	foundryhttp.EmptyQuery(), foundryhttp.RawRequestBody("application/octet-stream", "text/plain"), foundryhttp.JSONResponse(201, httpuploads.UploadReplyJSON()),
).WithLimits(rawLimits())

// MemberEvents streams typed server-sent events and resumes after Last-Event-ID.
var MemberEvents = foundryhttp.DefineEndpoint(
	foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "members.events", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/members/events")),
	foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EventStreamResponse(MemberJSON()),
).WithTimeout(time.Minute)

// signedMember accepts expiring, permanent and origin-relative signed links; a
// mail client's utm_source decoration is ignored.
func signedMember(signer foundryhttp.URLSigner) foundryhttp.SignedEndpoint[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody, Member] {
	return foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "members.signed", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/members/signed")),
		foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.JSONResponse(200, MemberJSON()),
	).Signed(signer).WithPermanentLinks().WithIgnoredParameters("utm_source")
}

// SignedLinks are the link forms a client receives from the server.
type SignedLinks struct {
	Absolute  string `json:"absolute"`
	Relative  string `json:"relative"`
	Permanent string `json:"permanent"`
	Decorated string `json:"decorated"`
}

// Links signs each link form for a server origin.
func (f Fixture) Links(ctx context.Context, origin foundryhttp.Origin) (SignedLinks, error) {
	endpoint := signedMember(f.signer)
	none, noQuery := foundryhttp.NoPath{}, foundryhttp.NoQuery{}
	expires := time.Now().Add(time.Hour)
	absolute, err := endpoint.URL(ctx, origin, none, noQuery, expires)
	if err != nil {
		return SignedLinks{}, err
	}
	relative, err := endpoint.RelativeURL(ctx, none, noQuery, expires)
	if err != nil {
		return SignedLinks{}, err
	}
	permanent, err := endpoint.PermanentURL(ctx, origin, none, noQuery)
	if err != nil {
		return SignedLinks{}, err
	}
	return SignedLinks{Absolute: absolute, Relative: relative, Permanent: permanent, Decorated: absolute + "&utm_source=mail"}, nil
}

func signingFixture() (foundryhttp.URLSigner, error) {
	// A fixed, non-secret fixture key for this loopback test only.
	keys, err := foundryhttp.NewSigningKeys(foundryhttp.SigningKey{ID: "fixture", Secret: secret.New("fixture-signing-key-material-0001")})
	if err != nil {
		return foundryhttp.URLSigner{}, err
	}
	return foundryhttp.NewURLSigner(keys, clock.System{})
}

func rawLimits() foundryhttp.EndpointLimits {
	limits := foundryhttp.DefaultEndpointLimits()
	limits.Raw.Bytes = 64 << 10
	return limits
}

func transportRoutes(signer foundryhttp.URLSigner) []foundryhttp.RouteRegistration {
	return []foundryhttp.RouteRegistration{
		signedMember(signer).Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (Member, error) {
			return Member{Display: "signed member"}, nil
		}),
		Upsert.Handle(func(_ context.Context, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, Member]) (foundryhttp.Statused[Member], error) {
			if in.Body.Display == "new" {
				return foundryhttp.Statused[Member]{Status: 201, Value: in.Body}, nil
			}
			return foundryhttp.Statused[Member]{Value: in.Body}, nil
		}),
		Continue.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Redirect, error) {
			return foundryhttp.RedirectToRoute(AccountRoute, foundryhttp.NoPath{}), nil
		}),
		MemberEvents.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Events[Member], error) {
			return foundryhttp.EventsFrom(func(ctx context.Context, sink *foundryhttp.EventSink[Member]) error {
				start := 1
				if last, ok := foundryhttp.LastEventID(ctx); ok {
					resumed, err := strconv.Atoi(last)
					if err != nil {
						return foundryhttp.BadRequest
					}
					start = resumed + 1
				}
				for index := start; index <= 3; index++ {
					event := foundryhttp.Event[Member]{ID: strconv.Itoa(index), Name: "member", Data: Member{Display: "member " + strconv.Itoa(index)}}
					if index == 3 {
						event.Retry = 1500 * time.Millisecond
					}
					if err := sink.Send(ctx, event); err != nil {
						return err
					}
				}
				return nil
			}), nil
		}),
		RawUpload.Handle(func(_ context.Context, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.RawBody]) (httpuploads.UploadReply, error) {
			size, err := io.Copy(io.Discard, in.Body)
			if err != nil {
				return httpuploads.UploadReply{}, err
			}
			media, _, err := mime.ParseMediaType(string(in.Body.MediaType()))
			return httpuploads.UploadReply{Name: "raw", Bytes: size, DetectedType: media}, err
		}),
	}
}

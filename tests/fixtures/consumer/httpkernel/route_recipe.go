package httpkernel

import (
	"foundry.test/consumer/mutatorqueries"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Member reuses the fixture model identity.
type Member = mutatorqueries.Member

// doc-example:begin member-url-path
// MemberPath binds the URL segment to the concrete member key.
type MemberPath struct {
	Member model.ID[Member]
}

// doc-example:end member-url-path

func memberURL(member Member) (string, error) {
	// doc-example:begin http-routing-1
	MemberShow := foundryhttp.DefineRoute(
		foundryhttp.RouteSpec{
			ID:     "members.show",
			Method: foundryhttp.GET,
			Access: foundryhttp.Public,
		},
		foundryhttp.DefinePath("/members/{member}",
			foundryhttp.Param("member", foundryhttp.ModelIDPath[Member](),
				func(path *MemberPath) *model.ID[Member] { return &path.Member }),
		),
	)
	MemberShow = foundryhttp.ConfigureRoute(MemberShow, foundryhttp.Within(foundryhttp.DefineScope("/api", "api")))

	// The URL is /api/members/<member UUID>.
	return foundryhttp.RouteURL(MemberShow, MemberPath{Member: member.ID})
	// doc-example:end http-routing-1
}

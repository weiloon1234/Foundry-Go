// Frozen method shapes for appendix name coverage. Arguments are simplified.
package http

type Endpoint[T0, T1, T2, T3 any] struct{}

func (e Endpoint[T0, T1, T2, T3]) ID() int                        { return 0 }
func (e Endpoint[T0, T1, T2, T3]) Method() int                    { return 0 }
func (e Endpoint[T0, T1, T2, T3]) Pattern() int                   { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithLimits(int) int             { return 0 }
func (e Endpoint[T0, T1, T2, T3]) Within(int) int                 { return 0 }
func (e Endpoint[T0, T1, T2, T3]) Validate() int                  { return 0 }
func (e Endpoint[T0, T1, T2, T3]) HandleBound(int) int            { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithPreparation(int) int        { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithAuthorization(int) int      { return 0 }
func (e Endpoint[T0, T1, T2, T3]) Description() int               { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithValidation(...int) int      { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithPathValidation(...int) int  { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithQueryValidation(...int) int { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithBodyValidation(...int) int  { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithErrors(...int) int          { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithHeaders(int) int            { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithTimeout(int) int            { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithBodyLimit(int) int          { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithDocumentation(int) int      { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithBodyExample(int) int        { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithResponseExample(int) int    { return 0 }
func (e Endpoint[T0, T1, T2, T3]) WithMiddleware(...int) int      { return 0 }

type AuthenticatedEndpoint[T0, T1, T2, T3, T4 any] struct{}

func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4]) WithActorMiddleware(...int) int { return 0 }
func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4]) WithScopes(int) int             { return 0 }
func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4]) WithPermissions(...int) int     { return 0 }
func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4]) Validate() int                  { return 0 }
func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4]) Description() int               { return 0 }
func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4]) HandleBound(int) int            { return 0 }
func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4]) WithPreparation(int) int        { return 0 }
func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4]) WithAuthorization(int) int      { return 0 }

type OptionalAuthenticationEndpoint[T0, T1, T2, T3, T4 any] struct{}

func (e OptionalAuthenticationEndpoint[T0, T1, T2, T3, T4]) WithActorMiddleware(...int) int { return 0 }
func (e OptionalAuthenticationEndpoint[T0, T1, T2, T3, T4]) Validate() int                  { return 0 }
func (e OptionalAuthenticationEndpoint[T0, T1, T2, T3, T4]) Description() int               { return 0 }
func (e OptionalAuthenticationEndpoint[T0, T1, T2, T3, T4]) HandleBound(int) int            { return 0 }
func (e OptionalAuthenticationEndpoint[T0, T1, T2, T3, T4]) WithPreparation(int) int        { return 0 }
func (e OptionalAuthenticationEndpoint[T0, T1, T2, T3, T4]) WithAuthorization(int) int      { return 0 }

type SignedEndpoint[T0, T1, T2, T3 any] struct{}

func (e SignedEndpoint[T0, T1, T2, T3]) ID() int                             { return 0 }
func (e SignedEndpoint[T0, T1, T2, T3]) Method() int                         { return 0 }
func (e SignedEndpoint[T0, T1, T2, T3]) Pattern() int                        { return 0 }
func (e SignedEndpoint[T0, T1, T2, T3]) Validate() int                       { return 0 }
func (e SignedEndpoint[T0, T1, T2, T3]) WithPermanentLinks() int             { return 0 }
func (e SignedEndpoint[T0, T1, T2, T3]) WithIgnoredParameters(...int) int    { return 0 }
func (e SignedEndpoint[T0, T1, T2, T3]) PermanentURL(int, int, int, int) int { return 0 }
func (e SignedEndpoint[T0, T1, T2, T3]) RelativeURL(int, int, int, int) int  { return 0 }
func (e SignedEndpoint[T0, T1, T2, T3]) HandleBound(int) int                 { return 0 }
func (e SignedEndpoint[T0, T1, T2, T3]) Description() int                    { return 0 }

type SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4 any] struct{}

func (e SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4]) ID() int                          { return 0 }
func (e SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4]) Method() int                      { return 0 }
func (e SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4]) Pattern() int                     { return 0 }
func (e SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4]) Validate() int                    { return 0 }
func (e SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4]) WithPermanentLinks() int          { return 0 }
func (e SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4]) WithIgnoredParameters(...int) int { return 0 }
func (e SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4]) PermanentURL(int, int, int, int) int {
	return 0
}
func (e SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4]) RelativeURL(int, int, int, int) int {
	return 0
}
func (e SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4]) Description() int    { return 0 }
func (e SignedAuthenticatedEndpoint[T0, T1, T2, T3, T4]) HandleBound(int) int { return 0 }

type IdempotentEndpoint[T0, T1, T2, T3 any] struct{}

func (e IdempotentEndpoint[T0, T1, T2, T3]) ID() int             { return 0 }
func (e IdempotentEndpoint[T0, T1, T2, T3]) WithHeaders(int) int { return 0 }
func (e IdempotentEndpoint[T0, T1, T2, T3]) Validate() int       { return 0 }
func (e IdempotentEndpoint[T0, T1, T2, T3]) Description() int    { return 0 }
func (e IdempotentEndpoint[T0, T1, T2, T3]) Prepare(int) int     { return 0 }

type AuthenticatedIdempotentEndpoint[T0, T1, T2, T3, T4 any] struct{}

func (e AuthenticatedIdempotentEndpoint[T0, T1, T2, T3, T4]) Validate() int       { return 0 }
func (e AuthenticatedIdempotentEndpoint[T0, T1, T2, T3, T4]) Description() int    { return 0 }
func (e AuthenticatedIdempotentEndpoint[T0, T1, T2, T3, T4]) WithHeaders(int) int { return 0 }
func (e AuthenticatedIdempotentEndpoint[T0, T1, T2, T3, T4]) Prepare(int) int     { return 0 }

type Route[T0 any] struct{}

func (e Route[T0]) Parameters() int           { return 0 }
func (e Route[T0]) ID() int                   { return 0 }
func (e Route[T0]) Method() int               { return 0 }
func (e Route[T0]) Pattern() int              { return 0 }
func (e Route[T0]) Validate() int             { return 0 }
func (e Route[T0]) URL(int) int               { return 0 }
func (e Route[T0]) HandleRaw(int) int         { return 0 }
func (e Route[T0]) WithTimeout(int) int       { return 0 }
func (e Route[T0]) WithBodyLimit(int) int     { return 0 }
func (e Route[T0]) WithDocumentation(int) int { return 0 }
func (e Route[T0]) WithMiddleware(...int) int { return 0 }
func (e Route[T0]) Within(int) int            { return 0 }
func (e Route[T0]) Signed(int) int            { return 0 }

type AuthenticatedRoute[T0, T1 any] struct{}

func (e AuthenticatedRoute[T0, T1]) WithActorMiddleware(...int) int { return 0 }
func (e AuthenticatedRoute[T0, T1]) WithScopes(int) int             { return 0 }
func (e AuthenticatedRoute[T0, T1]) WithPermissions(...int) int     { return 0 }
func (e AuthenticatedRoute[T0, T1]) Validate() int                  { return 0 }
func (e AuthenticatedRoute[T0, T1]) Description() int               { return 0 }
func (e AuthenticatedRoute[T0, T1]) URL(int) int                    { return 0 }
func (e AuthenticatedRoute[T0, T1]) HandleRaw(int) int              { return 0 }
func (e AuthenticatedRoute[T0, T1]) Signed(int) int                 { return 0 }

type OptionalAuthenticationRoute[T0, T1 any] struct{}

func (e OptionalAuthenticationRoute[T0, T1]) WithActorMiddleware(...int) int { return 0 }
func (e OptionalAuthenticationRoute[T0, T1]) Validate() int                  { return 0 }
func (e OptionalAuthenticationRoute[T0, T1]) Description() int               { return 0 }
func (e OptionalAuthenticationRoute[T0, T1]) URL(int) int                    { return 0 }
func (e OptionalAuthenticationRoute[T0, T1]) HandleRaw(int) int              { return 0 }
func (e OptionalAuthenticationRoute[T0, T1]) Signed(int) int                 { return 0 }

type SignedRoute[T0 any] struct{}

func (e SignedRoute[T0]) ID() int                          { return 0 }
func (e SignedRoute[T0]) Method() int                      { return 0 }
func (e SignedRoute[T0]) Pattern() int                     { return 0 }
func (e SignedRoute[T0]) Validate() int                    { return 0 }
func (e SignedRoute[T0]) URL(int, int, int, int) int       { return 0 }
func (e SignedRoute[T0]) WithPermanentLinks() int          { return 0 }
func (e SignedRoute[T0]) WithIgnoredParameters(...int) int { return 0 }
func (e SignedRoute[T0]) PermanentURL(int, int, int) int   { return 0 }
func (e SignedRoute[T0]) RelativeURL(int, int, int) int    { return 0 }
func (e SignedRoute[T0]) HandleRaw(int) int                { return 0 }

type SignedAuthenticatedRoute[T0, T1 any] struct{}

func (e SignedAuthenticatedRoute[T0, T1]) Validate() int                    { return 0 }
func (e SignedAuthenticatedRoute[T0, T1]) URL(int, int, int, int) int       { return 0 }
func (e SignedAuthenticatedRoute[T0, T1]) WithPermanentLinks() int          { return 0 }
func (e SignedAuthenticatedRoute[T0, T1]) WithIgnoredParameters(...int) int { return 0 }
func (e SignedAuthenticatedRoute[T0, T1]) PermanentURL(int, int, int) int   { return 0 }
func (e SignedAuthenticatedRoute[T0, T1]) RelativeURL(int, int, int) int    { return 0 }
func (e SignedAuthenticatedRoute[T0, T1]) Description() int                 { return 0 }
func (e SignedAuthenticatedRoute[T0, T1]) HandleRaw(int) int                { return 0 }

type Path[T0 any] struct{}

func (e Path[T0]) Pattern() int    { return 0 }
func (e Path[T0]) URL(int) int     { return 0 }
func (e Path[T0]) Parameters() int { return 0 }

type Query[T0 any] struct{}

func (e Query[T0]) Validate() int            { return 0 }
func (e Query[T0]) Parameters() int          { return 0 }
func (e Query[T0]) Decode(int, int, int) int { return 0 }
func (e Query[T0]) Encode(int, int, int) int { return 0 }

type Body[T0 any] struct{}

func (e Body[T0]) Validate() int { return 0 }

type Response[T0 any] struct{}

func (e Response[T0]) Validate() int { return 0 }

type Multipart[T0 any] struct{}

func (e Multipart[T0]) WithTempDirectory(int) int { return 0 }
func (e Multipart[T0]) Validate() int             { return 0 }
func (e Multipart[T0]) Description() int          { return 0 }

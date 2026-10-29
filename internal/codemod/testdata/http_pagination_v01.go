// Frozen method shapes for appendix name coverage. Arguments are simplified.
package pagination

type pageEndpoint[T0, T1, T2, T3, T4 any] struct{}
type PageEndpoint[T0, T1, T2, T3, T4 any] = pageEndpoint[T0, T1, T2, T3, T4]

func (e pageEndpoint[T0, T1, T2, T3, T4]) Validate() int                    { return 0 }
func (e pageEndpoint[T0, T1, T2, T3, T4]) ID() int                          { return 0 }
func (e pageEndpoint[T0, T1, T2, T3, T4]) Method() int                      { return 0 }
func (e pageEndpoint[T0, T1, T2, T3, T4]) Pattern() int                     { return 0 }
func (e pageEndpoint[T0, T1, T2, T3, T4]) Description() int                 { return 0 }
func (e pageEndpoint[T0, T1, T2, T3, T4]) Within(int) int                   { return 0 }
func (e pageEndpoint[T0, T1, T2, T3, T4]) WithMiddleware(...int) int        { return 0 }
func (e pageEndpoint[T0, T1, T2, T3, T4]) WithLimits(int) int               { return 0 }
func (e pageEndpoint[T0, T1, T2, T3, T4]) WithErrors(...int) int            { return 0 }
func (e pageEndpoint[T0, T1, T2, T3, T4]) WithPathValidation(...int) int    { return 0 }
func (e pageEndpoint[T0, T1, T2, T3, T4]) WithFiltersValidation(...int) int { return 0 }

type authenticatedPageEndpoint[T0, T1, T2, T3, T4, T5 any] struct{}
type AuthenticatedPageEndpoint[T0, T1, T2, T3, T4, T5 any] = authenticatedPageEndpoint[T0, T1, T2, T3, T4, T5]

func (e authenticatedPageEndpoint[T0, T1, T2, T3, T4, T5]) WithScopes(int) int         { return 0 }
func (e authenticatedPageEndpoint[T0, T1, T2, T3, T4, T5]) WithPermissions(...int) int { return 0 }
func (e authenticatedPageEndpoint[T0, T1, T2, T3, T4, T5]) WithAuthorization(int) int  { return 0 }
func (e authenticatedPageEndpoint[T0, T1, T2, T3, T4, T5]) Validate() int              { return 0 }
func (e authenticatedPageEndpoint[T0, T1, T2, T3, T4, T5]) ID() int                    { return 0 }
func (e authenticatedPageEndpoint[T0, T1, T2, T3, T4, T5]) Method() int                { return 0 }
func (e authenticatedPageEndpoint[T0, T1, T2, T3, T4, T5]) Pattern() int               { return 0 }
func (e authenticatedPageEndpoint[T0, T1, T2, T3, T4, T5]) Description() int           { return 0 }

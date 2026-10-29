// Frozen method shapes for appendix name coverage. Arguments are simplified.
package modelbinding

type Endpoint[T0, T1, T2, T3, T4 any] struct{}

func (e Endpoint[T0, T1, T2, T3, T4]) WithAuthorization(int) int { return 0 }
func (e Endpoint[T0, T1, T2, T3, T4]) Validate() int             { return 0 }
func (e Endpoint[T0, T1, T2, T3, T4]) Description() int          { return 0 }

type AuthenticatedEndpoint[T0, T1, T2, T3, T4, T5 any] struct{}

func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4, T5]) Validate() int             { return 0 }
func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4, T5]) Description() int          { return 0 }
func (e AuthenticatedEndpoint[T0, T1, T2, T3, T4, T5]) WithAuthorization(int) int { return 0 }

type Resolver[T0, T1 any] struct{}

func (e Resolver[T0, T1]) WithMissing(int) int  { return 0 }
func (e Resolver[T0, T1]) Validate() int        { return 0 }
func (e Resolver[T0, T1]) Resolve(int, int) int { return 0 }

type IdempotentEndpoint[T0, T1, T2, T3, T4 any] struct{}

func (e IdempotentEndpoint[T0, T1, T2, T3, T4]) Validate() int       { return 0 }
func (e IdempotentEndpoint[T0, T1, T2, T3, T4]) Description() int    { return 0 }
func (e IdempotentEndpoint[T0, T1, T2, T3, T4]) WithHeaders(int) int { return 0 }

type AuthenticatedIdempotentEndpoint[T0, T1, T2, T3, T4, T5 any] struct{}

func (e AuthenticatedIdempotentEndpoint[T0, T1, T2, T3, T4, T5]) Validate() int       { return 0 }
func (e AuthenticatedIdempotentEndpoint[T0, T1, T2, T3, T4, T5]) Description() int    { return 0 }
func (e AuthenticatedIdempotentEndpoint[T0, T1, T2, T3, T4, T5]) WithHeaders(int) int { return 0 }

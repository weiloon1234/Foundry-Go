package compilefail

import "github.com/weiloon1234/Foundry-Go/tracing"

var _ tracing.TraceID = tracing.SpanID{}

# Shared source identities for transport contracts

**Status: focused runtime, generation, consumer, compiler and editor acceptance passed.**

Generated path, query, JSON value and map-key metadata use the same source
identity for a Go type. This matters in an executable `main` package: runtime
reflection reports `main.Code`, while generation knows the actual module path.
Model IDs whose type argument belongs to `main` retain that argument's source
namespace as well.

Declare ordinary types and let generation supply the metadata:

```go
type Priority uint8

//foundry:path pattern=/priorities/{priority}
type PriorityPath struct { Priority Priority }

//foundry:query
type PriorityQuery struct { Priority Priority }

//foundry:dto
type PriorityResponse struct { Priority Priority }
```

All three descriptors expose the same qualified Priority identity. Integer
width, enum membership, model owner, syntax and native parsing remain attached
to the original typed codec. The
[independent consumer](../../tests/fixtures/consumer/httpquery/source_identity.go)
checks declaration agreement and rejects values outside uint8.

`http.URLType[V](sourceID, codec)` is the generated declaration boundary. It
keeps V and copies existing scalar metadata under the supplied source identity.
It does not guess an opaque custom codec's representation. Custom codecs need
an explicit same-value `DescribeURL` contract first; generation leaves
undescribed text codecs explicit. Applications normally consume generated
descriptors rather than call URLType directly.

The source identifier is supplied by the generator. A custom descriptor author
owns agreement between that identifier and the actual Go source type. Returned
metadata is an independent snapshot; assigning a new source identity does not
mutate the original codec or execute its Parse/Format methods.

Combined transport full regression passed.

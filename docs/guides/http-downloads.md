# Typed file downloads

A download endpoint returns `http.Download`. Its handler does not receive a
response writer. Foundry opens the representation after successful input and
handler execution, validates its declared media, handles conditions/ranges and
closes every transferred reader after the response finishes.

The [minimal consumer example](../../tests/fixtures/consumer/httpdownloads/example.go)
registers a PDF response with `DownloadResponse("application/pdf")` and returns:

```go
return foundryhttp.LocalDownload(root, "reports/monthly.pdf").
    WithName("report.pdf").
    WithMediaType("application/pdf"), nil
```

The application owns `*os.Root` and closes it during application shutdown. Foundry
owns the file opened for each request. Relative paths use `fs.ValidPath` and
`os.Root` confines resolution, including symlinks. Display names never select
paths. Local sources accept regular files, check the opened descriptor and use
nonblocking opens on Unix to avoid blocking on a replaced FIFO. A missing local
file returns the shared `NotFound` error; other unexpected I/O failures remain
internal errors.

## Typed domain lookups

The [document consumer](../../tests/fixtures/consumer/httpdownloads/downloads.go)
reuses a generated model-ID path descriptor and passes `model.ID[models.User]`
into a domain service. The service selects the stored object and provides its
display name, `MediaType` and `EntityTag`. Foundry handles the transfer. The route
can declare several concrete media types; a source must match one. Model values
remain distinct from transport DTOs and file responses.

`WithName` changes only presentation. `WithMediaType`, `WithDisposition` and
`WithEntityTag` retain semantic Go types; attachment is the default disposition.
Names are normalized through the same bounded display-name rules as uploads.
Content-Disposition includes a safe ASCII fallback and UTF-8 filename.

## Custom sources and ownership

`DownloadFrom` accepts a `func(context.Context) (DownloadContent, error)`.
`DownloadContent.Body` requires `io.ReadSeekCloser`; unseekable streams are a
separate transport capability. The callback is deferred, including when the
handler returns an error. Returning `Download` never opens or buffers its source
at route construction time.

A source owns resources until its callback returns. It must release anything it
opened if it panics/exits before returning. On return, ownership of a non-nil
body transfers to Foundry even alongside an error. Reads and seeks must terminate
and honor the supplied context. Foundry waits for actual callbacks to finish;
it does not abandon an uncooperative callback or claim that it has been cleaned
up. Cleanup runs on preparation, validation, transfer and cancellation failures.
Reader/seek/close panic and Goexit are contained, including native range workers.

The source owns validator correctness. Supply entity tags from the content's
actual revision, and use stable representations during a transfer. Foundry does
not invent tags from filenames or promise a snapshot of a mutable source.

## HTTP behavior and limits

Native `net/http.ServeContent` owns HEAD, conditional requests, single/suffix and
multiple byte ranges. Successful responses can be 200, 206 or 304. Preconditions
that fail return the shared `PreconditionFailed` JSON error; unsatisfiable or
malformed native ranges return `RangeNotSatisfiable`. A computed 416 Content-Range
is preserved. Internal source details never enter the public response.

`EndpointLimits.Files` bounds representation bytes, number of requested ranges
and Range header bytes independently of buffered JSON response limits. Defaults
are 1 GiB, 16 ranges and 4096 header bytes. Range-budget violations and duplicate
Range headers fail before the domain handler. Native grammar and satisfiability
remain authoritative within those budgets. Byte limits constrain the selected
representation; native multipart framing adds bounded transport overhead.
Every native size seek is checked, including a file that grows after preparation.

Successful transfers use fixed copy buffers and explicit media with `nosniff`.
No complete file buffer is required. Callback isolation adds allocation per
chunk; benchmark results must distinguish total allocation from peak live memory.
A short read/write or a failure after headers aborts the transfer. Foundry never
replaces an already started file with a JSON error. Failure of the native writer
itself also aborts, because the writer may no longer be usable for an error reply.

Endpoint metadata includes owned `FileResponseInfo` with declared media and
seekability. It has no invented JSON schema. Files and deferred downloads reject
implicit JSON serialization. OpenAPI and client generation consume the same
registered metadata in milestone 21.

## Scope

Runtime, independent consumer, compiler, editor, generation and documentation
checks passed before publication. Canonical acceptance and the complete combined
regression with unseekable streams also passed. Storage-provider adapters are
milestone 11; [static/SPA integration](http-assets.md) is delivered. Typed
unseekable streams are documented in [stream responses](http-streams.md). The documented example is compiled from the independent
consumer module as part of acceptance.

# Public API inventory — 2026-10-31

Baseline before the remaining D2 migration. The source scan covers all importable
library packages in this module, including generated and platform-specific Go
files, public test helpers, and exported interface methods. It excludes tests,
nested modules, `internal`, `testdata`, vendored code, and `main` packages.

Total: **10,002 declarations in 160 packages**; **483 exported methods on
private receivers**; **3,866 declarations without documentation**;
**518 DSL functions/types without `Example:`**.

## Exported methods on private receivers

| Package | Methods | Receivers and methods |
| --- | ---: | --- |
| `application` | 1 | `schemaTransactor`: `Transaction` |
| `attachments` | 29 | `manySlot`: `Add`, `AddFile`, `AddIn`, `Format`, `From`, `PublicURLs`, `Reorder`; `oneSlot`: `Format`, `From`, `PublicURL`; `slotDefinition`: `Accepts`, `Clear`, `Collection`, `Describe`, `Detach`, `Discard`, `Name`, `Prepare`, `PrepareFile`, `Registration`, `Replace`, `ReplaceFile`, `ReplaceIn`, `TemporaryURL`, `URL`, `Validate`, `VariantURL`; `verifiedReader`: `Close`, `Read` |
| `audit` | 6 | `metadata`: `Area`, `Correlation`, `CreatedAt`, `Origin`, `Route`, `Sequence` |
| `auth` | 3 | `failure`: `Error`, `Is`, `Unwrap` |
| `auth/lockout` | 3 | `unavailable`: `Error`, `Is`, `Unwrap` |
| `auth/oauth` | 3 | `failureCode`: `Error`; `pendingCodec`: `Format`, `Parse` |
| `cache` | 15 | `snapshotAccess`: `Add`, `Exists`, `Expire`, `Forget`, `Get`, `Increment`, `Put`; `taggedAccess`: `Add`, `Exists`, `Expire`, `Forget`, `Get`, `Increment`, `Put`, `PutLeased` |
| `cache/memory` | 5 | `expiryHeap`: `Len`, `Less`, `Pop`, `Push`, `Swap` |
| `cli` | 1 | `helpWriter`: `Write` |
| `database` | 2 | `collectedRow`: `FieldEncryption`, `Scan` |
| `database/orm` | 5 | `auditKeyCodec`: `Bind`, `Decode`; `eachTransaction`: `Transaction`; `fixedNow`: `Now`; `readExecutor`: `Query` |
| `database/orm/expr` | 113 | `aggregate`: `Asc`, `Desc`, `Eq`, `Filter`, `In`, `Ne`, `Param`, `Value`; `aggregateScanner`: `Scan`; `havingPredicate`: `Not`; `nullableOrderedAggregate`: `Asc`, `Desc`, `Eq`, `Filter`, `Gt`, `Gte`, `In`, `IsNotNull`, `IsNull`, `Lt`, `Lte`, `Ne`, `Param`, `Value`; `nullableOrderedRowExpression`: `Param`, `Value`; `nullableOrderedValueComparison`: `Eq`, `Gt`, `Gte`, `In`, `IsNotNull`, `IsNull`, `Lt`, `Lte`, `Ne`; `nullableRowComparison`: `Value`; `nullableTextRowExpression`: `Param`, `Value`; `nullableTextValueComparison`: `Contains`, `EndsWith`, `Eq`, `Gt`, `Gte`, `IContains`, `IEndsWith`, `ILike`, `IStartsWith`, `In`, `IsNotNull`, `IsNull`, `Like`, `Lt`, `Lte`, `Ne`, `StartsWith`; `order`: `NullsFirst`, `NullsLast`; `orderedAggregate`: `Asc`, `Desc`, `Eq`, `Filter`, `Gt`, `Gte`, `In`, `Lt`, `Lte`, `Ne`, `Param`, `Value`; `orderedRowExpression`: `Param`, `Value`; `orderedValueComparison`: `Eq`, `Gt`, `Gte`, `In`, `IsNotNull`, `IsNull`, `Lt`, `Lte`, `Ne`; `rowComparison`: `Value`; `rowExpression`: `Param`, `Value`; `selected`: `Asc`, `Desc`, `Key`, `Param`; `selectedOrder`: `NullsFirst`, `NullsLast`; `textRowExpression`: `Param`, `Value`; `textValueComparison`: `Contains`, `EndsWith`, `Eq`, `Gt`, `Gte`, `IContains`, `IEndsWith`, `ILike`, `IStartsWith`, `In`, `IsNotNull`, `IsNull`, `Like`, `Lt`, `Lte`, `Ne`, `StartsWith`; `valueComparison`: `Eq`, `In`, `IsNotNull`, `IsNull`, `Ne` |
| `database/postgres` | 56 | `arrayGetter`: `Dimensions`, `Index`, `IndexType`; `arraySetter`: `ScanIndex`, `ScanIndexType`, `SetDimensions`; `checkedValuer`: `Value`; `checkedValuerPlan`: `Encode`, `SetNext`; `compositeGetter`: `Index`, `IsNull`; `compositeSetter`: `ScanIndex`, `ScanNull`; `encodeNativePlan`: `Encode`; `encodeValuePlan`: `Encode`; `envelopeEncodePlan`: `Encode`, `SetNext`; `envelopeScanPlan`: `Scan`, `SetNext`; `failedEncodePlan`: `Encode`; `failedScanPlan`: `Scan`; `multirangeGetter`: `Index`, `IndexType`, `IsNull`, `Len`; `multirangeSetter`: `ScanIndex`, `ScanIndexType`, `ScanNull`, `SetLen`; `privateSQLValue`: `Format`; `protectedCodec`: `DecodeDatabaseSQLValue`, `DecodeValue`, `FormatSupported`, `PlanEncode`, `PlanScan`, `PreferredFormat`; `protectedConnection`: `CheckNamedValue`, `ExecContext`, `Prepare`, `PrepareContext`, `QueryContext`; `protectedConnector`: `Connect`, `Driver`; `protectedRewriter`: `RewriteQuery`; `protectedRows`: `ScanColumn`; `protectedStatement`: `ExecContext`, `QueryContext`; `rangeGetter`: `BoundTypes`, `Bounds`, `IsNull`; `rangeSetter`: `ScanBounds`, `ScanNull`, `SetBoundTypes`; `registrationPlan`: `Encode`; `scanNativePlan`: `Scan`; `scanValuePlan`: `Scan` |
| `datatable` | 8 | `csvReport`: `Abort`, `Finish`, `Row`; `exportWriter`: `Write`; `xlsxReport`: `Abort`, `Finish`, `Row`; `xmlBudget`: `Write` |
| `datatable/importer` | 3 | `budgetReader`: `Close`, `Read`; `csvGuard`: `Read` |
| `devserver` | 1 | `prefixWriter`: `Write` |
| `email` | 6 | `boundedBuffer`: `Bytes`, `Reset`, `String`, `Write`, `WriteString`; `childPartWriter`: `Write` |
| `email/smtp` | 4 | `loginAuth`: `Next`, `Start`; `xoauth2`: `Next`, `Start` |
| `foundation` | 5 | `pluginProvider`: `Boot`, `Dependencies`, `ID`, `Register`; `taskName`: `String` |
| `http` | 88 | `assetBody`: `Close`; `base64CookieCodec`: `Format`, `Parse`; `boolPathCodec`: `Format`, `FoundryAdapter`, `Parse`; `compressionResponse`: `FlushError`, `Header`, `ReadFrom`, `Unwrap`, `Write`, `WriteHeader`; `compressionSink`: `Write`; `cookieEncryptionError`: `Error`; `cookieSignatureError`: `Error`; `credentialCookieCodec`: `Format`, `FoundryAdapter`, `Parse`; `declaredResponseError`: `Is`, `Unwrap`; `describedURLCodec`: `Format`, `Parse`; `enumURLCodec`: `Format`, `Parse`; `etagConditionWriter`: `WriteHeader`; `etagResponse`: `FlushError`, `Header`, `ReadFrom`, `Unwrap`, `Write`, `WriteHeader`; `fileRangeErrorWriter`: `WriteHeader`; `fileReader`: `Close`, `Failure`, `Read`, `Seek`; `fileResponse`: `Validate`; `fileResponseWriter`: `Header`, `ReadFrom`, `Write`, `WriteHeader`; `floatPathCodec`: `Format`, `FoundryAdapter`, `Parse`; `idempotencyRetryError`: `Unwrap`; `integerPathCodec`: `Format`, `FoundryAdapter`, `Parse`; `localizedResponseWriter`: `FlushError`, `ReadFrom`, `Unwrap`; `metadataByteCounter`: `Write`; `modelIDPathCodec`: `Format`, `FoundryAdapter`, `Parse`; `multipartSource`: `Read`; `observedResponse`: `FlushError`, `Header`, `ReadFrom`, `Unwrap`, `Write`, `WriteHeader`; `rateLimitRejection`: `Error`, `Is`; `rawBodyReader`: `Read`; `refreshClearingWriter`: `Unwrap`, `Write`, `WriteHeader`; `responseBuffer`: `ReadAt`; `responseController`: `EnableFullDuplex`, `Hijack`, `Unwrap`; `responseError`: `Error`, `FoundryDiagnostic`, `Is`, `Unwrap`; `responseFlusher`: `Flush`; `responseHijacker`: `Hijack`; `responsePusher`: `Push`; `routingResponse`: `Header`, `Write`, `WriteHeader`; `signedURLError`: `Error`; `stringPathCodec`: `Format`, `FoundryAdapter`, `Parse`; `textPathCodec`: `Format`, `Parse` |
| `http/pagination` | 6 | `authenticatedPageEndpoint`: `Handle`, `URL`; `cursorCodec`: `Format`, `Parse`; `pageEndpoint`: `Handle`, `URL` |
| `imaging` | 3 | `boundedOutput`: `Write`; `rgbaPNGFrame`: `Opaque`; `webpOutput`: `Write` |
| `jobs` | 4 | `payloadError`: `Error`, `Unwrap`; `permanentError`: `Error`, `Unwrap` |
| `lease/memory` | 5 | `expiryHeap`: `Len`, `Less`, `Pop`, `Push`, `Swap` |
| `logging` | 21 | `correlationHandler`: `Enabled`, `Handle`, `WithAttrs`, `WithGroup`; `customHandler`: `Enabled`, `Handle`, `WithAttrs`, `WithGroup`; `rotatingFile`: `Close`, `Write`; `severityHandler`: `Enabled`, `Handle`, `WithAttrs`, `WithGroup`; `severityWriter`: `Write`; `stackHandler`: `Enabled`, `Handle`, `WithAttrs`, `WithGroup`; `syslogConnection`: `Close`, `Close` |
| `metadata` | 11 | `valueSlot`: `Describe`, `Forget`, `ForgetIn`, `Format`, `From`, `Key`, `Name`, `Registration`, `Save`, `SaveIn`, `Validate` |
| `pubsub` | 2 | `handlerFailure`: `Error`, `Unwrap` |
| `pubsub/memory` | 5 | `stream`: `Close`, `Done`, `Err`, `FoundryAdapter`, `Next` |
| `ratelimit/memory` | 5 | `expiries`: `Len`, `Less`, `Pop`, `Push`, `Swap` |
| `redis` | 15 | `containedTLSConn`: `Close`, `Read`, `Write`; `subscription`: `Close`, `Done`, `Err`, `FoundryAdapter`, `Next`; `tlsReader`: `Read`; `tlsSessionCache`: `Get`, `Put`; `tlsSigner`: `Public`, `Sign`, `SignMessage`; `tlsWriter`: `Write` |
| `redis/data` | 8 | `handle`: `AdapterKey`, `Count`, `Delete`, `DeleteMany`, `Exists`, `Expire`, `Name`, `Version` |
| `sanitize` | 2 | `boundedOutput`: `Write`; `contextInput`: `Read` |
| `storage` | 6 | `ownedReader`: `Close`, `Read`; `putReader`: `Read`; `seekReader`: `Close`, `Read`, `Seek` |
| `storage/local` | 7 | `candidates`: `Len`, `Less`, `Pop`, `Push`, `Swap`; `objectReader`: `Close`, `Read` |
| `storage/s3` | 1 | `multipartWriter`: `Write` |
| `testkit/storage` | 1 | `brokenReader`: `Read` |
| `translations` | 18 | `textSlot`: `Clear`, `ClearIn`, `Describe`, `Field`, `Forget`, `ForgetIn`, `Format`, `From`, `MergeRule`, `Name`, `Options`, `Registration`, `Rule`, `Save`, `SaveIn`, `Sync`, `SyncIn`, `Validate` |
| `value` | 1 | `jsonOutput`: `Write` |
| `webhook/outbound` | 1 | `deliverySink`: `RecordFailure` |
| `websocket` | 4 | `clusterError`: `Error`, `Unwrap`; `notPublished`: `Error`, `Unwrap` |

Packages absent from this table have zero hidden receiver methods. Counts include
both platform-specific declarations of `logging.syslogConnection.Close`.

## Documentation baseline

| Package | Exported declarations | Missing doc comments | Missing DSL examples |
| --- | ---: | ---: | ---: |
| `.` | 1 | 0 | 0 |
| `application` | 268 | 95 | 0 |
| `attachments` | 255 | 100 | 0 |
| `attachments/command` | 7 | 3 | 0 |
| `attribution` | 27 | 12 | 0 |
| `audit` | 82 | 35 | 0 |
| `audit/command` | 4 | 1 | 0 |
| `audit/record` | 72 | 30 | 0 |
| `auth` | 193 | 93 | 0 |
| `auth/challenge` | 74 | 52 | 0 |
| `auth/challenge/postgres` | 10 | 8 | 0 |
| `auth/emailverification` | 12 | 9 | 0 |
| `auth/lockout` | 71 | 47 | 0 |
| `auth/lockout/memory` | 6 | 6 | 0 |
| `auth/mfa` | 116 | 78 | 0 |
| `auth/mfa/command` | 4 | 1 | 0 |
| `auth/mfa/postgres` | 11 | 7 | 0 |
| `auth/oauth` | 38 | 13 | 0 |
| `auth/password` | 31 | 17 | 0 |
| `auth/passwordreset` | 13 | 9 | 0 |
| `auth/session` | 112 | 59 | 0 |
| `auth/session/postgres` | 22 | 10 | 0 |
| `auth/token` | 119 | 69 | 0 |
| `auth/token/postgres` | 25 | 9 | 0 |
| `cache` | 187 | 96 | 0 |
| `cache/file` | 17 | 6 | 0 |
| `cache/memory` | 34 | 26 | 0 |
| `cache/null` | 23 | 20 | 0 |
| `cache/postgres` | 20 | 4 | 0 |
| `cli` | 39 | 21 | 0 |
| `clock` | 4 | 1 | 0 |
| `cloud/credentials` | 24 | 16 | 0 |
| `collection` | 24 | 9 | 0 |
| `config` | 34 | 10 | 0 |
| `config/toml` | 3 | 0 | 0 |
| `contract` | 85 | 16 | 0 |
| `contract/manifest` | 22 | 11 | 0 |
| `countries` | 30 | 14 | 0 |
| `database` | 158 | 42 | 0 |
| `database/codec` | 36 | 0 | 0 |
| `database/command` | 7 | 0 | 0 |
| `database/encrypted` | 31 | 21 | 0 |
| `database/lifecycle` | 32 | 7 | 0 |
| `database/migrate` | 46 | 10 | 0 |
| `database/orm` | 942 | 5 | 0 |
| `database/orm/expr` | 796 | 48 | 441 |
| `database/orm/rows` | 445 | 0 | 0 |
| `database/postgres` | 72 | 59 | 0 |
| `database/prune` | 13 | 0 | 0 |
| `database/relation` | 23 | 6 | 0 |
| `database/seed` | 7 | 2 | 0 |
| `datatable` | 171 | 84 | 77 |
| `datatable/importer` | 28 | 10 | 0 |
| `decimal` | 26 | 7 | 0 |
| `decimal/money` | 29 | 12 | 0 |
| `devserver` | 5 | 1 | 0 |
| `diagnostics` | 17 | 7 | 0 |
| `email` | 151 | 105 | 0 |
| `email/cloudflare` | 7 | 6 | 0 |
| `email/failover` | 5 | 2 | 0 |
| `email/log` | 7 | 4 | 0 |
| `email/mailgun` | 8 | 7 | 0 |
| `email/memory` | 7 | 7 | 0 |
| `email/postmark` | 8 | 7 | 0 |
| `email/resend` | 8 | 7 | 0 |
| `email/ses` | 8 | 7 | 0 |
| `email/smtp` | 13 | 9 | 0 |
| `encryption` | 35 | 23 | 0 |
| `enum` | 15 | 7 | 0 |
| `events` | 61 | 23 | 0 |
| `extensions` | 60 | 24 | 0 |
| `extensions/slots` | 30 | 6 | 0 |
| `fault` | 21 | 8 | 0 |
| `foundation` | 98 | 38 | 0 |
| `health` | 15 | 9 | 0 |
| `health/checks` | 3 | 0 | 0 |
| `http` | 826 | 383 | 0 |
| `http/modelbinding` | 49 | 26 | 0 |
| `http/pagination` | 62 | 25 | 0 |
| `httpclient` | 95 | 66 | 0 |
| `i18n` | 74 | 29 | 0 |
| `i18n/message` | 16 | 11 | 0 |
| `i18n/numberformat` | 13 | 3 | 0 |
| `idempotency` | 58 | 34 | 0 |
| `imaging` | 141 | 68 | 0 |
| `infrastructure` | 219 | 106 | 0 |
| `inspection` | 10 | 6 | 0 |
| `jobs` | 274 | 136 | 0 |
| `jobs/archive` | 19 | 8 | 0 |
| `jobs/archive/command` | 4 | 1 | 0 |
| `jobs/command` | 4 | 1 | 0 |
| `jobs/inline` | 3 | 1 | 0 |
| `jobs/memory` | 21 | 14 | 0 |
| `keyspace` | 12 | 5 | 0 |
| `lease` | 78 | 32 | 0 |
| `lease/memory` | 14 | 9 | 0 |
| `logging` | 93 | 50 | 0 |
| `maintenance` | 41 | 13 | 0 |
| `maintenance/cachestore` | 4 | 3 | 0 |
| `metadata` | 78 | 31 | 0 |
| `metadata/command` | 3 | 2 | 0 |
| `model` | 36 | 19 | 0 |
| `notifications` | 112 | 67 | 0 |
| `notifications/command` | 4 | 1 | 0 |
| `observability` | 55 | 21 | 0 |
| `openapi` | 4 | 0 | 0 |
| `outbox` | 8 | 1 | 0 |
| `outbox/command` | 4 | 1 | 0 |
| `outbox/publisher` | 30 | 14 | 0 |
| `plugin` | 17 | 12 | 0 |
| `plugin/assets` | 14 | 8 | 0 |
| `plugin/manifest` | 17 | 12 | 0 |
| `plugin/scaffold` | 10 | 6 | 0 |
| `pubsub` | 72 | 47 | 0 |
| `pubsub/memory` | 11 | 8 | 0 |
| `randomtoken` | 4 | 1 | 0 |
| `ratelimit` | 47 | 26 | 0 |
| `ratelimit/memory` | 12 | 7 | 0 |
| `redis` | 143 | 70 | 0 |
| `redis/data` | 119 | 65 | 0 |
| `redis/raw` | 84 | 64 | 0 |
| `sanitize` | 8 | 6 | 0 |
| `schedule` | 92 | 58 | 0 |
| `schedule/command` | 4 | 1 | 0 |
| `secret` | 10 | 9 | 0 |
| `settings` | 65 | 35 | 0 |
| `storage` | 203 | 139 | 0 |
| `storage/http` | 3 | 0 | 0 |
| `storage/local` | 25 | 18 | 0 |
| `storage/s3` | 39 | 21 | 0 |
| `str` | 8 | 0 | 0 |
| `temporal` | 83 | 58 | 0 |
| `testkit` | 9 | 4 | 0 |
| `testkit/auth` | 2 | 0 | 0 |
| `testkit/dbassert` | 5 | 0 | 0 |
| `testkit/email` | 9 | 5 | 0 |
| `testkit/events` | 14 | 5 | 0 |
| `testkit/factory` | 29 | 6 | 0 |
| `testkit/http` | 18 | 1 | 0 |
| `testkit/httpclient` | 24 | 18 | 0 |
| `testkit/jobs` | 7 | 1 | 0 |
| `testkit/notifications` | 13 | 4 | 0 |
| `testkit/postgres` | 9 | 2 | 0 |
| `testkit/postgres/application` | 15 | 6 | 0 |
| `testkit/redis` | 1 | 0 | 0 |
| `testkit/storage` | 3 | 1 | 0 |
| `testkit/websocket` | 10 | 5 | 0 |
| `tests/oracle` | 33 | 18 | 0 |
| `tracing` | 33 | 20 | 0 |
| `translations` | 105 | 46 | 0 |
| `translations/command` | 3 | 3 | 0 |
| `typescript` | 8 | 1 | 0 |
| `validation` | 215 | 62 | 0 |
| `validation/database` | 17 | 0 | 0 |
| `validation/imaging` | 9 | 4 | 0 |
| `value` | 37 | 19 | 0 |
| `webhook` | 27 | 16 | 0 |
| `webhook/outbound` | 57 | 23 | 0 |
| `webhook/outbound/command` | 4 | 1 | 0 |
| `websocket` | 169 | 104 | 0 |

The missing-comment total includes 743 functions, 424 types, 2,449 methods, and
250 methods specified in exported interfaces. Twenty missing comments are in
generator-owned output. DSL baseline: expr has 48 missing comments and 441
missing examples; datatable has 84 missing comments and 77 missing examples.

The standalone-line count includes 155 expr declarations with an inline
`Example:` sentence that must move to its own comment line. Another 286 expr
and 77 datatable functions/types have no example text at all.

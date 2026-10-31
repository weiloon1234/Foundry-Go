package foundry_test

// These are the exact pre-existing methods outside expr/datatable. The lead will
// decide their visibility separately. Entries do not exempt future methods or
// receivers; the guard also rejects stale entries.
var consumerAPIPrivateMethodExceptions = map[string]string{
	// Existing schema transaction adapter; outside the approved DSL migration.
	"application.schemaTransactor": "Transaction",
	// Existing slot API implementations and owned-reader adapters; lead review deferred.
	"attachments.manySlot":       "Add AddFile AddIn Format From PublicURLs Reorder",
	"attachments.oneSlot":        "Format From PublicURL",
	"attachments.slotDefinition": "Accepts Clear Collection Describe Detach Discard Name Prepare PrepareFile Registration Replace ReplaceFile ReplaceIn TemporaryURL URL Validate VariantURL",
	"attachments.verifiedReader": "Close Read",
	// Existing audit metadata interface implementation; lead review deferred.
	"audit.metadata": "Area Correlation CreatedAt Origin Route Sequence",
	// Existing typed error adapters; outside the approved DSL migration.
	"auth.failure": "Error Is Unwrap",
	// Existing availability error adapter; outside the approved DSL migration.
	"auth/lockout.unavailable": "Error Is Unwrap",
	// Existing error and typed codec adapters; outside the approved DSL migration.
	"auth/oauth.failureCode":  "Error",
	"auth/oauth.pendingCodec": "Format Parse",
	// Existing scoped cache interface adapters; lead review deferred.
	"cache.snapshotAccess": "Add Exists Expire Forget Get Increment Put",
	"cache.taggedAccess":   "Add Exists Expire Forget Get Increment Put PutLeased",
	// Existing container/heap implementation; outside the approved DSL migration.
	"cache/memory.expiryHeap": "Len Less Pop Push Swap",
	// Existing io.Writer implementation; outside the approved DSL migration.
	"cli.helpWriter": "Write",
	// Existing database row and encryption adapters; lead review deferred.
	"database.collectedRow": "FieldEncryption Scan",
	// Existing transaction, clock, executor, and audit codec adapters; lead review deferred.
	"database/orm.auditKeyCodec":   "Bind Decode",
	"database/orm.eachTransaction": "Transaction",
	"database/orm.fixedNow":        "Now",
	"database/orm.readExecutor":    "Query",
	// Existing database/sql and pgx protocol adapters; lead review deferred.
	"database/postgres.arrayGetter":         "Dimensions Index IndexType",
	"database/postgres.arraySetter":         "ScanIndex ScanIndexType SetDimensions",
	"database/postgres.checkedValuer":       "Value",
	"database/postgres.checkedValuerPlan":   "Encode SetNext",
	"database/postgres.compositeGetter":     "Index IsNull",
	"database/postgres.compositeSetter":     "ScanIndex ScanNull",
	"database/postgres.encodeNativePlan":    "Encode",
	"database/postgres.encodeValuePlan":     "Encode",
	"database/postgres.envelopeEncodePlan":  "Encode SetNext",
	"database/postgres.envelopeScanPlan":    "Scan SetNext",
	"database/postgres.failedEncodePlan":    "Encode",
	"database/postgres.failedScanPlan":      "Scan",
	"database/postgres.multirangeGetter":    "Index IndexType IsNull Len",
	"database/postgres.multirangeSetter":    "ScanIndex ScanIndexType ScanNull SetLen",
	"database/postgres.privateSQLValue":     "Format",
	"database/postgres.protectedCodec":      "DecodeDatabaseSQLValue DecodeValue FormatSupported PlanEncode PlanScan PreferredFormat",
	"database/postgres.protectedConnection": "CheckNamedValue ExecContext Prepare PrepareContext QueryContext",
	"database/postgres.protectedConnector":  "Connect Driver",
	"database/postgres.protectedRewriter":   "RewriteQuery",
	"database/postgres.protectedRows":       "ScanColumn",
	"database/postgres.protectedStatement":  "ExecContext QueryContext",
	"database/postgres.rangeGetter":         "BoundTypes Bounds IsNull",
	"database/postgres.rangeSetter":         "ScanBounds ScanNull SetBoundTypes",
	"database/postgres.registrationPlan":    "Encode",
	"database/postgres.scanNativePlan":      "Scan",
	"database/postgres.scanValuePlan":       "Scan",
	// Existing bounded io.Reader implementations; outside the approved DSL migration.
	"datatable/importer.budgetReader": "Close Read",
	"datatable/importer.csvGuard":     "Read",
	// Existing io.Writer implementation; outside the approved DSL migration.
	"devserver.prefixWriter": "Write",
	// Existing bounded buffer and MIME io.Writer implementations; lead review deferred.
	"email.boundedBuffer":   "Bytes Reset String Write WriteString",
	"email.childPartWriter": "Write",
	// Existing net/smtp.Auth implementations; outside the approved DSL migration.
	"email/smtp.loginAuth": "Next Start",
	"email/smtp.xoauth2":   "Next Start",
	// Existing plugin provider and task-name adapters; lead review deferred.
	"foundation.pluginProvider": "Boot Dependencies ID Register",
	"foundation.taskName":       "String",
	// Existing HTTP, codec, body, diagnostic, and response-controller adapters; lead review deferred.
	"http.assetBody":               "Close",
	"http.base64CookieCodec":       "Format Parse",
	"http.boolPathCodec":           "Format FoundryAdapter Parse",
	"http.compressionResponse":     "FlushError Header ReadFrom Unwrap Write WriteHeader",
	"http.compressionSink":         "Write",
	"http.cookieEncryptionError":   "Error",
	"http.cookieSignatureError":    "Error",
	"http.credentialCookieCodec":   "Format FoundryAdapter Parse",
	"http.declaredResponseError":   "Is Unwrap",
	"http.describedURLCodec":       "Format Parse",
	"http.enumURLCodec":            "Format Parse",
	"http.etagConditionWriter":     "WriteHeader",
	"http.etagResponse":            "FlushError Header ReadFrom Unwrap Write WriteHeader",
	"http.fileRangeErrorWriter":    "WriteHeader",
	"http.fileReader":              "Close Failure Read Seek",
	"http.fileResponse":            "Validate",
	"http.fileResponseWriter":      "Header ReadFrom Write WriteHeader",
	"http.floatPathCodec":          "Format FoundryAdapter Parse",
	"http.idempotencyRetryError":   "Unwrap",
	"http.integerPathCodec":        "Format FoundryAdapter Parse",
	"http.localizedResponseWriter": "FlushError ReadFrom Unwrap",
	"http.metadataByteCounter":     "Write",
	"http.modelIDPathCodec":        "Format FoundryAdapter Parse",
	"http.multipartSource":         "Read",
	"http.observedResponse":        "FlushError Header ReadFrom Unwrap Write WriteHeader",
	"http.rateLimitRejection":      "Error Is",
	"http.rawBodyReader":           "Read",
	"http.refreshClearingWriter":   "Unwrap Write WriteHeader",
	"http.responseBuffer":          "ReadAt",
	"http.responseController":      "EnableFullDuplex Hijack Unwrap",
	"http.responseError":           "Error FoundryDiagnostic Is Unwrap",
	"http.responseFlusher":         "Flush",
	"http.responseHijacker":        "Hijack",
	"http.responsePusher":          "Push",
	"http.routingResponse":         "Header Write WriteHeader",
	"http.signedURLError":          "Error",
	"http.stringPathCodec":         "Format FoundryAdapter Parse",
	"http.textPathCodec":           "Format Parse",
	// Existing endpoint and cursor codec adapters; lead review deferred.
	"http/pagination.authenticatedPageEndpoint": "Handle URL",
	"http/pagination.cursorCodec":               "Format Parse",
	"http/pagination.pageEndpoint":              "Handle URL",
	// Existing bounded io.Writer and image protocol adapters; lead review deferred.
	"imaging.boundedOutput": "Write",
	"imaging.rgbaPNGFrame":  "Opaque",
	"imaging.webpOutput":    "Write",
	// Existing error classification adapters; outside the approved DSL migration.
	"jobs.payloadError":   "Error Unwrap",
	"jobs.permanentError": "Error Unwrap",
	// Existing container/heap implementation; outside the approved DSL migration.
	"lease/memory.expiryHeap": "Len Less Pop Push Swap",
	// Existing slog.Handler, io.Writer, and connection adapters; lead review deferred.
	"logging.correlationHandler": "Enabled Handle WithAttrs WithGroup",
	"logging.customHandler":      "Enabled Handle WithAttrs WithGroup",
	"logging.rotatingFile":       "Close Write",
	"logging.severityHandler":    "Enabled Handle WithAttrs WithGroup",
	"logging.severityWriter":     "Write",
	"logging.stackHandler":       "Enabled Handle WithAttrs WithGroup",
	"logging.syslogConnection":   "Close",
	// Existing typed metadata slot implementation; lead review deferred.
	"metadata.valueSlot": "Describe Forget ForgetIn Format From Key Name Registration Save SaveIn Validate",
	// Existing handler error adapter; outside the approved DSL migration.
	"pubsub.handlerFailure": "Error Unwrap",
	// Existing stream interface implementation; lead review deferred.
	"pubsub/memory.stream": "Close Done Err FoundryAdapter Next",
	// Existing container/heap implementation; outside the approved DSL migration.
	"ratelimit/memory.expiries": "Len Less Pop Push Swap",
	// Existing stream and TLS protocol adapters; lead review deferred.
	"redis.containedTLSConn": "Close Read Write",
	"redis.subscription":     "Close Done Err FoundryAdapter Next",
	"redis.tlsReader":        "Read",
	"redis.tlsSessionCache":  "Get Put",
	"redis.tlsSigner":        "Public Sign SignMessage",
	"redis.tlsWriter":        "Write",
	// Existing typed data-handle implementation; lead review deferred.
	"redis/data.handle": "AdapterKey Count Delete DeleteMany Exists Expire Name Version",
	// Existing bounded I/O implementations; outside the approved DSL migration.
	"sanitize.boundedOutput": "Write",
	"sanitize.contextInput":  "Read",
	// Existing owned io.Reader implementations; outside the approved DSL migration.
	"storage.ownedReader": "Close Read",
	"storage.putReader":   "Read",
	"storage.seekReader":  "Close Read Seek",
	// Existing heap and io.ReadCloser implementations; outside the approved DSL migration.
	"storage/local.candidates":   "Len Less Pop Push Swap",
	"storage/local.objectReader": "Close Read",
	// Existing multipart io.Writer implementation; outside the approved DSL migration.
	"storage/s3.multipartWriter": "Write",
	// Existing fault-injection io.Reader implementation; outside the approved DSL migration.
	"testkit/storage.brokenReader": "Read",
	// Existing typed translation slot implementation; lead review deferred.
	"translations.textSlot": "Clear ClearIn Describe Field Forget ForgetIn Format From MergeRule Name Options Registration Rule Save SaveIn Sync SyncIn Validate",
	// Existing JSON encoding io.Writer implementation; outside the approved DSL migration.
	"value.jsonOutput": "Write",
	// Existing failure sink implementation; lead review deferred.
	"webhook/outbound.deliverySink": "RecordFailure",
	// Existing delivery error adapters; outside the approved DSL migration.
	"websocket.clusterError": "Error Unwrap",
	"websocket.notPublished": "Error Unwrap",
}

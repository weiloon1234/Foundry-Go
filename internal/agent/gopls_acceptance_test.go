package agent

import (
	"context"
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This gate deliberately requires an approved, existing real gopls executable.
// Ordinary tests skip it; make agent-smoke refuses to run without that selection.
func TestRealGoplsConsumer(t *testing.T) {
	testkit.TrackExternalInputs(t)
	executable := os.Getenv("FOUNDRY_TEST_GOPLS")
	if executable == "" {
		t.Skip("real gopls acceptance requires FOUNDRY_TEST_GOPLS; no installation is automatic")
	}
	if strings.ContainsRune(executable, filepath.Separator) && !filepath.IsAbs(executable) {
		absolute, err := filepath.Abs(executable)
		if err != nil {
			t.Fatal(err)
		}
		executable = absolute
	}
	workspace, err := filepath.Abs("../../tests/fixtures/consumer")
	if err != nil {
		t.Fatal(err)
	}
	// Check the shared probe catalogue before starting any gopls requests.
	// This also makes a focused run catch stale consumer expressions promptly.
	for _, probe := range consumerEditorProbes() {
		data, err := os.ReadFile(filepath.Join(workspace, probe.file))
		if err != nil {
			t.Fatalf("editor probe %s: %v", probe.name, err)
		}
		if !strings.Contains(string(data), probe.prefix+probe.symbol) {
			t.Fatalf("editor probe %s: consumer expression was removed from %s", probe.name, probe.file)
		}
	}
	path := filepath.Join(workspace, "catalog", "product.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	marker := "models.UserDraft{}.SetID"
	start := strings.Index(source, marker)
	if start < 0 {
		t.Fatal("consumer expression for semantic inspection was removed")
	}
	selector := start + len("models.UserDraft{}.")
	for _, operation := range []string{"complete", "hover", "definition"} {
		t.Run(operation, func(t *testing.T) {
			parallelRealGopls(t)
			offset := selector
			if operation != "complete" {
				offset++
			}
			options := Options{Workspace: workspace, File: path, Operation: operation, Gopls: executable, Line: strings.Count(source[:offset], "\n") + 1, Column: offset - strings.LastIndexByte(source[:offset], '\n')}
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			result, err := Inspect(ctx, options)
			if err != nil {
				t.Fatal(err)
			}
			if result.Server.Name != "gopls" {
				t.Fatalf("expected real gopls server identity, got %+v", result.Server)
			}
			// gopls may encode its full build information in Version. Log only
			// the version so this acceptance gate stays readable.
			version := result.Server.Version
			var build struct{ Version string }
			if json.Unmarshal([]byte(version), &build) == nil && build.Version != "" {
				version = build.Version
			}
			t.Logf("gopls server: %s %s", result.Server.Name, version)
			switch operation {
			case "complete":
				var list struct {
					Items []struct{ Label string } `json:"items"`
				}
				if err := json.Unmarshal(result.Payload, &list); err != nil {
					if err := json.Unmarshal(result.Payload, &list.Items); err != nil {
						t.Fatal(err)
					}
				}
				labels := make(map[string]bool)
				for _, item := range list.Items {
					labels[item.Label] = true
				}
				for _, method := range []string{"SetEmail", "SetID", "ClearNickname"} {
					found := false
					for label := range labels {
						found = found || label == method || strings.HasPrefix(label, method+"(")
					}
					if !found {
						t.Fatalf("gopls did not complete generated %s: %s", method, result.Payload)
					}
				}
				for label := range labels {
					if label == "ClearEmail" || strings.HasPrefix(label, "ClearEmail(") {
						t.Fatal("gopls exposed null clearing for non-nullable Email")
					}
				}
			case "hover":
				text := string(result.Payload)
				for _, signature := range []string{"SetID", "ID[", "UserDraft"} {
					if !strings.Contains(text, signature) {
						t.Fatalf("hover lost generated model signature %s: %s", signature, text)
					}
				}
			case "definition":
				if !strings.Contains(string(result.Payload), "/models/user_foundry.gen.go") {
					t.Fatalf("definition did not resolve the installed consumer dependency's generated method: %s", result.Payload)
				}
			}
			current, err := os.ReadFile(path)
			if err != nil || string(current) != source {
				t.Fatal("semantic inspection edited the consumer source")
			}
		})
	}
	for _, probe := range consumerEditorProbes() {
		t.Run(probe.name, func(t *testing.T) {
			parallelRealGopls(t)
			path := filepath.Join(workspace, probe.file)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			source := string(data)
			prefix := probe.prefix
			start := strings.Index(source, prefix+probe.symbol)
			if start < 0 {
				t.Fatal("consumer query inspection expression was removed")
			}
			var operations []Options
			for _, operation := range []string{"complete", "hover", "definition"} {
				offset := start + len(prefix)
				if operation != "complete" {
					offset++
				}
				operations = append(operations, Options{Workspace: workspace, File: path, Operation: operation, Gopls: executable, Line: strings.Count(source[:offset], "\n") + 1, Column: offset - strings.LastIndexByte(source[:offset], '\n')})
			}
			results, err := InspectBatch(t.Context(), operations, 45*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != len(operations) {
				t.Fatal("incomplete editor scenario")
			}
			for _, result := range results {
				switch result.Operation {
				case "complete":
					var list struct {
						Items []struct{ Label string } `json:"items"`
					}
					if err := json.Unmarshal(result.Payload, &list); err != nil {
						if err := json.Unmarshal(result.Payload, &list.Items); err != nil {
							t.Fatal(err)
						}
					}
					for _, method := range probe.completion {
						found := false
						for _, item := range list.Items {
							found = found || item.Label == method || strings.HasPrefix(item.Label, method+"(")
						}
						if !found {
							t.Fatalf("gopls missed query method %s", method)
						}
					}
				case "hover":
					for _, part := range probe.hover {
						if !strings.Contains(string(result.Payload), part) {
							t.Fatalf("typed query hover lost %s: %s", part, result.Payload)
						}
					}
				case "definition":
					if !strings.Contains(string(result.Payload), probe.definition) {
						t.Fatal("query definition missed generated consumer method")
					}
				}
			}
			current, err := os.ReadFile(path)
			if err != nil || string(current) != source {
				t.Fatal("query semantic inspection edited the consumer")
			}
		})
	}
}

// consumerEditorProbe owns one consumer expression and its semantic assertions.
// The preflight and actual gopls calls use this same catalogue.
type consumerEditorProbe struct {
	name, file, prefix, symbol string
	completion, hover          []string
	definition                 string
}

func consumerEditorProbes() []consumerEditorProbe {
	return []consumerEditorProbe{
		{"client-presentation", "clientcontracts/presentation.go", "parameter.", "WithPresentation", []string{"WithPresentation"}, []string{"Presentation", "QueryParameter"}, "/http/presentation.go"},
		{"application-spa-portal", "spaportals/portals.go", "builder.", "SPA", []string{"SPA", "HTTP", "Register", "Build"}, []string{"RouteID", "Assets", "SPAConfig", "Builder"}, "/application/builder.go"},
		{"configured-dates", "configuredprofile/timezone_test.go", "dates.", "Today", []string{"Now", "Today", "Parse", "Format", "AddDays"}, []string{"Date", "error"}, "/temporal/service.go"},
		{"configured-calendar", "configuredprofile/timezone_test.go", "calendar.", "DailyAt", []string{"DailyAt", "Cron", "In", "Weekly"}, []string{"ID", "Handler", "Declaration"}, "/schedule/calendar.go"},
		{"configured-timezone-key", "configuredprofile/timezone_test.go", "keys.TimeZone.", "Set", []string{"Set", "Name", "Sensitive"}, []string{"Settings", "ZoneName"}, "/config/schema.go"},
		{"configured-log-rotation", "configuredprofile/logging_test.go", "policy.", "MaxBytes", []string{"MaxBytes", "MaxFiles", "MaxAge", "Disabled"}, []string{"int64"}, "/logging/rotation_config.go"},
		{"configured-log-rotation-key", "configuredprofile/logging_test.go", "keys.Sink.Rotation.MaxBytes.", "Set", []string{"Set", "Name", "Sensitive"}, []string{"ChannelSettings", "int64"}, "/config/schema.go"},
		{"webhook-typed-account", "security/contracts.go", "delivery.", "Account", []string{"Account", "Provider", "ID", "Timestamp"}, []string{"BillingAccountID"}, "/webhook/verifier.go"},
		{"migration-reconciliation", "security/contracts.go", "runner.", "Reconcile", []string{"Reconcile", "Up", "Status"}, []string{"Progress", "StatementOutcome", "error"}, "/database/migrate/progress.go"},
		{"configured-mail-selector", "bootstrap/supporting.go", "services.Mailers.", "Mailer", []string{"Mailer", "Default", "Names"}, []string{"MailerName", "Mailer", "error"}, "/email/named.go"},
		{"configured-typed-job", "bootstrap/supporting.go", "d.work.", "Dispatch", []string{"Dispatch", "Capture", "Enqueue"}, []string{"Profile", "Receipt", "Context"}, "/jobs/connection.go"},
		{"configured-browser-binding", "bootstrap/persistent_test.go", "members.", "Binding", []string{"Binding", "Browser", "Sessions"}, []string{"GuardBinding", "Member"}, "/application/auth_providers.go"},
		{"bootstrap-domain-actor", "bootstrap/routes.go", "actor.", "Name", []string{"Name", "ID", "FoundryReference"}, []string{"Name", "string"}, "/bootstrap/models.go"},
		{"bootstrap-image-service", "bootstrap/routes.go", "services.", "Image", []string{"Image", "Database", "Cache", "Disk"}, []string{"Engine", "error"}, "/application/services.go"},
		{"bootstrap-configured-timeout", "bootstrap/bootstrap_test.go", "keys.App.HTTP.Server.RequestTimeout.", "Set", []string{"Set", "Name", "Sensitive"}, []string{"Settings", "Duration"}, "/config/schema.go"},
		{"configured-default-cache", "configured/settings.go", "configured.", "Cache", []string{"Cache", "Database", "Disk", "RedisConnection"}, []string{"Store", "error"}, "/infrastructure/services.go"},
		{"configured-named-cache", "configured/settings.go", "configured.Caches.", "Store", []string{"Store", "Default", "Names", "DefaultName"}, []string{"StoreName", "Store", "error"}, "/cache/named.go"},
		{"configured-typed-default", "configured/settings_test.go", "keys.Services.Cache.Default.", "Set", []string{"Set", "Name", "Sensitive"}, []string{"StoreName", "Settings"}, "/config/schema.go"},
		{"generated-config-field", "startupconfig/settings_test.go", "keys.HTTP.", "Port", []string{"Port", "Timeout", "Listen", "Origins"}, []string{"Key[", "Settings", "Port"}, "/startupconfig/settings_foundry.gen.go"},
		{"generated-config-override", "startupconfig/settings_test.go", "keys.HTTP.Port.", "Set", []string{"Set", "Name", "Sensitive"}, []string{"Override[", "Settings", "Port"}, "/config/schema.go"},
		{"production-readiness-module", "tooling/production.go", "health.", "Module", []string{"Module", "NewRegistry", "Probe"}, []string{"Probe", "Resolver", "Module"}, "/health/module.go"},
		{"production-diagnostics-module", "tooling/production.go", "diagnostics.", "Module", []string{"Module", "Route", "New"}, []string{"Config", "Registry", "Module"}, "/diagnostics/module.go"},
		{"production-protected-diagnostics", "tooling/production.go", "diagnostics.", "Route", []string{"Route", "Status", "Metrics", "Readiness"}, []string{"AuthenticatedRoute", "RouteRegistration"}, "/diagnostics/route.go"},
		{"production-shared-observation", "tooling/production.go", "observability.", "Observe", []string{"Observe", "Operation", "OutcomeFor", "New"}, []string{"Operation", "Context", "error"}, "/observability/operation.go"},
		{"production-traced-job", "tooling/production.go", "definition.", "Capture", []string{"Capture", "Dispatch", "Enqueue"}, []string{"Options[", "Pending[", "Context"}, "/jobs/dispatcher.go"},
		{"production-readiness-report", "tooling/production_test.go", "status.", "Readiness", []string{"Readiness", "Liveness", "Snapshot"}, []string{"ReadinessReport", "Context", "error"}, "/diagnostics/runtime.go"},
		{"production-maintenance-gate", "tooling/production_test.go", "recorder.Gate().", "Set", []string{"Set", "Mode", "Admit", "Drain"}, []string{"bool", "error"}, "/maintenance/gate.go"},
		{"production-owned-observation", "tooling/production_test.go", "foundation.", "WithObservability", []string{"WithObservability", "WithShutdownTimeout"}, []string{"Recorder", "Option"}, "/foundation/observability.go"},
		{"database-primary-read", "tooling/routing.go", "db.", "Primary", []string{"Primary", "QueryRead", "Health", "RoutingStats"}, []string{"PrimaryExecutor"}, "/database/routing.go"},
		{"database-routed-module", "tooling/routing.go", "postgres.", "RoutedModule", []string{"RoutedModule", "OpenRouted", "RoutingConfig"}, []string{"RoutingConfig", "Module"}, "/database/postgres/routing.go"},
		{"database-endpoint-health", "tooling/routing.go", "db.", "Health", []string{"Health", "PingPrimary", "PingRead"}, []string{"PoolHealth", "Context"}, "/database/routing.go"},
		{"binary-draft", "binarymodels/record.go", "RecordDraft{}.", "SetID", []string{"SetBody", "SetRaw", "ClearNote"}, []string{"int", "RecordDraft"}, "/binarymodels/record_foundry.gen.go"},
		{"binary-equality", "binarymodels/record.go", "RecordFields().Body.", "Eq", []string{"Eq", "In", "Value", "Set"}, []string{"Payload", "Predicate"}, "/database/query/field.go"},
		{"binary-nullable", "binarymodels/record.go", "RecordFields().Note.", "Value", []string{"Value", "IsNull", "SetNull"}, []string{"Nullable", "Payload"}, "/database/query/field_binary.go"},
		{"binary-custom-input", "binarymodels/record.go", "RecordFields().Body.", "Set", []string{"Set", "Incoming"}, []string{"Input", "ConflictUpdate"}, "/binarymodels/record_foundry.gen.go"},
		{"tooling-cli-declaration", "tooling/commands.go", "Greet.", "Declare", []string{"Declare", "Name", "Validate"}, []string{"Handler[", "Greeting", "Resolver"}, "/cli/command.go"},
		{"tooling-cli-parse", "tooling/commands.go", "registry.", "Parse", []string{"Parse", "Describe"}, []string{"Invocation", "[]string", "Writer"}, "/cli/command.go"},
		{"tooling-cli-invocation", "tooling/commands.go", "invocation.", "Run", []string{"Run", "Name"}, []string{"Context", "Resolver", "Streams"}, "/cli/command.go"},
		{"tooling-inspection", "tooling/commands.go", "inspection.", "Collect", []string{"Collect", "Command", "Write"}, []string{"Sources", "Report", "error"}, "/inspection/report.go"},
		{"tooling-factory-new", "tooling/database.go", "factory.", "New", []string{"New", "Sequence", "Factory"}, []string{"Build[", "Factory[", "error"}, "/testkit/factory/factory.go"},
		{"tooling-query-plan", "tooling/database.go", "models.QueryUsers().Limit(10).", "Explain", []string{"Explain", "ExplainAnalyze", "All"}, []string{"Executor", "Plan", "error"}, "/database/query/explain.go"},
		{"tooling-locked-analysis", "tooling/database.go", "models.QueryUsers().ForUpdate().", "ExplainAnalyze", []string{"Explain", "ExplainAnalyze", "NoWait"}, []string{"Tx", "Plan", "error"}, "/database/query/explain.go"},
		{"tooling-factory-draft", "tooling/tooling_test.go", "records.", "Draft", []string{"Draft", "Create", "CreateMany", "WithStates"}, []string{"WriteRecordDraft", "Context", "error"}, "/testkit/factory/factory.go"},
		{"plugin-registration", "pluginusage/bootstrap.go", "foundry.New().", "RegisterPlugin", []string{"RegisterPlugin", "Register", "Build", "Inspect", "OverrideContributions"}, []string{"Plugin", "Builder"}, "/foundation/plugins.go"},
		{"plugin-inspection", "pluginusage/bootstrap.go", "builder.", "Inspect", []string{"Inspect", "Build", "RegisterPlugin"}, []string{"context.Context", "Inspection", "error"}, "/foundation/inspection.go"},
		{"plugin-override", "pluginusage/bootstrap.go", "builder.", "OverrideContributions", []string{"OverrideContributions", "RegisterPlugin", "Replace"}, []string{"ProviderID", "Registrar", "Builder"}, "/foundation/inspection.go"},
		{"plugin-assets", "pluginusage/bootstrap.go", "assets.", "Bundles", []string{"Bundles", "New", "Register", "Recover"}, []string{"Resolver", "Bundle", "error"}, "/plugin/assets/assets.go"},
		{"plugin-scaffold-resolution", "pluginusage/bootstrap.go", "scaffold.", "Resolve", []string{"Resolve", "New", "Register", "Declarations"}, []string{"Resolver", "Scaffold", "error"}, "/plugin/scaffold/scaffold.go"},
		{"plugin-asset-publication", "pluginusage/plugins_test.go", "bundles[0].", "Publish", []string{"Publish", "Files", "Info"}, []string{"context.Context", "string", "bool", "Report", "error"}, "/plugin/assets/assets.go"},
		{"plugin-scaffold-publication", "pluginusage/plugins_test.go", "scaffold.", "Publish", []string{"Publish", "Render", "Info"}, []string{"context.Context", "Report", "error"}, "/plugin/scaffold/scaffold.go"},

		{"workflow-patch-service", "teamworkflow/routes.go", "s.", "Patch", []string{"Patch", "Submit", "Routes"}, []string{"Actor", "PatchRequest", "ActionEnvelope"}, "/teamworkflow/service.go"},
		{"workflow-transaction-service", "teamworkflow/routes.go", "s.", "Submit", []string{"Patch", "Submit", "Routes"}, []string{"Tx", "Actor", "SubmissionRequest", "ActionEnvelope"}, "/teamworkflow/service.go"},
		{"workflow-tagged-result", "teamworkflow/service.go", "", "ActionFromQueued", []string{"ActionFromQueued", "ActionFromUpdated"}, []string{"SubmissionView", "Action", "error"}, "/teamworkflow/action_foundry.gen.go"},
		{"idempotency-core-run", "idempotenthttp/operation.go", "op.", "Run", []string{"Run", "Validate", "Definition"}, []string{"Scope", "Key", "Submission", "Receipt"}, "/idempotency/operation.go"},
		{"idempotency-bound-handler", "idempotenthttp/application.go", "operation.", "Handle", []string{"Handle", "WithHeaders", "Description", "Validate"}, []string{"Tx", "Actor", "Workspace", "Receipt"}, "/http/modelbinding/idempotency.go"},
		{"idempotency-configured-store", "idempotenthttp/application.go", "services.", "Idempotency", []string{"Idempotency", "Database", "Events"}, []string{"Store", "error"}, "/application/idempotency.go"},
		{"scoped-pool-settings", "isolatedhttp/isolation_test.go", "environment.", "Settings", []string{"Settings", "Start", "Migrate"}, []string{"DatabaseSettings"}, "/testkit/postgres/application/environment.go"},
		{"scoped-named-connection", "isolatedhttp/isolation_test.go", "pgapp.", "On", []string{"On", "Bind"}, []string{"ConnectionName", "Selection"}, "/testkit/postgres/application/environment.go"},
		{"scoped-generated-draft", "isolatedhttp/application.go", "RecordDraft{}.", "SetName", []string{"SetName", "SetText"}, []string{"string", "RecordDraft"}, "/isolatedhttp/record_foundry.gen.go"},
		{"nested-project-field", "nestedbindings/bindings.go", "input.Model.Parent.Child.", "Slug", []string{"ID", "TeamID", "Slug", "Team", "Tasks"}, []string{"Slug"}, "/nestedbindings/models.go"},
		{"nested-task-field", "nestedbindings/bindings.go", "input.Model.Child.", "Slug", []string{"ID", "ProjectID", "Slug"}, []string{"Slug"}, "/nestedbindings/models.go"},
		{"nested-alternate-key", "nestedbindings/bindings.go", "modelbinding.", "ByField", []string{"ByField", "Through", "ThroughSelected", "Then"}, []string{"KeyField", "Resolver", "Executor"}, "/http/modelbinding/field.go"},
		{"form-generated-field", "requestflow/forms.go", "fields.", "Name", []string{"Name", "Title", "Tags", "Blocked"}, []string{"Field", "Submission", "string"}, "/requestflow/submission_foundry.gen.go"},
		{"form-preparation-input", "requestflow/forms.go", "input.Body.", "Name", []string{"Name", "Title", "Tags", "Blocked"}, []string{"string"}, "/requestflow/forms.go"},
		{"form-typed-actor", "requestflow/forms.go", "actor.", "Enabled", []string{"ID", "Enabled"}, []string{"bool"}, "/requestflow/forms.go"},
		{"union-constructor", "unions/payment.go", "", "PaymentMethodFromCard", []string{"PaymentMethodFromCard", "PaymentMethodFromBank"}, []string{"CardDTO", "PaymentMethod", "error"}, "/unions/payment_method_foundry.gen.go"},
		{"union-accessor", "unions/payment.go", "input.", "Card", []string{"Card", "Bank", "Wrapped"}, []string{"CardDTO", "bool"}, "/unions/payment_method_foundry.gen.go"},
		{"union-payload", "unions/payment.go", "card.", "Token", []string{"Token", "Sequence", "Labels"}, []string{"string"}, "/unions/payment.go"},
		{"generic-dto-data", "genericdto/envelope.go", "in.Data.", "Name", []string{"Name", "ID", "Note", "Sequence"}, []string{"string"}, "/genericdto/envelope.go"},
		{"generic-dto-contract", "genericdto/envelope.go", "", "EnvelopeJSON", []string{"EnvelopeJSON", "EnvelopeValidationFields"}, []string{"JSON", "Envelope", "T"}, "/genericdto/envelope_foundry.gen.go"},
		{"generic-dto-validation", "genericdto/envelope.go", "EnvelopeValidationFields[UserDTO]().", "Data", []string{"Data", "Trace"}, []string{"Field", "Envelope", "UserDTO"}, "/genericdto/envelope_foundry.gen.go"},
		{"client-manifest-build", "clientcontracts/contracts.go", "manifest.", "Build", []string{"Build", "Decode", "Sources", "Manifest"}, []string{"context.Context", "Sources", "Manifest", "error"}, "/contract/manifest/build.go"},
		{"client-typescript-publication", "clientcontracts/contracts.go", "typescript.", "Generate", []string{"Generate", "Render", "Options", "Report"}, []string{"context.Context", "Manifest", "Options", "Report"}, "/typescript/generate.go"},
		{"client-realtime-metadata", "clientcontracts/contracts.go", "websocket.", "DescribeClient", []string{"DescribeClient", "ProtocolDescription", "DefaultConfig"}, []string{"Registry", "Config", "ClientDescription", "error"}, "/websocket/client_metadata.go"},
		{"client-openapi-adapter", "clientcontracts/contracts_test.go", "openapi.", "Render", []string{"Render", "Options", "SpecificationVersion"}, []string{"Manifest", "Options", "byte", "error"}, "/openapi/openapi.go"},
		{"client-manifest-snapshot", "clientcontracts/contracts_test.go", "source.", "Snapshot", []string{"JSON", "Snapshot"}, []string{"Document", "error"}, "/contract/manifest/serialization.go"},
		{"httpclient-request", "outgoing/client.go", "s.client.", "Post", []string{"Post", "Get", "Request", "Do", "Stream", "Close"}, []string{"string", "Request"}, "/httpclient/request.go"},
		{"httpclient-typed-json", "outgoing/client.go", "httpclient.", "JSON", []string{"JSON", "DecodeJSON", "New", "DefaultConfig"}, []string{"contract.JSON", "Request", "payload", "error"}, "/httpclient/json.go"},
		{"httpclient-owned-stream", "outgoing/client.go", "s.client.", "Stream", []string{"Stream", "Do", "Snapshot", "Close"}, []string{"context.Context", "Request", "StreamResponse", "error"}, "/httpclient/client.go"},

		{"validation-translated-message", "localization/validation.go", "validation.", "WithTranslation", []string{"WithTranslation", "ValidateMessages", "MessageDefinitions"}, []string{"Rule", "Message", "args"}, "/validation/messages.go"},
		{"message-typed-bind", "localization/messages.go", "WelcomeArgsMessage().", "Bind", []string{"Bind", "BindLiteral", "Format"}, []string{"WelcomeArgs", "PreparedMessage", "Template"}, "/i18n/message/message.go"},
		{"message-typed-format", "localization/messages.go", "WelcomeArgsMessage().", "Format", []string{"Format", "Key", "Definition", "Description", "Registration"}, []string{"WelcomeArgs", "Catalog", "LocaleID", "Result"}, "/i18n/message/message.go"},
		{"message-generated-contract", "localization/messages.go", "", "CartArgsMessage", []string{"CartArgsMessage", "CartArgsJSON", "CartArgsValidationFields"}, []string{"Message", "CartArgs"}, "/localization/cart_args_foundry.gen.go"},
		{"message-catalog-load", "localization/messages.go", "i18n.", "Load", []string{"Load", "NewCatalog", "NewLocaleSet", "WithLocale"}, []string{"FS", "LocaleCatalog", "MessageDefinition", "Catalog"}, "/i18n/load.go"},
		{"message-enum-label", "localization/messages.go", "Draft.EnumDescriptor().", "LabelDefinitions", []string{"LabelKey", "Label", "LabelDefinitions", "Definition"}, []string{"MessageDefinition", "error"}, "/enum/labels.go"},

		{"datatable-typed-page", "reporting/http.go", "Members.", "Query", []string{"Query", "Count", "Export", "Download", "Inspect"}, []string{"Authority", "Request", "Page", "MemberRow"}, "/datatable/manager.go"},
		{"datatable-http-download", "reporting/http.go", "Members.", "Download", []string{"Download", "Export"}, []string{"Authority", "ExportOptions", "Download", "error"}, "/datatable/http.go"},
		{"datatable-declaration", "reporting/members.go", "datatable.", "Define", []string{"Define", "DefineColumn", "Where", "Related"}, []string{"Spec", "Table"}, "/datatable/declaration.go"},
		{"datatable-typed-filter", "reporting/members.go", "datatable.", "NullableWhere", []string{"NullableWhere", "Where", "Having", "NullableHaving"}, []string{"QueryCodec", "RowNullable", "FilterSource", "Nullable"}, "/datatable/filter.go"},
		{"datatable-queued-export", "reporting/export_job.go", "datatable.", "ExportHandler", []string{"ExportHandler"}, []string{"Table", "Artifact", "Handler", "error"}, "/datatable/jobs.go"},
		{"datatable-generated-row", "reporting/members.go", "", "MemberRowValidationFields", []string{"MemberRowValidationFields", "MemberRowJSON", "MemberRowProjection"}, []string{"MemberRowValidationFieldSet"}, "/reporting/member_row_foundry.gen.go"},

		{"attachment-typed-replace", "profiles/profile.go", "Avatar.", "Replace", []string{"Replace", "Add", "Find", "Load"}, []string{"Reference", "Upload", "Result", "error"}, "/attachments/write.go"},
		{"attachment-localized-batch", "profiles/profile.go", "Localized.", "LoadLocalized", []string{"LoadLocalized", "ForLocale", "Resolve"}, []string{"Reference", "LocalizedBatch", "error"}, "/attachments/localized.go"},
		{"metadata-typed-value", "profiles/profile.go", "PreferencesKey.", "Set", []string{"Set", "Get", "Load", "Matching"}, []string{"Reference", "Preferences", "error"}, "/metadata/values.go"},
		{"translation-typed-locale", "profiles/profile.go", "Label.", "Set", []string{"Set", "Get", "Resolve", "Load"}, []string{"Reference", "LocaleID", "string", "error"}, "/translations/write.go"},
		{"settings-typed-default", "profiles/profile.go", "PageSize.", "GetOr", []string{"GetOr", "Set", "Ensure", "Configure"}, []string{"uint32", "error"}, "/settings/values.go"},
		{"image-immutable-plan", "profiles/profile.go", "imaging.NewPlan().", "Fill", []string{"Fill", "Resize", "Format", "Crop"}, []string{"int", "bool", "Plan"}, "/imaging/plan.go"},
		{"image-positioned-sizing", "profiles/images.go", "\t\t", "Contain", []string{"FillAt", "Pad", "Contain", "Canvas", "RotateDegrees"}, []string{"int", "NRGBA", "Position", "Plan"}, "/imaging/geometry.go"},
		{"image-layer-composition", "profiles/images.go", "\t\t", "Insert", []string{"Insert", "Mask", "Sharpen", "Resampling"}, []string{"Result", "Placement", "Plan"}, "/imaging/composite.go"},
		{"image-text-options", "profiles/image_labels.go", "\t\t", "Text", []string{"Text", "Draw", "Rectangle", "Ellipse", "Polygon"}, []string{"string", "TextOptions", "Plan"}, "/imaging/text.go"},
		{"image-shape-style", "profiles/image_labels.go", "\t\t", "Rectangle", []string{"Rectangle", "Circle", "Line", "Draw"}, []string{"float64", "ShapeStyle", "Plan"}, "/imaging/path.go"},
		{"image-animation-policy", "profiles/animated_images.go", "\t\t", "Frames", []string{"Frames", "Fit", "Format", "Insert"}, []string{"Frames", "Plan"}, "/imaging/plan.go"},
		{"image-png-compression", "profiles/image_encoding.go", "\t\t", "PNGCompression", []string{"PNGCompression", "AVIFSpeed", "AVIFAlphaQuality"}, []string{"PNGCompression", "Plan"}, "/imaging/encoding.go"},
		{"image-avif-speed", "profiles/image_encoding.go", "\t\t", "AVIFSpeed", []string{"AVIFSpeed", "AVIFQuality", "AVIFAlphaQuality"}, []string{"int", "Plan"}, "/imaging/encoding.go"},
		{"image-webp-mode", "profiles/image_encoding.go", "\t\t", "WebPMode", []string{"WebPMode", "WebPQuality", "WebPMethod"}, []string{"WebPMode", "Plan"}, "/imaging/encoding.go"},
		{"image-native-crop", "profiles/native_images.go", "\t\t", "SmartFill", []string{"SmartFill", "ToSRGB", "Format"}, []string{"CropInterest", "Plan"}, "/imaging/backend.go"},
		{"image-native-color", "profiles/native_images.go", "\t\t", "ToSRGB", []string{"ToSRGB", "SmartFill", "Metadata"}, []string{"Plan"}, "/imaging/backend.go"},
		{"country-typed-lookup", "profiles/profile_test.go", "countries.", "Find", []string{"Find", "Seed", "Seeder", "CountryFields"}, []string{"Code", "Country", "error"}, "/countries/queries.go"},

		{"notification-typed-capture", "notifying/orders.go", "binding.", "Capture", []string{"Capture", "Send", "Registration"}, []string{"Reference", "PendingNotification", "error"}, "/notifications/capture.go"},
		{"notification-transaction-enqueue", "notifying/orders.go", "pending.", "Enqueue", []string{"Enqueue", "Send", "ID"}, []string{"Tx", "DeliveryJob", "Outbox", "error"}, "/notifications/jobs.go"},
		{"notification-owned-inbox", "notifying/orders.go", "inbox.", "List", []string{"List", "MarkRead", "MarkUnread", "UnreadCount"}, []string{"PageRequest", "Record", "error"}, "/notifications/inbox.go"},

		{"email-upload-attachment", "mailing/uploads_test.go", "s.message.", "AttachUpload", []string{"AttachUpload", "AttachStored", "Attach", "AttachData"}, []string{"Context", "UploadSource", "Message", "error"}, "/email/attachment_sources.go"},
		{"email-stored-attachment", "articles/email_postgres_test.go", "message.", "AttachStored", []string{"AttachStored", "AttachUpload"}, []string{"StoredAttachment", "Message", "error"}, "/email/attachment_sources.go"},
		{"email-cloudflare-driver", "mailing/configured_test.go", "infrastructure.", "CloudflareMail", []string{"CloudflareMail", "SMTPMail", "SESMail", "ResendMail", "PostmarkMail", "MailgunMail"}, []string{"MailDriver"}, "/infrastructure/mail_settings.go"},
		{"email-cloudflare-config", "mailing/configured_test.go", "keys.API.", "AccountID", []string{"AccountID", "Token", "Endpoint"}, []string{"Key[", "MailerSettings", "string"}, "/infrastructure/mailer_settings_foundry.gen.go"},
		{"email-typed-template", "mailing/welcome.go", "email.", "NewTemplate", []string{"NewTemplate", "NewDynamicTemplate"}, []string{"TemplateSource", "Template", "error"}, "/email/template.go"},
		{"email-job-handler", "mailing/welcome.go", "email.", "JobHandler", []string{"JobHandler", "NewMessage"}, []string{"Mailer", "Builder", "Handler"}, "/email/job.go"},
		{"email-mailer-send", "mailing/welcome.go", "s.mailer.", "Send", []string{"Send", "Snapshot", "Close"}, []string{"Message", "SendOptions", "Result", "error"}, "/email/mailer.go"},

		{"websocket-distributed-constructor", "realtime/orders.go", "websocket.", "NewDistributed", []string{"NewDistributed", "NewPublisher"}, []string{"Registry", "ClusterBackend", "ClusterConfig", "Hub", "error"}, "/websocket/cluster_runtime.go"},
		{"websocket-typed-disconnect", "realtime/orders.go", "websocket.", "DisconnectSubject", []string{"DisconnectSubject", "DisconnectConnection"}, []string{"Guard", "Reference", "PublisherSource", "error"}, "/websocket/disconnect.go"},
		{"websocket-protected-diagnostics", "realtime/orders.go", "websocket.", "Diagnose", []string{"Diagnose"}, []string{"Guard", "Diagnostics", "error"}, "/websocket/diagnostics.go"},
		{"typed-websocket-publish", "realtime/orders.go", "return websocket.", "Publish", []string{"Publish", "Broadcast", "DefineOutgoing"}, []string{"Channel", "Outgoing", "MessageID", "error"}, "/websocket/publish.go"},
		{"typed-websocket-handler", "realtime/orders.go", "Inspect.", "Authorize", []string{"Authorize", "Handle"}, []string{"MessageContext", "OrderResponse", "error"}, "/websocket/event.go"},
		{"typed-websocket-owned-room", "realtime/orders.go", "websocket.", "OwnedRooms", []string{"OwnedRooms", "Private", "Public"}, []string{"Guard", "Reference", "Channel"}, "/websocket/channel.go"},
		{"typed-schedule-daily", "scheduling/reports.go", "schedule.", "DailyAt", []string{"DailyAt", "Every", "JobTarget"}, []string{"ID", "time.Location", "Handler", "Declaration"}, "/schedule/declaration.go"},
		{"typed-schedule-next", "scheduling/reports.go", "spec.", "Next", []string{"Next", "TimeZone", "Validate"}, []string{"time.Time", "error"}, "/schedule/spec.go"},
		{"typed-schedule-module", "scheduling/reports.go", "schedule.", "Module", []string{"Module", "New", "NewRegistry"}, []string{"Scheduler", "lease.Manager", "foundation.Module"}, "/schedule/module.go"},
		{"typed-job-dispatch", "background/welcome.go", "WelcomeJob.", "Dispatch", []string{"Dispatch", "Capture", "Enqueue", "DeclareWith"}, []string{"Welcome", "Dispatcher", "Receipt"}, "/jobs/dispatcher.go"},
		{"typed-job-retry", "background/operations.go", "WelcomeJob.", "Retry", []string{"Retry", "Inspect", "Cancel"}, []string{"Welcome", "RetryToken"}, "/jobs/retry.go"},
		{"configured-job-middleware", "background/operations.go", "application.", "JobWith", []string{"Job", "JobWith"}, []string{"HandlerOptions", "JobDeclaration"}, "/application/declarations.go"},
		{"typed-job-workflow-step", "background/module.go", "first.", "Step", []string{"Step", "Dispatch", "Enqueue"}, []string{"Step"}, "/jobs/workflow.go"},
		{"typed-job-module", "background/module.go", "jobs.", "Module", []string{"Module", "NewWorker", "NewDispatcher"}, []string{"Dispatcher", "foundation", "Module"}, "/jobs/module.go"},

		{"typed-storage-put", "storing/files.go", "disk.", "Put", []string{"Put", "Open", "List"}, []string{"ObjectKey", "io.Reader", "PutOptions", "StoredObject"}, "/storage/disk.go"},
		{"typed-storage-download", "storing/files.go", "storagehttp.", "Download", []string{"Download", "Stream", "StoreUpload"}, []string{"ObjectKey", "ReadOptions", "Download"}, "/storage/http/download.go"},

		{"typed-mfa-observer", "multifactor/security.go", "factors.", "WithObserver", []string{"WithObserver", "RetireIn", "Verifier"}, []string{"Observer", "Account", "error"}, "/auth/mfa/observer.go"},
		{"typed-mfa-retirement", "multifactor/security.go", "factors.", "RetireIn", []string{"RetireIn", "Disable"}, []string{"Reference", "Account", "database.Tx"}, "/auth/mfa/retirement.go"},
		{"typed-auth-signed-endpoint", "authenticating/composed_routes.go", "endpoint.", "Signed", []string{"Signed", "WithPermissions", "Handle"}, []string{"URLSigner", "SignedAuthenticatedEndpoint", "User"}, "/http/signed_authenticated_endpoint.go"},
		{"typed-auth-resource-binding", "authenticating/composed_routes.go", "modelbinding.", "BindAuthenticated", []string{"BindAuthenticated", "ByKey"}, []string{"AuthenticatedTransport", "Resolver", "AuthenticatedEndpoint"}, "/http/modelbinding/authenticated_endpoint.go"},
		{"typed-auth-native-route", "authenticating/composed_routes.go", "route.", "HandleRaw", []string{"HandleRaw", "Signed", "WithPermissions"}, []string{"ResponseWriter", "User", "NoPath"}, "/http/authenticated_route.go"},
		{"typed-auth-permission", "authenticating/permissions.go", "ViewAccount.", "Authorize", []string{"Authorize", "Allows", "Registration"}, []string{"Guard", "User", "error"}, "/auth/permission.go"},
		{"typed-http-permissions", "authenticating/permissions.go", "endpoint.", "WithPermissions", []string{"WithPermissions", "WithScopes", "Handle"}, []string{"Permission", "User", "AuthenticatedEndpoint"}, "/http/authenticated_endpoint.go"},
		{"typed-auth-attribution", "authenticating/attribution.go", "guard.", "Origin", []string{"Origin", "WithAttribution", "Require"}, []string{"context.Context", "attribution.Origin", "error"}, "/auth/attribution.go"},
		{"typed-recovery-revision", "recovering/hooks.go", "draft.", "SetEmailRevision", []string{"SetEmailRevision", "EmailRevision", "UnsetEmailRevision"}, []string{"Revision", "Member", "MemberDraft"}, "/recovering/member_foundry.gen.go"},
		{"typed-recovery-request-body", "recovering/routes.go", "input.Body.", "Token", []string{"Token", "Password"}, []string{"Token", "Member"}, "/recovering/requests.go"},
		{"typed-recovery-requester", "recovering/link_requests.go", "requests.", "Request", []string{"Request", "Validate"}, []string{"Request", "identifier string", "error"}, "/auth/challenge/requests.go"},
		{"typed-recovery-request-assembly", "recovering/link_requests.go", "challenge.", "NewRequests", []string{"NewRequests", "ParseToken"}, []string{"Issuer", "RequestCallbacks", "Limiter"}, "/auth/challenge/requests.go"},
		{"typed-mfa-browser-completion", "multifactor/routes.go", "browser.", "CompleteMFA", []string{"CompleteMFA", "Login"}, []string{"SecondFactor", "IssueOptions", "Info", "Account"}, "/http/browser_sessions.go"},
		{"typed-mfa-confirmation-input", "multifactor/routes.go", "input.Body.", "EnrollmentID", []string{"EnrollmentID", "Code", "Credentials"}, []string{"EnrollmentID", "Account"}, "/multifactor/requests.go"},
		{"typed-mfa-session-completion", "multifactor/completion.go", "sessions.", "CompleteMFA", []string{"CompleteMFA", "Issue"}, []string{"SecondFactor", "IssueOptions", "Issued", "Account"}, "/auth/session/completion.go"},
		{"typed-mfa-token-completion", "multifactor/completion.go", "tokens.", "CompleteMFA", []string{"CompleteMFA", "Refresh"}, []string{"SecondFactor", "IssueOptions", "Issued", "Account"}, "/auth/token/completion.go"},
		{"typed-mfa-verifier", "multifactor/completion.go", "factors.", "Verifier", []string{"Verifier", "Confirm"}, []string{"Response", "SecondFactor", "Account"}, "/auth/mfa/completion.go"},
		{"typed-mfa-confirmation", "multifactor/account.go", "factors.", "Confirm", []string{"Enroll", "Confirm", "Disable", "RegenerateRecovery"}, []string{"PasswordResult", "EnrollmentID", "TOTPCode", "Account"}, "/auth/mfa/enrollment.go"},
		{"typed-mfa-recovery-management", "multifactor/account.go", "factors.", "RegenerateRecovery", []string{"RegenerateRecovery", "Reencrypt", "Prune"}, []string{"PasswordResult", "Response", "RecoveryCodes", "Account"}, "/auth/mfa/management.go"},
		{"typed-factor-encryption", "multifactor/primitives.go", "keys.", "Encrypt", []string{"Encrypt", "Decrypt", "Reencrypt"}, []string{"Context", "Ciphertext", "secret.String"}, "/encryption/keyring.go"},
		{"typed-password-model-recheck", "recovering/credentials.go", "provider.", "RecheckPassword", []string{"RecheckPassword", "CheckModel"}, []string{"Tx", "PasswordResult", "Member"}, "/auth/password_recheck.go"},
		{"typed-credential-revocation", "recovering/credentials.go", "revocations.", "Invalidate", []string{"Invalidate", "Validate"}, []string{"Context", "Tx", "Member"}, "/auth/revocation.go"},
		{"session-revocation-contribution", "recovering/credentials.go", "sessions.", "Revocation", []string{"Revocation", "RevokeAllIn"}, []string{"Revocation", "Member", "error"}, "/auth/session/revocation.go"},
		{"proof-preserving-scope-narrowing", "recovering/credentials.go", "proof.", "WithAccessScopes", []string{"WithAccessScopes", "HasIssuanceCheck"}, []string{"Proof", "Member", "AccessScopes"}, "/auth/proof_issuance.go"},
		{"typed-password-reset", "recovering/recovery.go", "reset.", "Complete", []string{"Issue", "Complete", "Revoke", "Prune"}, []string{"Member", "Token", "Plaintext"}, "/auth/passwordreset/reset.go"},
		{"typed-email-verification", "recovering/recovery.go", "verification.", "Complete", []string{"Issue", "Complete", "Revoke", "Prune"}, []string{"Member", "Token"}, "/auth/emailverification/verification.go"},
		{"password-lockout-binding", "passwords/lockout.go", "login.", "WithLockout", []string{"WithLockout", "Authenticate"}, []string{"Throttle", "string", "Account"}, "/auth/password_lockout.go"},
		{"typed-lockout-declaration", "passwords/lockout.go", "PasswordAttempts.", "Bind", []string{"Bind", "Policy", "Name"}, []string{"Store", "Throttle", "string"}, "/auth/lockout/declaration.go"},
		{"typed-lockout-reset", "passwords/lockout.go", "throttle.", "Reset", []string{"Reset", "Run", "Policy"}, []string{"Context", "string", "bool", "error"}, "/auth/lockout/throttle.go"},
		{"password-model-login", "passwords/login.go", "login.", "Authenticate", []string{"Authenticate", "Validate"}, []string{"Plaintext", "PasswordResult", "Account", "string"}, "/auth/password_login.go"},
		{"password-typed-check", "passwords/passwords.go", "hasher.", "Check", []string{"Check", "Hash", "NeedsRehash"}, []string{"Plaintext", "Hash", "bool", "error"}, "/auth/password/hasher.go"},
		{"token-refresh-input-secret", "authenticating/token_routes.go", "request.RefreshToken.", "Secret", []string{"Secret"}, []string{"secret.String"}, "/internal/authtransport/refresh_request.go"},
		{"token-model-owned-issue", "authenticating/tokens.go", "tokens.", "Issue", []string{"Issue", "Refresh", "RevokeID", "Guard"}, []string{"Proof", "User", "IssueOptions", "Issued"}, "/auth/token/tokens.go"},
		{"token-model-owned-refresh", "authenticating/tokens.go", "tokens.", "Refresh", []string{"Refresh", "List", "RevokeAll", "Touch"}, []string{"Context", "secret.String", "Issued", "User"}, "/auth/token/tokens.go"},
		{"auth-model-scope-check", "authenticating/access_scopes.go", "guard.", "RequireScopes", []string{"RequireScopes", "Require", "Optional"}, []string{"User", "AccessScopes", "error"}, "/auth/access_scopes.go"},
		{"auth-http-scope-contract", "authenticating/access_scopes.go", "endpoint.", "WithScopes", []string{"WithScopes", "Handle", "Description"}, []string{"User", "AccessScopes", "AuthenticatedEndpoint"}, "/http/authenticated_endpoint.go"},
		{"auth-immutable-scope-names", "authenticating/access_scopes.go", "grants.", "Names", []string{"Names", "Contains", "ContainsAll"}, []string{"AccessScopeName"}, "/auth/access_scopes.go"},
		{"browser-typed-login", "authenticating/browser.go", "web.", "Login", []string{"Login", "Rotate", "Logout", "Authentication"}, []string{"Proof", "User", "Info", "error"}, "/http/browser_sessions.go"},
		{"browser-typed-rotation", "authenticating/browser.go", "web.", "Rotate", []string{"Rotate", "Guard", "Middleware"}, []string{"Context", "User", "Info"}, "/http/browser_sessions.go"},
		{"session-typed-issue", "authenticating/sessions.go", "sessions.", "Issue", []string{"Issue", "List", "RevokeID", "Guard"}, []string{"Proof", "User", "Issued", "error"}, "/auth/session/sessions.go"},
		{"session-typed-revoke", "authenticating/sessions.go", "sessions.", "RevokeID", []string{"RevokeID", "RevokeAll", "Rotate"}, []string{"Reference", "User", "ID", "bool"}, "/auth/session/sessions.go"},
		{"session-stored-subject", "authenticating/sessions.go", "info.", "Subject", []string{"Subject", "ID", "ExpiresAt"}, []string{"Reference", "User"}, "/auth/session/info.go"},
		{"auth-current-model", "authenticating/accounts.go", "guard.", "Require", []string{"Require", "Optional", "Source"}, []string{"User", "Context", "error"}, "/auth/guard.go"},
		{"auth-typed-policy", "authenticating/accounts.go", "ReadOrder.", "Authorize", []string{"Authorize", "Allows", "Registration"}, []string{"User", "Order", "error"}, "/auth/policy.go"},
		{"auth-stored-reference", "authenticating/accounts.go", "provider.", "Parse", []string{"Parse", "ModelName", "Validate"}, []string{"Reference", "User", "Identity", "error"}, "/auth/provider.go"},
		{"auth-http-handler", "authenticating/accounts.go", "endpoint.", "Handle", []string{"Handle", "Description", "Validate"}, []string{"User", "Context", "Input", "NoContent"}, "/http/authenticated_endpoint.go"},

		{"redis-raw-key-resolution", "rediscommands/commands.go", "keys.", "For", []string{"For", "Exists", "Expire", "DeleteMany"}, []string{"Group", "Key", "Context", "error"}, "/redis/raw/key.go"},
		{"redis-raw-command-result", "rediscommands/commands.go", "command.", "Run", []string{"Run", "Arg", "Key"}, []string{"string", "Store", "Context", "error"}, "/redis/raw/store.go"},
		{"redis-raw-pipeline-run", "rediscommands/commands.go", "pipeline.", "Run", []string{"Run"}, []string{"Store", "Context", "error"}, "/redis/raw/pipeline.go"},
		{"redis-raw-pipeline-result", "rediscommands/commands.go", "count.", "Value", []string{"Value"}, []string{"int64", "error"}, "/redis/raw/pipeline.go"},
		{"redis-raw-script-result", "rediscommands/commands.go", "script.", "Run", []string{"Run", "Arg", "Key"}, []string{"int64", "Store", "error"}, "/redis/raw/store.go"},
		{"redis-data-adapter-export", "rediscommands/commands.go", "profiles.", "AdapterKey", []string{"AdapterKey", "Get", "Set"}, []string{"Member", "Key", "Context", "error"}, "/redis/data/entry.go"},

		{"redis-data-hash-set", "redisdata/members.go", "profiles.", "Set", []string{"Set", "Get", "DeleteField", "Count", "Expire"}, []string{"Member", "ProfileField", "Profile", "bool", "error"}, "/redis/data/hash.go"},
		{"redis-data-hash-get", "redisdata/members.go", "profiles.", "Get", []string{"Set", "Get", "DeleteMany"}, []string{"Member", "ProfileField", "Profile", "bool", "error"}, "/redis/data/hash.go"},
		{"redis-data-set-add", "redisdata/members.go", "groups.", "Add", []string{"Add", "Remove", "Contains", "Members", "Count"}, []string{"Member", "Group", "bool", "error"}, "/redis/data/set.go"},
		{"redis-data-set-members", "redisdata/members.go", "groups.", "Members", []string{"Members", "Delete", "DeleteMany", "Expire"}, []string{"Member", "Group", "error"}, "/redis/data/set.go"},
		{"redis-data-entry-expiry", "redisdata/members.go", "groups.", "Expire", []string{"Exists", "Expire", "Count", "DeleteMany"}, []string{"Member", "TTL", "bool", "error"}, "/redis/data/entry.go"},

		{"cache-entry-exists", "caching/entries.go", "profiles.", "Exists", []string{"Exists", "Expire", "ForgetMany", "Get"}, []string{"Member", "bool", "Context", "error"}, "/cache/entry.go"},
		{"cache-entry-expiry", "caching/entries.go", "profiles.", "Expire", []string{"Exists", "Expire", "ForgetMany"}, []string{"Member", "TTL", "bool", "error"}, "/cache/entry.go"},
		{"cache-entry-batch", "caching/entries.go", "profiles.", "ForgetMany", []string{"Exists", "Expire", "ForgetMany"}, []string{"Member", "uint64", "error"}, "/cache/entry.go"},
		{"cache-namespace-invalidation", "caching/invalidate.go", "store.", "Invalidate", []string{"Invalidate", "InvalidateTags", "Namespace"}, []string{"Context", "error", "namespace"}, "/cache/invalidate.go"},
		{"typed-pubsub-publication", "messaging/members.go", "topic.", "Publish", []string{"Publish", "Subscribe", "Name", "Version"}, []string{"Member", "MemberChange", "uint64", "error"}, "/pubsub/topic.go"},
		{"typed-pubsub-receive", "messaging/members.go", "subscription.", "Receive", []string{"Receive", "Close", "Done", "Err"}, []string{"MemberChange", "Context", "error"}, "/pubsub/subscription.go"},
		{"typed-rate-limit-consumption", "limiting/members.go", "limiter.", "Take", []string{"Take", "Allow", "TakeWith", "Limit", "Name"}, []string{"Member", "uint32", "Decision", "error"}, "/ratelimit/limiter.go"},
		{"typed-rate-limit-binding", "limiting/members.go", "MemberRequests.", "Bind", []string{"Bind", "Validate", "Limit", "Name"}, []string{"Member", "Store", "Limiter", "error"}, "/ratelimit/declaration.go"},
		{"distributed-cache-construction", "caching/distributed.go", "cache.", "NewCoordinatedStore", []string{"NewStore", "NewCoordinatedStore", "Define"}, []string{"Manager", "Config", "CoordinationConfig", "Store", "error"}, "/cache/coordination.go"},
		{"typed-lease-scope", "coordination/members.go", "locks.", "With", []string{"With", "Acquire", "TryAcquire"}, []string{"Member", "Context", "Duration", "bool", "error"}, "/lease/acquire.go"},
		{"typed-lease-guard", "coordination/members.go", "locks.", "TryAcquire", []string{"With", "Acquire", "TryAcquire"}, []string{"Member", "Guard", "Duration", "bool", "error"}, "/lease/acquire.go"},
		{"typed-lease-binding", "coordination/members.go", "MemberRefresh.", "Bind", []string{"Bind", "Validate", "Name"}, []string{"Member", "Manager", "Leases"}, "/lease/declaration.go"},
		{"redis-connection-lifecycle", "caching/redis.go", "client.", "Ping", []string{"Start", "Close", "Ping", "Stats", "Done"}, []string{"Context", "error"}, "/redis/client.go"},
		{"typed-cache-tag-key", "caching/tags.go", "tags.", "For", []string{"For", "Invalidate"}, []string{"Member", "Tag"}, "/cache/tag.go"},
		{"typed-cache-tag-view", "caching/tags.go", "profiles.", "WithTags", []string{"WithTags", "Remember", "Get", "Put"}, []string{"Profile", "Member", "Tag", "error"}, "/cache/tag_view.go"},
		{"typed-cache-counter", "caching/counters.go", "counts.", "Increment", []string{"Get", "Put", "Add", "Forget", "Increment", "Decrement"}, []string{"Member", "int64", "Context", "TTL", "error"}, "/cache/counter.go"},
		{"typed-cache-counter-binding", "caching/counters.go", "ProfileViews.", "Bind", []string{"Bind", "Name", "Validate"}, []string{"Counter", "Member", "Store"}, "/cache/counter_declaration.go"},
		{"typed-cache-remember", "caching/profiles.go", "profiles.", "Remember", []string{"Get", "Remember", "Put"}, []string{"Profile", "Member", "Context", "TTL", "error"}, "/cache/remember.go"},
		{"typed-cache-read", "caching/profiles.go", "profiles.", "Get", []string{"Get", "Put", "Add", "Forget"}, []string{"Profile", "Member", "bool", "error"}, "/cache/cache.go"},
		{"typed-cache-binding", "caching/profiles.go", "ProfileEntries.", "Bind", []string{"Bind", "Validate", "Name"}, []string{"Profile", "Member", "Store"}, "/cache/declaration.go"},
		{"typed-http-compression", "httpcompression/compression.go", "foundryhttp.", "Compression", []string{"Compression", "DefaultCompressionConfig", "GzipCompression", "BrotliCompression"}, []string{"CompressionConfig", "Middleware"}, "/http/compression.go"},
		{"typed-http-compression-encoder", "httpcompression/compression.go", "encoder.", "Encoding", []string{"Encoding", "Validate"}, []string{"ContentEncoding"}, "/http/compression_config.go"},
		{"typed-etag-config", "httpetags/etags.go", "foundryhttp.", "DefaultETagConfig", []string{"DefaultETagConfig", "ETags"}, []string{"ETagConfig"}, "/http/etag_config.go"},
		{"typed-etag-middleware", "httpetags/etags.go", "foundryhttp.", "ETags", []string{"ETags", "ApplyMiddleware"}, []string{"ETagConfig", "Middleware"}, "/http/etag.go"},
		{"typed-asset-config", "httpassets/assets.go", "foundryhttp.", "DefaultAssetsConfig", []string{"DefaultAssetsConfig", "AssetsModule", "DirectoryAssets", "FilesystemAssets"}, []string{"AssetSource", "AssetsConfig"}, "/http/assets_config.go"},
		{"typed-asset-mount", "httpassets/assets.go", "mount.", "Register", []string{"Register", "URL", "WithMiddleware", "Validate"}, []string{"RouteRegistration"}, "/http/assets_route.go"},
		{"typed-spa-fallback", "httpassets/assets.go", "router.", "WithSPA", []string{"WithSPA", "Routes", "Endpoints"}, []string{"RouteID", "Assets", "SPAConfig", "Router"}, "/http/spa.go"},
		{"typed-stream-response", "httpstreams/streams.go", "foundryhttp.", "StreamResponse", []string{"StreamResponse", "StreamFrom"}, []string{"MediaType", "Response[", "Stream"}, "/http/stream_response.go"},
		{"typed-stream-value", "httpstreams/streams.go", "stream.", "WithName", []string{"WithName", "WithMediaType", "WithDisposition"}, []string{"string", "Stream"}, "/http/stream.go"},
		{"typed-stream-handler", "httpstreams/streams.go", "Export.", "Handle", []string{"Handle", "Description", "Validate"}, []string{"UserPath", "Stream"}, "/http/endpoint.go"},
		{"typed-download-response", "httpdownloads/downloads.go", "foundryhttp.", "DownloadResponse", []string{"DownloadResponse", "LocalDownload", "DownloadFrom"}, []string{"MediaType", "Response[", "Download"}, "/http/download_response.go"},
		{"typed-download-value", "httpdownloads/downloads.go", "download.", "WithName", []string{"WithName", "WithMediaType", "WithDisposition", "WithEntityTag"}, []string{"string", "Download"}, "/http/download.go"},
		{"typed-download-handler", "httpdownloads/downloads.go", "Show.", "Handle", []string{"Handle", "Description", "Validate"}, []string{"UserPath", "Download"}, "/http/endpoint.go"},

		{"typed-multipart-descriptor", "httpuploads/forms.go", "ProfileInputDescriptor().", "WithTempDirectory", []string{"WithTempDirectory", "Validate", "Description"}, []string{"Multipart", "ProfileInput"}, "/http/multipart.go"},
		{"typed-multipart-validation-field", "httpuploads/forms.go", "fields.Attachment.", "Rules", []string{"Rules", "WithLabel"}, []string{"ProfileInput", "File"}, "/validation/composition.go"},
		{"typed-upload-reader", "httpuploads/forms.go", "file.", "Open", []string{"Open", "Name", "Size", "ContentType", "Extension"}, []string{"Context", "ReadSeekCloser", "error"}, "/internal/upload/file.go"},
		{"typed-http-model-binding", "httpmodels/bindings.go", "modelbinding.", "ByKey", []string{"ByKey", "Define", "Bind"}, []string{"Resolver", "KeyQuery"}, "/http/modelbinding/key.go"},
		{"typed-http-model-binding-handler", "httpmodels/bindings.go", "bound.", "Handle", []string{"Handle", "Description", "Validate"}, []string{"User", "UserResponse"}, "/http/modelbinding/endpoint.go"},
		{"typed-http-cursor", "httppagination/cursor.go", "pagination.", "DefineCursor", []string{"DefineCursor", "MapCursorPage", "CursorJSON"}, []string{"CursorEndpoint", "CursorConfig"}, "/http/pagination/cursor_endpoint.go"},
		{"typed-http-cursor-handler", "httppagination/cursor.go", "CursorList.", "Handle", []string{"Handle", "URL", "WithFiltersValidation"}, []string{"Member", "MemberResponse"}, "/http/pagination/endpoint.go"},
		{"typed-authenticated-pagination", "httppagination/authenticated.go", "return pagination.", "Authenticated", []string{"Authenticated", "DefineNumbered", "DefineCursor"}, []string{"GuardBinding", "authenticatedPageEndpoint"}, "/http/pagination/authenticated_endpoint.go"},
		{"typed-authenticated-pagination-handler", "httppagination/authenticated.go", "endpoint.", "Handle", []string{"Handle", "URL", "Description", "WithAuthorization", "WithPermissions", "WithScopes"}, []string{"Actor", "MemberResponse"}, "/http/pagination/authenticated_endpoint.go"},
		{"typed-http-pagination", "httppagination/pagination.go", "pagination.", "DefineNumbered", []string{"DefineNumbered", "DefineSimple", "MapPage"}, []string{"Route", "Query", "JSON", "NumberedEndpoint"}, "/http/pagination/endpoint.go"},
		{"typed-http-pagination-handler", "httppagination/pagination.go", "List.", "Handle", []string{"Handle", "URL", "Description", "WithFiltersValidation"}, []string{"MemberFilters", "MemberResponse"}, "/http/pagination/endpoint.go"},
		{"typed-query-default-composition", "httpquery/composition.go", "foundryhttp.", "DefaultQueryParam", []string{"DefaultQueryParam", "EmbedQuery", "MergeQueries"}, []string{"QueryCodec", "QueryParameter"}, "/http/query_default.go"},
		{"typed-http-error-declaration", "httpendpoints/errors.go", "foundryhttp.", "DefineError", []string{"DefineError", "DefineEndpoint"}, []string{"ErrorCode", "ErrorDeclaration"}, "/http/error_declaration.go"},
		{"typed-native-json-contract", "httpdto/stream.go", "StreamLabel{}.", "JSONContract", []string{"JSONContract", "MarshalJSONTo"}, []string{"JSON", "StreamLabel"}, "/httpdto/stream.go"},
		{"typed-url-source-identity", "httpquery/source_identity.go", "foundryhttp.", "URLType", []string{"URLType", "DescribeURL", "IntegerPath"}, []string{"TypeID", "PathCodec"}, "/http/url_type.go"},
		{"typed-json-key-factory", "httpdto/map_keys.go", "contract.", "DefineJSONKey", []string{"DefineJSONKey", "JSONMapType", "StringJSONKey"}, []string{"Scalar", "JSONKey"}, "/contract/json_key.go"},
		{"typed-json-key-description", "httpdto/map_keys.go", "WarehouseKeys.", "Description", []string{"Description", "Validate"}, []string{"JSONKeyInfo", "error"}, "/contract/json_key.go"},
		{"typed-custom-json-scalar", "httpquery/json_contract.go", "contract.", "ScalarJSON", []string{"ScalarJSON", "DefineJSONValue", "JSONType"}, []string{"Scalar", "JSON"}, "/contract/json_scalar.go"},
		{"typed-custom-json-method", "httpdto/custom_code.go", "httpquery.TrackingCode(\"\").", "JSONContract", []string{"JSONContract", "MarshalText"}, []string{"JSON", "TrackingCode"}, "/httpquery/json_contract.go"},
		{"typed-url-scalar", "httpquery/scalar_metadata.go", "foundryhttp.", "DescribeURL", []string{"DescribeURL", "EnumQuery", "ModelIDQuery"}, []string{"PathCodec", "Scalar"}, "/http/url_scalar.go"},
		{"typed-scalar-description", "httpquery/scalar_metadata.go", "TrackingScalar.", "Description", []string{"Description", "Validate"}, []string{"Type", "error"}, "/contract/scalar_descriptor.go"},
		{"typed-http-signed-endpoint", "httpsigned/links.go", "links.Preview.", "URL", []string{"URL", "Handle", "Description"}, []string{"Origin", "UserPath", "SearchInput", "time.Time"}, "/http/signed_endpoint.go"},
		{"typed-http-signed-route", "httpsigned/links.go", "links.Asset.", "URL", []string{"URL", "HandleRaw", "Validate"}, []string{"Origin", "AssetPath", "time.Time"}, "/http/signed_route.go"},
		{"typed-http-cookie", "httpcookies/cookies.go", "SelectedUser.", "Signed", []string{"Signed", "Read", "Set", "Clear"}, []string{"CookieSigner", "SignedCookie["}, "/http/signed_cookie.go"},
		{"typed-http-signed-cookie", "httpcookies/cookies.go", "selected.", "Read", []string{"Read", "Set", "Clear"}, []string{"Optional[", "Request", "error"}, "/http/signed_cookie.go"},
		{"typed-http-public-url", "httpsecurity/public_urls_test.go", "foundryhttp.", "PublicURL", []string{"PublicURL", "PublicOrigin", "AbsoluteURL"}, []string{"context.Context", "string", "error"}, "/http/public_url.go"},
		{"typed-http-proxy-origin", "httpsecurity/public_urls.go", "foundryhttp.", "XForwardedOriginHeaders", []string{"XForwardedOriginHeaders", "ForwardedOriginHeader", "ProxySchemeHeader"}, []string{"ProxyOriginHeader"}, "/http/proxy_origin.go"},
		{"typed-http-csp", "httpsecurity/csp.go", "foundryhttp.", "ContentSecurityPolicy", []string{"ContentSecurityPolicy", "CSPNonceSource", "CSPPolicy"}, []string{"CSPConfig", "Middleware"}, "/http/csp.go"},
		{"typed-http-csp-nonce", "httpsecurity/csp.go", "nonce.", "String", []string{"String"}, []string{"CSPNonce", "string"}, "/http/csp.go"},
		{"generated-validation-field", "httpvalidation/validation.go", "httpdto.UpdateUserValidationFields().", "Email", []string{"Email", "Nickname", "State"}, []string{"Field[", "UpdateUser", "Optional[string]"}, "/httpdto/update_user_foundry.gen.go"},
		{"typed-validation-rule-binding", "httpvalidation/validation.go", `httpdto.UpdateUserValidationFields().Email.WithLabel("Email address").`, "Rules", []string{"Rules", "Validate"}, []string{"Rule[", "UpdateUser", "Optional[string]"}, "/validation/composition.go"},
		{"typed-http-validation", "httpvalidation/validation.go", "httpendpoints.Update.", "WithBodyValidation", []string{"WithBodyValidation", "WithQueryValidation", "WithValidation"}, []string{"Rule[", "UpdateUser"}, "/http/endpoint_validation.go"},
		{"typed-validation-membership", "validationrules/rules.go", "validation.", "OneOf", []string{"OneOf"}, []string{"Scalar", "Rule["}, "/validation/membership.go"},
		{"conditional-validation-field", "validationrules/rules.go", "fields.Company.", "Rules", []string{"Rules"}, []string{"Registration", "Optional[string]"}, "/validation/composition.go"},
		{"typed-validation-prefix", "validationrules/preferences.go", "validation.", "StartsWith", []string{"StartsWith"}, []string{"~string", "Rule["}, "/validation/text_format.go"},
		{"typed-temporal-comparison", "validationrules/window.go", "validation.", "BeforeField", []string{"BeforeField"}, []string{"Temporal", "Field[", "Rule["}, "/validation/temporal.go"},
		{"typed-required-presence", "validationrules/presence.go", "validation.", "Required", []string{"Required"}, []string{"Optional[", "Rule["}, "/validation/required.go"},
		{"typed-required-nullable", "validationrules/presence.go", "validation.", "RequiredNullable", []string{"RequiredNullable"}, []string{"Optional[", "Nullable[", "Rule["}, "/validation/required.go"},
		{"validation-parallel", "validationrules/expanded.go", "validation.", "Parallel", []string{"Parallel", "ParallelLimit", "PasswordValue", "ContainsItems"}, []string{"Rule[", "rules"}, "/validation/parallel.go"},
		{"validation-batch-model", "modelvalidation/rules.go", "databasevalidation.", "ExistsAll", []string{"ExistsAll", "Unique", "Exists"}, []string{"ModelQuerySource[", "KeyField[", "Rule["}, "/validation/database/lookup.go"},
		{"validation-password-value", "validationrules/expanded.go", "validation.", "PasswordValue", []string{"PasswordValue", "DefaultPasswordOptions"}, []string{"PasswordOptions", "Rule["}, "/validation/password.go"},
		{"typed-database-validation", "modelvalidation/rules.go", "databasevalidation.", "Unique", []string{"Unique", "Exists"}, []string{"ModelQuerySource[", "KeyField[", "Rule["}, "/validation/database/lookup.go"},
		{"typed-http-middleware", "httpmiddleware/router.go", "foundryhttp.", "DefineMiddleware", []string{"DefineMiddleware", "ApplyMiddleware"}, []string{"MiddlewareID", "Handler", "Middleware"}, "/http/middleware.go"},
		{"typed-http-cors", "httpcors/router.go", "foundryhttp.", "CORS", []string{"CORS", "CORSConfig"}, []string{"CORSConfig", "Middleware"}, "/http/cors.go"},
		{"typed-http-proxy", "httpproxy/router.go", "foundryhttp.", "TrustedProxy", []string{"TrustedProxy", "TrustedProxyConfig"}, []string{"TrustedProxyConfig", "Middleware"}, "/http/trusted_proxy.go"},
		{"typed-http-proxy-header", "httpproxy/router.go", "foundryhttp.", "XForwardedForHeader", []string{"XForwardedForHeader", "ForwardedHeader", "ClientIPHeader"}, []string{"ProxyHeader"}, "/http/proxy_config.go"},
		{"typed-http-security", "httpsecurity/router.go", "foundryhttp.", "SecurityHeaders", []string{"SecurityHeaders", "DefaultSecurityHeadersConfig"}, []string{"SecurityHeadersConfig", "Middleware"}, "/http/security_headers.go"},
		{"typed-http-security-policy", "httpsecurity/router.go", "config.", "Frame", []string{"Frame", "Referrer", "HSTS"}, []string{"FramePolicy"}, "/http/security_headers_config.go"},
		{"typed-http-endpoint-handler", "httpendpoints/endpoints.go", "Update.", "Handle", []string{"Handle", "URL", "Description", "WithLimits"}, []string{"UserPath", "NearbyInput", "UpdateUser", "UserResponse"}, "/http/endpoint.go"},
		{"typed-http-endpoint-input", "httpendpoints/endpoints_test.go", "in.", "Path", []string{"Path", "Query", "Body"}, []string{"UserPath"}, "/http/endpoint.go"},
		{"typed-http-endpoint-url", "httpendpoints/endpoints_test.go", "httpendpoints.Update.", "URL", []string{"URL", "Handle", "Description"}, []string{"UserPath", "NearbyInput", "string", "error"}, "/http/endpoint.go"},
		{"generated-http-float-descriptor", "httpquery/floats_test.go", "httpquery.", "NearbyInputDescriptor", []string{"NearbyInputDescriptor", "PositionPathDescriptor"}, []string{"Query[", "NearbyInput"}, "/httpquery/nearby_input_foundry.gen.go"},
		{"generated-http-float-field", "httpquery/floats_test.go", "input.", "Latitude", []string{"Latitude", "Maximum", "Distances"}, []string{"httpquery.Latitude"}, "/httpquery/floats.go"},
		{"generated-http-float-path", "httpquery/floats_test.go", "httpquery.PositionPathDescriptor().", "URL", []string{"URL", "Pattern"}, []string{"PositionPath", "string", "error"}, "/http/path.go"},
		{"generated-http-query-descriptor", "httpquery/query_test.go", "httpquery.", "SearchInputDescriptor", []string{"SearchInputDescriptor", "Parameters"}, []string{"Query[", "SearchInput"}, "/httpquery/search_input_foundry.gen.go"},
		{"generated-http-query-decode", "httpquery/query_test.go", "parameters.", "Decode", []string{"Decode", "Encode", "Parameters", "Validate"}, []string{"SearchInput", "QueryLimits", "error"}, "/http/query.go"},
		{"generated-http-query-enum-slice", "httpquery/query_test.go", "input.", "Statuses", []string{"User", "Search", "Statuses"}, []string{"[]models.Status"}, "/httpquery/query.go"},
		{"generated-http-path-descriptor", "httpkernel/generated_routes_test.go", "httpkernel.", "UserPathDescriptor", []string{"UserPathDescriptor", "UserFeedPathDescriptor", "AssetPathDescriptor"}, []string{"Path[", "UserPath"}, "/httpkernel/user_path_foundry.gen.go"},
		{"generated-http-dto-encode", "httpdto/encoding_test.go", "rows.", "Encode", []string{"Encode", "Decode", "Description"}, []string{"[]", "UserResponse", "JSONLimits", "error"}, "/contract/json_encoding.go"},
		{"generated-http-dto-slice", "httpdto/encoding_test.go", "contract.", "Slice", []string{"Slice", "Nullable"}, []string{"JSON[", "[]T"}, "/contract/json_composition.go"},
		{"generated-http-dto-nullable", "httpdto/encoding_test.go", "optional.", "Decode", []string{"Decode", "Encode", "Description"}, []string{"Nullable[", "UserResponse", "error"}, "/contract/json.go"},
		{"generated-http-dto-descriptor", "httpdto/dto_test.go", "httpdto.", "UserResponseJSON", []string{"UpdateUserJSON", "UserResponseJSON", "OrderResponseJSON"}, []string{"JSON[", "UserResponse"}, "/httpdto/user_response_foundry.gen.go"},
		{"generated-http-dto-decode", "httpdto/dto_test.go", "declaration.", "Decode", []string{"Decode", "Validate", "Description"}, []string{"UpdateUser", "JSONLimits", "error"}, "/contract/json.go"},
		{"generated-http-dto-id", "httpdto/dto_test.go", "response.", "ID", []string{"ID", "Email", "State"}, []string{"ID[", "User"}, "/httpdto/dto.go"},
		{"generated-http-dto-patch", "httpdto/dto_test.go", "input.", "State", []string{"Email", "Nickname", "State"}, []string{"Optional[", "Status"}, "/httpdto/dto.go"},
		{"generated-http-path-url", "httpkernel/generated_routes_test.go", "httpkernel.UserPathDescriptor().", "URL", []string{"URL", "Pattern"}, []string{"UserPath", "string", "error"}, "/http/path.go"},
		{"generated-http-path-id", "httpkernel/generated_routes_test.go", "input.", "User", []string{"User"}, []string{"ID[", "User"}, "/httpkernel/path_declarations.go"},
		{"generated-model-query", "model_query_postgres_test.go", "base.Where(fields.Age.Gte(30)).", "Find", []string{"All", "First", "Find", "Count", "Each", "Where", "Create", "Update", "Delete", "Paginate", "CursorPaginate", "With", "Load", "LoadMissing"}, []string{"Find", "ID[", "User", "Optional["}, "/models/user_foundry.gen.go"},
		{"generated-relation-set", "model_relations_postgres_test.go", "models.UserRelations().", "Introducer", []string{"Introducer", "Referrals", "Orders", "SingleOrder"}, []string{"Introducer", "OneRelation[", "User"}, "/models/user_foundry.gen.go"},
		{"generated-pivot-relation", "model_through_postgres_test.go", "models.UserRelations().", "Groups", []string{"Groups", "Friends"}, []string{"Groups", "ThroughRelation[", "User", "Group", "Membership"}, "/models/user_foundry.gen.go"},
		{"generated-extension-owner", "articles/cleanup_postgres_test.go", "articles.", "ArticleExtensionOwner", []string{"ArticleExtensionOwner", "ArticleExtensions", "ArticleExtensionDeclaration", "FoundryExtensions"}, []string{"ArticleExtensionOwner", "Owner[", "Article"}, "/articles/article_foundry.gen.go"},
		{"generated-extension-binding", "articles/cleanup_postgres_test.go", "articles.ArticleExtensions().", "From", []string{"From", "Title", "Summary", "Logo", "Galleries", "SEO"}, []string{"From", "Runtime", "ArticleExtensionSlots"}, "/articles/article_foundry.gen.go"},
		{"generated-extension-slot", "articles/cleanup_postgres_test.go", "x.", "Title", []string{"Title", "Summary", "Logo", "Galleries", "SEO", "From"}, []string{"Title", "TextSlot[", "Article"}, "/articles/article_foundry.gen.go"},
		{"extension-slot-read", "articles/loading_postgres_test.go", "full.Title.", "Resolve", []string{"Resolve", "ResolveRequest", "Exact", "Values", "IsLoaded"}, []string{"Resolve", "Optional[", "Resolved"}, "/translations/slot.go"},
		{"extension-slot-link", "articles/loading_postgres_test.go", "s.x.Logo.", "PublicURL", []string{"PublicURL", "URL", "TemporaryURL", "VariantURL", "Collection", "From"}, []string{"PublicURL", "Optional[", "string"}, "/attachments/slot.go"},
		{"extension-slot-write", "articles/writes_postgres_test.go", "x.Title.", "SaveIn", []string{"Save", "SaveIn", "Sync", "SyncIn", "ForgetIn", "ClearIn", "Rule", "Field"}, []string{"SaveIn", "Tx", "LocaleID"}, "/translations/slot.go"},
		{"extension-file-accepts", "articles/writes_postgres_test.go", "x.Logo.", "Accepts", []string{"Accepts", "ReplaceFile", "Replace", "Detach", "Clear", "PublicURL"}, []string{"Accepts", "FileSource", "bool"}, "/attachments/slot_write.go"},
		{"extension-file-prepare", "articles/writes_postgres_test.go", "x.Logo.", "PrepareFile", []string{"PrepareFile", "Prepare", "ReplaceIn", "ReplaceFile", "Discard"}, []string{"PrepareFile", "FileSource", "Prepared["}, "/attachments/slot_write.go"},
		{"extension-files-in-transaction", "articles/writes_postgres_test.go", "x.Galleries.", "AddIn", []string{"AddIn", "AddFile", "Add", "PrepareFile", "Reorder"}, []string{"AddIn", "Tx", "Prepared["}, "/attachments/slot_write.go"},
		{"lifecycle-deletion-observer", "articles/article_foundry.gen.go", "lifecycle.", "NewDeletionObserver", []string{"NewDeletionObserver", "NewObserver", "NewRetrievalObserver"}, []string{"NewDeletionObserver", "Observer["}, "/database/lifecycle/observers.go"},
		{"extension-owner-active-subjects", "articles/owner_postgres_test.go", "owner.", "ActiveSubjects", []string{"ActiveSubjects", "Active", "SubjectKey", "Subject", "Lock"}, []string{"ActiveSubjects", "map[string]bool", "[]string"}, "/extensions/owner.go"},
		{"extension-identity-cleanup", "articles/cleanup_postgres_test.go", "metadata.", "CleanupIdentity", []string{"CleanupIdentity", "Cleanup", "InspectOrphans", "PruneOrphans"}, []string{"CleanupIdentity", "OwnerName", "Identity"}, "/metadata/records.go"},
		{"extension-cleanup-job", "articles/durable_cleanup_postgres_test.go", "slots.", "DefineCleanupJob", []string{"DefineCleanupJob", "NewCleanup", "Declare", "ObserverName"}, []string{"DefineCleanupJob", "Name", "Policy", "CleanupJob"}, "/extensions/slots/cleanup_job.go"},
		{"extension-cleanup-queue", "articles/durable_cleanup_postgres_test.go", "runtime.", "CleanupQueue", []string{"CleanupQueue", "Store", "Metadata", "Translations", "Attachments"}, []string{"field CleanupQueue", "*slots.CleanupQueue"}, "/extensions/slots/slots.go"},
		{"generated-aggregate-set", "model_aggregates_postgres_test.go", "models.UserAggregates().", "OrderCount", []string{"OrderCount", "OrderTotal", "GroupPriority", "MeasurementAverage"}, []string{"OrderCount", "AggregateRelation[", "User", "int64"}, "/models/user_foundry.gen.go"},
		{"generated-projection", "projectionqueries/projections_postgres_test.go", "reports.", "SelectUserSummary", []string{"SelectUserSummary", "UserSummaryFields", "SelectBuyerTotals"}, []string{"SelectUserSummary", "ProjectionQuery[", "UserSummary", "ProjectionSource["}, "/reports/user_summary_foundry.gen.go"},
		{"typed-aggregate-condition", "projectionqueries/projections_postgres_test.go", "fields.TotalCents.Sum().", "Gt", []string{"Gt", "Eq", "IsNull", "IsNotNull", "Desc"}, []string{"Gt", "Decimal", "HavingPredicate[", "Order"}, "/database/query/aggregate_condition.go"},
		{"joined-model-fields", "joinqueries/joins_postgres_test.go", "referral.", "Email", []string{"Email", "ID", "Status", "Age"}, []string{"TextField[", "Left[", "User"}, "/models/user_foundry.gen.go"},
		{"outer-joined-model-fields", "joinqueries/joins_postgres_test.go", "sponsor.", "Email", []string{"Email", "ID", "Nickname", "Status"}, []string{"NullableTextField[", "Left[", "User"}, "/models/user_foundry.gen.go"},
		{"inferred-projection-builder", "joinqueries/joins_postgres_test.go", "selection.", "SelectID", []string{"SelectID", "SelectEmail", "SelectIntroducerID", "Query"}, []string{"Expression[", "Left[", "ID[", "User"}, "/reports/referral_row_foundry.gen.go"},
		{"derived-report-fields", "advancedqueries/subqueries_postgres_test.go", "fields.", "Total", []string{"Total", "BuyerID", "Orders", "Average"}, []string{"NullableExactField[", "Alias[", "BuyerTotals", "Decimal"}, "/reports/buyer_totals_foundry.gen.go"},
		{"outer-derived-report-fields", "advancedqueries/subqueries_postgres_test.go", "stats.", "Orders", []string{"Total", "Orders", "Average"}, []string{"NullableExactField[", "Left[", "BuyerTotals", "int64"}, "/reports/buyer_totals_foundry.gen.go"},
		{"typed-membership-query", "advancedqueries/subqueries_postgres_test.go", "u.ID.", "InQuery", []string{"InQuery", "InNullableQuery", "Eq"}, []string{"ValueQuerySource[", "ID[", "User"}, "/database/query/subquery.go"},
		{"correlated-model-fields", "correlations/correlations_postgres_test.go", "order.", "BuyerID", []string{"BuyerID", "TotalCents", "ID"}, []string{"ScalarField[", "Correlation[", "User", "Order"}, "/models/order_foundry.gen.go"},
		{"correlated-value-boundary", "correlations/correlations_postgres_test.go", "ids.", "Exists", []string{"Where", "Having", "GroupBy", "OrderBy", "Exists"}, []string{"Predicate[", "User"}, "/database/query/correlated_value.go"},
		{"typed-column-range", "correlations/correlations_postgres_test.go", "child.TotalCents.", "GtColumn", []string{"GtColumn", "GteColumn", "LtColumn", "EqColumn"}, []string{"OrderedKeyField[", "Correlation[", "int64"}, "/database/query/column_comparison.go"},
		{"generated-relationship-filter", "relationshipfilters/filters_postgres_test.go", "models.QueryUsers().", "WhereHas", []string{"WhereHas", "WhereDoesntHave", "Where", "All", "RequireFirst"}, []string{"ExistenceRelation[", "User", "UserQuery"}, "/models/user_foundry.gen.go"},
		{"relationship-existence-predicate", "relationshipfilters/filters_postgres_test.go", "orders.", "Exists", []string{"Where", "OrderBy", "With", "Exists"}, []string{"Predicate[", "User"}, "/database/query/relation_predicate.go"},
		{"relationship-filter-model-slice", "relationshipfilters/filters_postgres_test.go", "builder.OrderBy(u.Email.Asc()).", "All", []string{"WhereHas", "WhereDoesntHave", "All", "First", "RequireFind"}, []string{"All", "[]", "User", "error"}, "/database/query/execution.go"},
		{"typed-cte-materialization", "ctes/ctes_postgres_test.go", "query.CTE(\"eligible_users\", models.QueryUsers().Where(u.Age.Gte(21))).", "Materialized", []string{"Materialized", "NotMaterialized"}, []string{"CommonTable[", "User", "Materialized"}, "/database/query/cte.go"},
		{"typed-cte-fields", "ctes/ctes_postgres_test.go", "df.", "Status", []string{"ID", "Email", "Status", "Nickname"}, []string{"ScalarField[", "Alias[", "User", "Status"}, "/models/user_foundry.gen.go"},
		{"typed-model-union", "setqueries/sets_postgres_test.go", "models.QueryUsers().Where(u.Age.Lte(21)).", "Union", []string{"Union", "UnionAll", "Intersect", "Except"}, []string{"RecordQuerySource[", "User", "SetQuery["}, "/database/query/set_operations.go"},
		{"combined-record-fields", "setqueries/sets_postgres_test.go", "rf.", "Total", []string{"BuyerID", "Total", "Orders", "Average"}, []string{"NullableExactField[", "Set[", "BuyerTotals", "Decimal"}, "/reports/buyer_totals_foundry.gen.go"},
		{"combined-value-expression", "setqueries/sets_postgres_test.go", "combined.", "Value", []string{"Value", "All", "First", "Union", "OrderBy"}, []string{"Expression[", "Set[", "int"}, "/database/query/value_set.go"},
		{"joined-complete-model-slice", "recordqueries/records_postgres_test.go", "builder.", "All", []string{"All", "First", "RequireFirst", "Where", "OrderBy", "Union"}, []string{"All", "[]", "User", "error"}, "/database/query/projection.go"},
		{"recursive-cte-construction", "recursivequeries/recursive_postgres_test.go", "query.", "RecursiveCTE", []string{"RecursiveCTE", "RecursiveAllCTE", "SelectRecord"}, []string{"RecursiveSelf[", "RecordQuerySource[", "CommonTable["}, "/database/query/recursive_cte.go"},
		{"recursive-working-fields", "recursivequeries/recursive_postgres_test.go", "p.", "ID", []string{"ID", "Email", "IntroducerID"}, []string{"ScalarField[", "Alias[", "ID[", "User"}, "/models/user_foundry.gen.go"},
		{"distinct-model-selection", "distinctqueries/distinct_postgres_test.go", "models.QueryUsers().", "DistinctOn", []string{"Distinct", "DistinctOn", "Where"}, []string{"Group[", "ProjectionQuery[", "User"}, "/database/query/distinct.go"},
		{"window-scope-builder", "windowqueries/windows_postgres_test.go", "query.WindowFor(base).", "PartitionBy", []string{"PartitionBy", "OrderBy", "RowsBetween", "GroupsBetween", "RangeBetween"}, []string{"Group[", "Window[", "Order"}, "/database/query/window.go"},
		{"typed-window-rank", "windowqueries/windows_postgres_test.go", "query.", "RowNumber", []string{"RowNumber", "Rank", "DenseRank", "Lag", "Lead"}, []string{"Window[", "Expression[", "int64"}, "/database/query/window_function.go"},
		{"typed-simple-model-page", "pagequeries/pages_postgres_test.go", "base.", "SimplePaginate", []string{"SimplePaginate", "Paginate", "CursorPaginate"}, []string{"SimplePage[", "User", "error"}, "/database/query/pagination.go"},
		{"typed-projection-page", "pagequeries/pages_postgres_test.go", "q.", "Paginate", []string{"SimplePaginate", "Paginate", "All"}, []string{"Page[", "UserSummary", "error"}, "/database/query/result_pagination.go"},
		{"typed-result-cursor-page", "cursorqueries/cursors_postgres_test.go", "ordered.", "Paginate", []string{"Paginate", "UniqueBy", "Where", "Compile"}, []string{"CursorRequest[", "UserSummary", "CursorPage["}, "/database/query/result_cursor.go"},
		{"typed-aggregate-filter", "filterqueries/filters_postgres_test.go", "o.TotalCents.Sum().", "Filter", []string{"Filter", "Gt", "IsNull", "Over"}, []string{"Predicate[", "Order", "NullableOrderedAggregate["}, "/database/query/aggregate_filter.go"},
		{"typed-row-expression", "expressionqueries/expressions_postgres_test.go", "display.", "Eq", []string{"Eq", "Ne", "In", "Value", "Param"}, []string{"string", "Predicate[", "User"}, "/database/query/row_expression.go"},
		{"typed-parameter", "expressionqueries/expressions_postgres_test.go", "u.Age.", "Param", []string{"Param", "Eq", "Gt", "Value"}, []string{"int", "RowExpression[", "User"}, "/database/query/row_expression.go"},
		{"typed-case-branches", "expressionqueries/expressions_test.go", "choice.", "When", []string{"When", "Else", "ElseNull"}, []string{"Predicate[", "RowValue[", "Case[", "User", "string"}, "/database/query/conditional_expression.go"},
		{"computed-group-key", "keyqueries/keys_test.go", "bucket.", "Group", []string{"Group", "Value", "Eq"}, []string{"Group[", "User"}, "/database/query/expression_key.go"},
		{"numeric-range-distance", "framequeries/ranges_postgres_test.go", "r.", "Preceding", []string{"Preceding", "Following", "Between", "CurrentRow"}, []string{"RangeBoundary[", "Sample", "int"}, "/database/query/window_range.go"},
		{"arithmetic-row-comparison", "scalarqueries/scalars_postgres_test.go", "incremented.", "Gte", []string{"Gte", "Lt", "Value", "Asc"}, []string{"Predicate[", "Sample", "int16"}, "/database/query/expression_comparison.go"},
		{"text-row-comparison", "scalarqueries/scalars_postgres_test.go", "trimmed.", "Contains", []string{"Contains", "Like", "Value", "Asc"}, []string{"Predicate[", "Sample", "string"}, "/database/query/expression_comparison.go"},
		{"nullable-arithmetic-comparison", "scalarqueries/scalars_postgres_test.go", "nullable.", "Gt", []string{"Gt", "IsNull", "Value"}, []string{"Predicate[", "Sample", "Decimal"}, "/database/query/expression_comparison.go"},
		{"selected-arithmetic-comparison", "scalarqueries/scalars_postgres_test.go", "query.OrderValue(doubled).", "Gt", []string{"Gt", "Eq", "In"}, []string{"HavingPredicate[", "Sample", "int64"}, "/database/query/expression_comparison.go"},
		{"computed-model-order", "scalarqueries/scalars_postgres_test.go", "magnitude.", "Desc", []string{"Desc", "Asc", "Value"}, []string{"Order[", "Sample"}, "/database/query/row_expression.go"},
		{"binary-row-comparison", "comparisonqueries/predicates_postgres_test.go", "query.", "Equal", []string{"Equal", "NotEqual", "Greater", "Like"}, []string{"RowValue[", "Predicate["}, "/database/query/value_comparison.go"},
		{"computed-join-condition", "comparisonqueries/joins_postgres_test.go", "adjacent.", "WhereRight", []string{"WhereLeft", "WhereRight", "Not"}, []string{"JoinOn[", "Predicate[", "olderAlias", "User"}, "/database/query/join_condition.go"},
		{"cartesian-model-slice", "comparisonqueries/joins_postgres_test.go", "records.OrderBy(l.Age.Asc()).", "All", []string{"All", "First", "Where", "OrderBy"}, []string{"[]", "User", "error"}, "/database/query/projection.go"},
		{"scalar-row-boundary", "comparisonqueries/subqueries_postgres_test.go", "query.", "ScalarRowQuery", []string{"ScalarRowQuery", "ScalarNullableRowQuery", "CorrelatedScalarRowQuery"}, []string{"RowExpression[", "Nullable[", "ValueQuerySource["}, "/database/query/row_subquery.go"},
		{"json-field-operators", "jsonqueries/query_postgres_test.go", "f.Settings.", "Contains", []string{"Contains", "ContainedBy", "Kind", "IsJSONNull", "Eq", "Value"}, []string{"JSON[", "Preferences", "Predicate[", "Document"}, "/database/query/field_json.go"},
		{"json-nullable-kind", "jsonqueries/query_postgres_test.go", "f.Backup.", "Kind", []string{"Contains", "ContainedBy", "IsNull", "IsJSONNull", "Kind"}, []string{"RowExpression[", "Nullable[", "JSONKind"}, "/database/query/field_json.go"},
		{"json-selected-input", "jsonqueries/query_postgres_test.go", "query.", "JSONContainsValue", []string{"JSONContainsValue", "JSONContainsNullableValue", "JSONTypeValue"}, []string{"Expression[", "bool"}, "/database/query/json_expression.go"},
		{"json-generated-properties", "jsonqueries/path_postgres_test.go", "f.Settings.", "Properties", []string{"Properties", "Path", "Contains"}, []string{"JSONProperties[", "Document"}, "/jsonqueries/document_foundry.gen.go"},
		{"json-typed-map-path", "jsonqueries/path_postgres_test.go", "p.Attributes.", "At", []string{"At", "JSON", "Exists", "IsMissing"}, []string{"int", "JSONPath[", "Document"}, "/jsonqueries/document_foundry.gen.go"},
		{"json-path-text-filter", "jsonqueries/path_postgres_test.go", "prefs.Theme.Scalar().", "Like", []string{"Like", "Contains", "Eq", "Value"}, []string{"string", "Predicate[", "Document"}, "/database/query/expression_comparison.go"},
		{"interval-field-summary", "intervalqueries/calculation_postgres_test.go", "f.Period.", "Sum", []string{"Sum", "Avg", "Min", "Max", "Eq"}, []string{"NullableOrderedAggregate[", "Interval"}, "/database/query/field_interval.go"},
		{"interval-dynamic-shift", "intervalqueries/calculation_postgres_test.go", "query.", "ShiftInstant", []string{"ShiftInstant", "ShiftLocal", "ShiftDate"}, []string{"RowValue[", "Interval", "TimeZone"}, "/database/query/interval_expression.go"},
		{"interval-selected-input", "intervalqueries/calculation_postgres_test.go", "query.", "IntervalMonthsNullableValue", []string{"IntervalMonthsNullableValue", "IntervalDaysNullableValue", "IntervalElapsedNullableValue"}, []string{"Expression[", "Nullable[", "int32"}, "/database/query/interval_expression.go"},
		{"temporal-instant-zone", "temporalqueries/calendar_postgres_test.go", "query.", "TruncateInstant", []string{"TruncateInstant", "LocalAt", "ResolveLocal"}, []string{"RowValue[", "TimestampUnit", "TimeZone"}, "/database/query/temporal_expression.go"},
		{"temporal-date-extraction", "temporalqueries/extract_postgres_test.go", "query.", "Year", []string{"Year", "Month", "ISOWeek"}, []string{"RowValue[", "OrderedRowExpression[", "int64"}, "/database/query/temporal_extract.go"},
		{"temporal-selected-value", "temporalqueries/composition_postgres_test.go", "query.", "FromUnixMillisValue", []string{"FromUnixMillisValue", "FromUnixMillisNullableValue"}, []string{"Expression[", "temporal.DateTime"}, "/database/query/temporal_expression.go"},
		{"lateral-record-selection", "lateralqueries/records_postgres_test.go", "query.", "SelectCorrelatedRecord", []string{"SelectCorrelatedRecord", "ProjectCorrelated", "AsLateral"}, []string{"CorrelatedRecordQuery[", "RecordScope["}, "/database/query/correlated_record.go"},
		{"lateral-source-ownership", "lateralqueries/records_postgres_test.go", "query.", "AsLateral", []string{"AsLateral", "LeftJoinLateral", "CrossJoinLateral"}, []string{"CorrelatedRecordSource[", "LateralSource["}, "/database/query/lateral_join.go"},
		{"lateral-generated-report", "lateralqueries/composition_postgres_test.go", "reports.", "ProjectCorrelatedOrderSummary", []string{"ProjectCorrelatedOrderSummary", "SelectCorrelatedOrderSummary", "ProjectOrderSummary"}, []string{"CorrelatedSource[", "OrderSummaryCorrelatedProject["}, "/reports/order_summary_foundry.gen.go"},
		{"lateral-report-having", "lateralqueries/composition_postgres_test.go", "selected.", "Having", []string{"Where", "Having", "Distinct", "Exists"}, []string{"CorrelatedRecordQuery[", "HavingPredicate[", "OrderSummary"}, "/database/query/correlated_record.go"},
		{"temporal-range-distance", "framequeries/ranges_postgres_test.go", "clock.", "Preceding", []string{"Preceding", "Following", "Between", "CurrentRow"}, []string{"RangeBoundary[", "Sample", "Interval"}, "/database/query/window_range.go"},
		{"completed-range-window", "framequeries/ranges_postgres_test.go", "r.", "Between", []string{"Between", "Asc", "Desc"}, []string{"RangeBoundary[", "Window[", "Sample"}, "/database/query/window_range.go"},
		{"named-window-descriptor", "framequeries/named_postgres_test.go", "query.WindowFor(q).PartitionBy(f.ID.Param(1).Group()).", "Named", []string{"Named", "OrderBy", "PartitionBy"}, []string{"string", "Window[", "Sample"}, "/database/query/window_named.go"},
		{"selected-expression-key", "keyqueries/keys_test.go", "selected.", "Key", []string{"Key", "Asc", "Desc"}, []string{"ProjectionKey[", "User"}, "/database/query/expression_key.go"},
		{"selected-window-partition", "keyqueries/keys_test.go", "query.WindowFor(base).", "PartitionByValues", []string{"PartitionBy", "PartitionByValues", "OrderBy"}, []string{"ProjectionKey[", "Window[", "User"}, "/database/query/window.go"},
		{"typed-filtered-comparison", "filterqueries/filters_postgres_test.go", "filtered.", "Gt", []string{"Filter", "Gt", "IsNull", "Over"}, []string{"decimal.Decimal", "HavingPredicate["}, "/database/query/aggregate_condition.go"},
		{"typed-result-cursor-fields", "cursorqueries/cursors_postgres_test.go", "cursorFields.", "Nickname", []string{"ID", "Email", "Nickname", "Status"}, []string{"NullableTextField[", "CursorScope[", "UserSummary"}, "/reports/user_summary_foundry.gen.go"},
		{"typed-model-key-chunks", "chunkqueries/chunks_postgres_test.go", "q.", "ChunkByID", []string{"Chunk", "ChunkByID", "EachChunked", "EachByID"}, []string{"ChunkByID", "[]", "User", "error"}, "/database/query/chunk.go"},
		{"typed-model-chunk-callback", "chunkqueries/chunks_postgres_test.go", "q.", "EachChunked", []string{"Each", "EachChunked", "EachByID"}, []string{"EachChunked", "User", "error"}, "/database/query/chunk.go"},
		{"typed-conflict-rows", "upsertqueries/calculations_postgres_test.go", "rows.", "Proposed", []string{"Stored", "Proposed"}, []string{"ConflictRow[", "WriteRecord"}, "/database/query/conflict_rows.go"},
		{"typed-conflict-index", "upsertqueries/index_targets_postgres_test.go", "query.", "OnConflictKeys", []string{"OnConflictKeys", "OnConflict"}, []string{"Group[", "Conflict["}, "/database/query/conflict_target.go"},
		{"typed-conflict-target-predicate", "upsertqueries/index_targets_postgres_test.go", "query.OnConflictKeys(key.Group()).", "TargetWhere", []string{"TargetWhere", "Where"}, []string{"Predicate[", "WriteRecord"}, "/database/query/conflict_target.go"},
		{"typed-conflict-value", "upsertqueries/calculations_postgres_test.go", "query.", "SetConflictValue", []string{"SetConflictValue"}, []string{"ConflictValueField[", "RowValue[", "ConflictRow[", "ConflictUpdate["}, "/database/query/conflict_rows.go"},
		{"typed-conflict-condition", "upsertqueries/calculations_postgres_test.go", ").Where(f.Enabled.Eq(true)).", "WhereRows", []string{"Where", "WhereRows"}, []string{"Predicate[", "ConflictRow[", "WriteRecord"}, "/database/query/conflict_rows.go"},
		{"typed-model-upsert", "upsertqueries/upserts_postgres_test.go", "q.", "Upsert", []string{"Upsert", "UpsertMany", "CreateMany"}, []string{"Optional[", "WriteRecord", "Conflict["}, "/models/write_record_foundry.gen.go"},
		{"typed-lock-natural-key", "lockqueries/locking_postgres_test.go", "lockedCountry.", "RequireFind", []string{"RequireFind", "Find", "All", "SkipLocked"}, []string{"CountryCode", "*", "Tx", "Country"}, "/models/country_foundry.gen.go"},
		{"typed-lock-transaction", "lockqueries/locking_postgres_test.go", "lockedCountry.SkipLocked().", "All", []string{"All", "First", "NoWait"}, []string{"[]", "Country", "*", "Tx"}, "/database/query/locked_model.go"},
		{"typed-lock-join-scope", "lockqueries/locking_postgres_test.go", "query.SelectRecord(joined, countryScope).ForUpdate().", "Of", []string{"Of", "All", "SkipLocked"}, []string{"LockTarget[", "Left[", "Country", "Location"}, "/database/query/locked_result.go"},
		{"typed-lock-clauses", "lockqueries/clauses_postgres_test.go", "query.SelectRecord(joined, country).", "LockRows", []string{"LockRows", "ForUpdate"}, []string{"RowLock[", "Inner[", "Country", "Location"}, "/database/query/row_lock_clause.go"},
		{"typed-lock-clause-policy", "lockqueries/clauses_postgres_test.go", "query.KeyShareLock(location).", "NoWait", []string{"NoWait", "SkipLocked", "Wait"}, []string{"RowLock[", "Country", "Location"}, "/database/query/row_lock_clause.go"},
		{"typed-lock-clause-transaction", "lockqueries/clauses_postgres_test.go", "selected.", "All", []string{"All", "First", "Each"}, []string{"[]", "Country", "*", "Tx"}, "/database/query/locked_result.go"},
		{"typed-transaction-projection", "transactionqueries/models.go", "SelectID(fields.ID.Value()).", "SelectName", []string{"SelectName", "SelectID", "Query"}, []string{"Expression[", "string", "SummaryTransactionProject["}, "/transactionqueries/summary_foundry.gen.go"},
		{"typed-transaction-terminal", "transactionqueries/composition_postgres_test.go", "q.", "All", []string{"All", "First", "Count", "Exists", "Each"}, []string{"[]", "Record", "*", "Tx"}, "/database/query/transaction_query.go"},
		{"typed-transaction-cte", "transactionqueries/composition_postgres_test.go", "query.TransactionCTE(\"first_claim\", first).", "Materialized", []string{"Materialized", "NotMaterialized"}, []string{"TransactionCommonTable[", "Record"}, "/database/query/transaction_cte.go"},
		{"typed-transaction-value-lock", "transactionqueries/subqueries_postgres_test.go", "query.SelectTransactionValue(inner, b.ID.Value()).Where(b.ID.Eq(2)).", "ForUpdate", []string{"ForUpdate", "ForShare", "Count", "All"}, []string{"LockedTransactionValue[", "int64"}, "/database/query/transaction_value.go"},
		{"typed-transaction-scalar", "transactionqueries/subqueries_postgres_test.go", "query.", "TransactionScalarQuery", []string{"TransactionScalarQuery", "TransactionScalarNullableQuery", "TransactionScalarRowQuery"}, []string{"TransactionProjectionSource[", "TransactionValueSource[", "Nullable["}, "/database/query/transaction_subquery.go"},
		{"typed-transaction-membership", "transactionqueries/subqueries_postgres_test.go", "query.", "TransactionInQuery", []string{"TransactionInQuery", "TransactionInNullableQuery"}, []string{"RowValue[", "TransactionValueSource[", "Predicate["}, "/database/query/transaction_subquery.go"},
		{"typed-transaction-correlated-projection", "transactionqueries/correlations_postgres_test.go", "ProjectTransactionCorrelatedSummary(c).", "SelectID", []string{"SelectID", "SelectName", "Query"}, []string{"Expression[", "TransactionCorrelation[", "SummaryTransactionCorrelatedProject["}, "/transactionqueries/summary_foundry.gen.go"},
		{"typed-transaction-correlated-lock", "transactionqueries/correlations_postgres_test.go", "query.SelectTransactionCorrelatedRecord(c, query.InnerScope(c, child.Scope())).OrderBy(f.ID.Asc()).Limit(1).", "ForUpdate", []string{"ForUpdate", "ForShare", "NoWait", "Of", "Exists"}, []string{"TransactionCorrelatedRecordQuery[", "Record"}, "/database/query/transaction_correlated_lock.go"},
		{"typed-transaction-lateral-join", "transactionqueries/correlations_postgres_test.go", "query.", "TransactionLeftJoinLateral", []string{"TransactionCrossJoinLateral", "TransactionInnerJoinLateral", "TransactionLeftJoinLateral"}, []string{"TransactionJoinInput[", "TransactionLateralSource[", "TransactionLeftJoined["}, "/database/query/transaction_lateral.go"},
		{"typed-transaction-correlated-scalar", "transactionqueries/correlations_postgres_test.go", "query.", "TransactionCorrelatedScalarQuery", []string{"TransactionCorrelatedScalarQuery", "TransactionCorrelatedScalarNullableQuery", "TransactionCorrelatedScalarRowQuery"}, []string{"TransactionCorrelatedValueSource[", "Nullable["}, "/database/query/transaction_correlated_value.go"},
		{"typed-transaction-nullable-correlation", "transactionqueries/correlations_postgres_test.go", "query.", "OuterNullableScope", []string{"OuterNullableScope", "OuterScope", "InnerScope", "InnerNullableScope"}, []string{"outerCorrelationSource[", "NullableRecordScope["}, "/database/query/correlation_boundary.go"},
		{"typed-explicit-getter", "mutatorqueries/accessors_test.go", "member.", "AccessEmail", []string{"AccessID", "AccessEmail", "AccessNickname"}, []string{"AccessEmail", "DisplayEmail", "error"}, "/mutatorqueries/accessors.go"},
		{"typed-mutator-draft", "mutatorqueries/mutators_postgres_test.go", "mutatorqueries.MemberDraft{}.", "SetEmail", []string{"SetEmail", "SetAttempts", "ClearNickname"}, []string{"SetEmail", "string", "MemberDraft"}, "/mutatorqueries/member_foundry.gen.go"},
		{"typed-relation-attach", "linkqueries/lifecycle_postgres_test.go", "r.", "Attach", []string{"Attach", "Detach", "ForceDetach"}, []string{"CreateDraft[", "Member", "Group", "Membership"}, "/database/query/relation_write.go"},
		{"typed-relation-detach", "linkqueries/lifecycle_postgres_test.go", "r.", "Detach", []string{"Detach", "WithWriteLimit"}, []string{"[]", "Membership"}, "/database/query/relation_write.go"},
		{"typed-relation-force-detach", "linkqueries/lifecycle_postgres_test.go", "r.WithTrashedPivot().", "ForceDetach", []string{"ForceDetach", "Detach"}, []string{"[]", "Membership"}, "/database/query/relation_write.go"},
		{"typed-relation-write-limit", "linkqueries/lifecycle_postgres_test.go", "r.", "WithWriteLimit", []string{"WithWriteLimit", "Attach"}, []string{"int", "MaxRelationWriteRows"}, "/database/query/relation_write.go"},
		{"typed-per-model-create", "linkqueries/each_postgres_test.go", "q.", "CreateEach", []string{"CreateEach", "CreateMany"}, []string{"[]", "MembershipDraft", "Membership"}, "/linkqueries/membership_foundry.gen.go"},
		{"typed-per-model-update", "linkqueries/each_postgres_test.go", "q.", "UpdateEach", []string{"UpdateEach", "Update"}, []string{"func(", "MembershipDraft", "Membership", "Tx", "int"}, "/linkqueries/membership_foundry.gen.go"},
		{"typed-per-model-delete", "linkqueries/each_postgres_test.go", "q.", "DeleteEach", []string{"DeleteEach", "Delete"}, []string{"[]", "Membership", "int"}, "/linkqueries/membership_foundry.gen.go"},
		{"typed-per-model-restore", "linkqueries/each_postgres_test.go", "q.", "RestoreEach", []string{"RestoreEach", "Restore"}, []string{"[]", "Membership", "int"}, "/linkqueries/membership_foundry.gen.go"},
		{"typed-per-model-force-delete", "linkqueries/each_postgres_test.go", "q.", "ForceDeleteEach", []string{"ForceDeleteEach", "ForceDelete"}, []string{"[]", "Membership", "int"}, "/linkqueries/membership_foundry.gen.go"},
		{"typed-lookup-create", "linkqueries/lookup_postgres_test.go", "selected.", "FirstOrCreate", []string{"FirstOrCreate", "UpdateOrCreate"}, []string{"MembershipDraft", "Membership"}, "/linkqueries/membership_foundry.gen.go"},
		{"typed-lookup-update", "linkqueries/lookup_postgres_test.go", "selected.", "UpdateOrCreate", []string{"FirstOrCreate", "UpdateOrCreate"}, []string{"MembershipDraft", "Membership", "Tx", "func("}, "/linkqueries/membership_foundry.gen.go"},
		{"typed-insert-select-field", "linkqueries/insert_select_postgres_test.go", "linkqueries.InsertArchiveFrom(source).", "SelectMemberID", []string{"SelectID", "SelectMemberID", "SelectName", "SelectAlias", "Values", "Returning", "Exec"}, []string{"Expression[", "ID[", "Member"}, "/linkqueries/archive_foundry.gen.go"},
		{"typed-insert-select-values", "linkqueries/insert_select_postgres_test.go", "plan.", "Values", []string{"Values", "Returning", "Exec"}, []string{"ArchiveDraft", "ArchiveInsertFromBuilder"}, "/linkqueries/archive_foundry.gen.go"},
		{"typed-insert-select-returning", "linkqueries/insert_select_postgres_test.go", "plan.", "Returning", []string{"Returning", "Exec"}, []string{"[]", "Archive", "int", "Transactor"}, "/linkqueries/archive_foundry.gen.go"},
		{"typed-source-update-key", "linkqueries/source_write_postgres_test.go", "linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).", "MatchID", []string{"MatchID", "MatchNullableID", "SelectName", "SelectAlias", "Values", "Exec", "Returning"}, []string{"Expression[", "ID[", "Member"}, "/linkqueries/member_foundry.gen.go"},
		{"typed-source-update-returning", "linkqueries/source_write_postgres_test.go", "plan.", "Returning", []string{"Returning", "Exec", "Values"}, []string{"[]", "Member", "int", "Transactor"}, "/linkqueries/member_foundry.gen.go"},
		{"typed-source-update-values", "linkqueries/source_write_postgres_test.go", "linkqueries.UpdateMemberFrom(linkqueries.QueryLinkMembers(), linkqueries.QueryLinkArchives()).MatchID(a.MemberID.Value()).", "Values", []string{"Values", "Returning", "SelectName"}, []string{"MemberDraft", "MemberUpdateFromBuilder"}, "/linkqueries/member_foundry.gen.go"},
		{"typed-source-delete-natural-key", "linkqueries/source_write_postgres_test.go", "linkqueries.DeleteGroupUsing(linkqueries.QueryLinkGroups(), linkqueries.QueryLinkMemberships()).", "MatchCode", []string{"MatchCode", "MatchNullableCode", "Exec", "Returning"}, []string{"Expression[", "GroupCode"}, "/linkqueries/group_foundry.gen.go"},
		{"typed-model-reference", "mutatorqueries/reference_test.go", "member.", "FoundryReference", []string{"FoundryReference", "FoundryIdentity", "AccessID"}, []string{"Reference[", "Member", "ID[", "stored", "AccessID"}, "/mutatorqueries/member_foundry.gen.go"},
		{"typed-model-reference-key", "mutatorqueries/reference_test.go", "reference.", "Key", []string{"Key", "Identity", "Parse", "ModelName"}, []string{"ID[", "Member"}, "/model/reference.go"},
		{"typed-event-after-commit", "eventqueries/events.go", "Created.", "AfterCommit", []string{"AfterCommit", "Dispatch", "Declare", "Name", "Version"}, []string{"RecordCreated", "Tx", "Bus"}, "/events/dispatch.go"},
		{"typed-event-model-key", "eventqueries/events_postgres_test.go", "entries[0].Event.", "ID", []string{"ID"}, []string{"ID[", "Plain"}, "/eventqueries/events.go"},
		{"typed-event-listener", "eventqueries/events.go", "events.", "RegisterListener", []string{"RegisterListener", "RegisterTopic", "Module", "Define"}, []string{"Topic[", "Handler[", "ListenerID"}, "/events/module.go"},
		{"typed-outbox-enqueue", "eventqueries/durable.go", "Created.", "Enqueue", []string{"Enqueue", "Find", "AfterCommit", "Dispatch"}, []string{"ID[", "RecordCreated", "Tx", "Outbox"}, "/events/outbox.go"},
		{"typed-outbox-stored-payload", "eventqueries/durable_postgres_test.go", "stored.", "Payload", []string{"Payload", "ID", "Origin", "CreatedAt"}, []string{"RecordCreated", "error"}, "/events/outbox.go"},
		{"typed-outbox-model-field", "eventqueries/durable_postgres_test.go", "link.", "MessageID", []string{"MessageID", "ID", "FoundryReference"}, []string{"ID[", "RecordCreated"}, "/eventqueries/event_link.go"},
		{"typed-outbox-message-id-setter", "eventqueries/durable_postgres_test.go", "eventqueries.EventLinkDraft{}.SetID(1).", "SetMessageID", []string{"SetMessageID", "MessageID", "SetID"}, []string{"RecordCreated", "EventLinkDraft"}, "/eventqueries/event_link_foundry.gen.go"},
		{"typed-audit-capture", "auditqueries/history.go", "changes.", "Audit", []string{"Audit", "Before", "After"}, []string{"AccountAuditPolicy", "Model[", "Account"}, "/auditqueries/account_foundry.gen.go"},
		{"typed-audit-field", "auditqueries/history.go", "fields.", "Email", []string{"Email", "PasswordHash", "ID"}, []string{"FieldChange[string]", "AccessEmail", "MutateEmail"}, "/auditqueries/account_foundry.gen.go"},
		{"typed-audit-stored-value", "auditqueries/history.go", "email.", "After", []string{"After", "Before", "Changed", "Assigned"}, []string{"FieldValue[string]"}, "/audit/record/view.go"},
		{"typed-audit-model-history", "auditqueries/audit_postgres_test.go", "row.", "Changes", []string{"Changes", "ID", "Origin", "Area"}, []string{"Model[", "Account", "ID["}, "/audit/model.go"},
		{"typed-soft-delete-force", "softqueries/lifecycle_postgres_test.go", "q.WithTrashed().", "ForceDelete", []string{"ForceDelete", "Restore", "Delete"}, []string{"ID[", "Member"}, "/softqueries/member_foundry.gen.go"},
		{"typed-soft-delete-restore", "softqueries/lifecycle_postgres_test.go", "q.", "Restore", []string{"Restore", "WithTrashed", "OnlyTrashed"}, []string{"ID[", "Member"}, "/softqueries/member_foundry.gen.go"},
		{"typed-soft-delete-field", "softqueries/lifecycle_postgres_test.go", "deleted.", "DeletedAt", []string{"DeletedAt", "AccessDeletedAt"}, []string{"Nullable[", "Managed soft-delete timestamp", "Custom getter"}, "/softqueries/models.go"},
		{"typed-soft-delete-operation", "softqueries/hooks.go", "changes.", "Operation", []string{"Operation", "Before", "After", "Fields"}, []string{"Optional[", "Operation"}, "/softqueries/member_foundry.gen.go"},
		{"typed-soft-delete-pivot", "softqueries/visibility_postgres_test.go", "r.Groups.", "OnlyTrashedPivot", []string{"WithTrashed", "OnlyTrashed", "WithTrashedPivot", "OnlyTrashedPivot"}, []string{"ThroughRelation["}, "/database/query/relation_soft_delete.go"},
		{"typed-timestamp-draft", "timequeries/timestamps_postgres_test.go", "timequeries.MemberDraft{}.SetName(\" explicit \").SetCreatedAt(old).", "SetUpdatedAt", []string{"SetCreatedAt", "SetUpdatedAt"}, []string{"DateTime", "MemberDraft"}, "/timequeries/member_foundry.gen.go"},
		{"typed-timestamp-field-notice", "timequeries/timestamps_postgres_test.go", "member.", "UpdatedAt", []string{"CreatedAt", "UpdatedAt"}, []string{"DateTime", "Managed update timestamp"}, "/timequeries/models.go"},
		{"typed-database-clock", "timequeries/fixture_test.go", "tx.", "Clock", []string{"Clock", "Savepoint"}, []string{"clock.Clock"}, "/database/clock.go"},
		{"typed-mutation-input-draft", "inputqueries/inputs_postgres_test.go", "inputqueries.MemberDraft{}.", "SetEmail", []string{"SetEmail", "SetNote", "ClearNote"}, []string{"EmailInput", "MemberDraft"}, "/inputqueries/member_foundry.gen.go"},
		{"typed-mutation-input-conflict", "inputqueries/inputs_postgres_test.go", "f.Email.", "Set", []string{"Set", "Eq", "Like", "Incoming"}, []string{"EmailInput", "ConflictUpdate"}, "/inputqueries/member_foundry.gen.go"},
		{"typed-mutation-input-field-notice", "inputqueries/inputs_postgres_test.go", "member.", "Email", []string{"Email", "Note", "AccessEmail", "MutateEmail"}, []string{"StoredEmail", "Custom setter", "EmailInput", "Custom getter"}, "/inputqueries/models.go"},
		{"typed-automatic-mutator", "mutatorqueries/member_foundry.gen.go", "(Member{}).", "MutateEmail", []string{"MutateEmail", "MutateNickname", "MutateAttempts"}, []string{"MutateEmail", "string", "error"}, "/mutatorqueries/member.go"},
		{"typed-field-change", "mutatorqueries/changes_postgres_test.go", "email.", "Assigned", []string{"Assigned", "Changed", "Before", "After"}, []string{"Assigned", "bool"}, "/database/lifecycle/change.go"},
		{"generated-model-changes", "model_changes_test.go", "changes.", "Before", []string{"Before", "After", "Fields", "Assigned", "Changed"}, []string{"Before", "Optional[", "User"}, "/models/user_foundry.gen.go"},
		{"generated-field-changes", "model_changes_test.go", "changes.Fields().", "Email", []string{"ID", "Email", "Nickname", "IntroducerID"}, []string{"FieldChange[", "string"}, "/models/user_foundry.gen.go"},
		{"typed-hook-draft", "hookqueries/member.go", "draft.", "SetIntroducerID", []string{"SetIntroducerID", "ClearIntroducerID", "UnsetIntroducerID"}, []string{"SetIntroducerID", "ID[", "MemberDraft"}, "/hookqueries/member_foundry.gen.go"},
		{"typed-hook-callback", "hookqueries/member_foundry.gen.go", "hooks.", "Creating", []string{"Creating", "Updating", "Deleting", "AfterCommit"}, []string{"Context", "Tx", "MemberDraft", "error"}, "/hookqueries/member_foundry.gen.go"},
		{"typed-retrieval-callback", "retrievalqueries/member_foundry.gen.go", "hooks.", "Retrieved", []string{"Retrieved"}, []string{"Context", "Executor", "Member", "error"}, "/retrievalqueries/member_foundry.gen.go"},
		{"generated-retrieval-registration", "retrievalqueries/retrieval_postgres_test.go", "retrievalqueries.", "RegisterMemberRetrievalObserver", []string{"RegisterMemberRetrievalObserver", "NewMemberRetrievalObserver", "RegisterGroupRetrievalObserver"}, []string{"MemberRetrievalObserver", "MemberRetrievalHooks", "Resolver"}, "/retrievalqueries/member_foundry.gen.go"},
		{"typed-provider-contributions", "contributions_test.go", "foundation.", "ResolveAll", []string{"ResolveAll", "Resolve", "Factory", "Provide"}, []string{"ResolveAll", "Resolver", "error"}, "/foundation/resolve_all.go"},
		{"typed-observer-registration", "observer_ownership_test.go", "database.", "RegisterObserver", []string{"RegisterObserver", "Module", "Prepare"}, []string{"Observer[", "Resolver", "error"}, "/database/observers.go"},
		{"database-startup-diagnostics", "database_example_test.go", "database.", "WithStartupLog", []string{"WithStartupLog", "WithSlowQueryLog", "WithQueryObserver"}, []string{"slog.Logger", "Option"}, "/database/startup_logging.go"},
		{"generated-observer-registration", "observerqueries/observers_postgres_test.go", "observerqueries.", "RegisterRecordObserver", []string{"RegisterRecordObserver", "NewRecordObserver", "RegisterPlainObserver"}, []string{"RecordObserver", "RecordHooks", "Resolver"}, "/observerqueries/record_foundry.gen.go"},
		{"typed-nullable-field-snapshot", "mutatorqueries/changes_postgres_test.go", "nickname.", "After", []string{"Assigned", "Changed", "Before", "After"}, []string{"After", "Optional[", "Nullable[", "string"}, "/database/lifecycle/change.go"},
		{"typed-model-upsert-batch", "upsertqueries/upserts_postgres_test.go", "q.", "UpsertMany", []string{"Upsert", "UpsertMany", "CreateMany"}, []string{"[]", "WriteRecordDraft", "WriteRecord", "Conflict["}, "/models/write_record_foundry.gen.go"},
		{"typed-nullable-conflict-assignment", "upsertqueries/upserts_postgres_test.go", "f.Note.", "SetNull", []string{"Incoming", "Set", "SetNull"}, []string{"ConflictUpdate[", "WriteRecord"}, "/database/query/conflict.go"},
	}
}

GO ?= go
TEST_PACKAGE_BATCH_SIZE ?= 6
TEST_TIMEOUT ?= 20m
GENERATION_DIRS := logging cloud/credentials infrastructure application internal/outboxstore internal/idempotencystore internal/auditstore internal/sessionstore internal/tokenstore internal/challengestore internal/mfastore internal/notificationstore internal/authtransport internal/extensionstore internal/attachmentstore internal/jobarchive internal/webhookstore countries datatable tests/fixtures/consumer
FIXTURE_DIRS := tests/fixtures/plugin_base tests/fixtures/plugin_dep tests/fixtures/consumer
export GOWORK := off

# Compute once, lazily, for this make invocation. The tool prints one validated
# environment assignment; empty output is a hard failure. External child gates
# read this variable through testkit.TrackExternalInputs; ordinary tests keep
# Go's normal caching. No shell evaluation of user-controlled command text.
define prepare_test_inputs
$(if $(foundry_test_input_assignment),,$(eval foundry_test_input_assignment := $(shell $(GO) run ./internal/cmd/testinputs --go $(GO))))
$(if $(foundry_test_input_assignment),$(eval export $(foundry_test_input_assignment)),$(error Could not fingerprint external test inputs))
endef

# Keep temporary test binaries bounded without reducing package coverage.
# Capture go list first so a failed package discovery cannot pass via xargs.
# Exit 255 tells xargs to stop immediately after a failed test batch.
define test_packages
task_packages=$$($(GO) list ./...); \
test -n "$$task_packages"; \
printf '%s\n' "$$task_packages" | xargs -n $(TEST_PACKAGE_BATCH_SIZE) sh -c '$(GO) test -timeout=$(TEST_TIMEOUT) $(1) "$$@" || exit 255' foundry-tests
endef

# Framework-owned generated models precede consumer generation so a fresh
# checkout can bootstrap their metadata without relying on existing output.
# The repository opts into managed field notes, which its editor tests inspect.
define generate_packages
set -eu; for task_generate_dir in $(GENERATION_DIRS); do \
	$(GO) run ./cmd/foundry generate --recursive --field-docs $(1) --dir "$$task_generate_dir"; \
done
endef

.PHONY: toolchain-check fmt fmt-check vet test race fixture-check generate generate-check gopls-install agent-smoke typescript-check test-postgres test-redis docs-check release-tools-check security-check security-tools-check verify

toolchain-check:
	$(GO) version

fmt:
	@sh tools/format-go.sh write "$$($(GO) env GOROOT)/bin/gofmt"

fmt-check:
	@sh tools/format-go.sh check "$$($(GO) env GOROOT)/bin/gofmt"

vet:
	$(GO) vet ./...

test:
	$(call prepare_test_inputs)
	@set -eu; $(call test_packages,)

race:
	$(call prepare_test_inputs)
	@set -eu; $(call test_packages,-race)
	@set -eu; for task_fixture_dir in $(FIXTURE_DIRS); do \
		(cd "$$task_fixture_dir"; $(call test_packages,-race)); \
	done

fixture-check:
	$(call prepare_test_inputs)
	@set -eu; for task_fixture_dir in $(FIXTURE_DIRS); do \
		(cd "$$task_fixture_dir"; $(GO) vet ./...; $(call test_packages,)); \
	done

generate:
	@$(call generate_packages,)

generate-check:
	@$(call generate_packages,--check)

gopls-install:
	@mkdir -p bin
	GOBIN="$(CURDIR)/bin" $(GO) install golang.org/x/tools/gopls@$$(cat tools/gopls.version)

agent-smoke: generate-check
	@test -n "$$FOUNDRY_TEST_GOPLS" || { printf 'Set FOUNDRY_TEST_GOPLS to an existing approved gopls executable.\n'; exit 1; }
	$(GO) test -timeout=$(TEST_TIMEOUT) -count=1 -v ./internal/agent -run '^(TestRealGoplsConsumer|TestRealGoplsFieldBehaviorDocumentation)$$'

test-redis:
	FOUNDRY_TEST_REDIS_REQUIRED=1 $(GO) test -race -timeout=$(TEST_TIMEOUT) ./redis/... ./cache/... ./lease/... ./ratelimit/... ./pubsub/...

docs-check:
	$(GO) run ./internal/cmd/checkdocs

release-tools-check:
	cd tools/release && $(GO) vet ./... && $(GO) test ./...
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p '*_test.py'

typescript-check:
	@test -n "$$FOUNDRY_TEST_NODE" -a -n "$$FOUNDRY_TEST_TYPESCRIPT" || { printf 'Set FOUNDRY_TEST_NODE and FOUNDRY_TEST_TYPESCRIPT to existing native tool paths.\n'; exit 1; }
	cd tests/fixtures/consumer && FOUNDRY_TEST_TYPESCRIPT_REQUIRED=1 $(GO) test -timeout=$(TEST_TIMEOUT) -count=1 -v ./clientcontracts -run '^(TestTypeScriptClientAgainstRealHTTPAndWebSocket|TestOptionalFormAdaptersWithRealReactAndVue)$$'

test-postgres:
	$(GO) run ./internal/cmd/testpostgres --go $(GO) --race --batch-size $(TEST_PACKAGE_BATCH_SIZE) --timeout $(TEST_TIMEOUT)

verify: toolchain-check fmt-check vet test fixture-check generate-check docs-check release-tools-check

# Scans reuse installed tools and warm caches; no cold package measurement.
SECURITY_OUTPUT ?= .cache/security-check
security-check:
	@test -n "$$FOUNDRY_TEST_GOVULNCHECK" -a -n "$$FOUNDRY_TEST_GOPLS" -a -n "$$FOUNDRY_TEST_NODE" -a -n "$$FOUNDRY_TEST_NPM" || { printf 'Select existing scanner, gopls, Node and npm executables.\n'; exit 1; }
	PYTHONDONTWRITEBYTECODE=1 python3 tools/security_scan.py --go "$(GO)" --govulncheck "$$FOUNDRY_TEST_GOVULNCHECK" --gopls "$$FOUNDRY_TEST_GOPLS" --node "$$FOUNDRY_TEST_NODE" --npm "$$FOUNDRY_TEST_NPM" --module . --module tools/release $(foreach module,$(FIXTURE_DIRS),--module $(module)) --output "$(SECURITY_OUTPUT)"

security-tools-check:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p '*_test.py'

# Native tests run without custom tags. Required mode prevents optional skips;
# bypass result caching here because shared-library changes are external inputs.
.PHONY: test-imaging-portable test-imaging-native
test-imaging-portable:
	CGO_ENABLED=0 $(GO) test ./imaging ./validation/imaging
	cd tests/fixtures/consumer && CGO_ENABLED=0 $(GO) test ./profiles -run '^TestImageRuntimeMissingLibrary$$'

test-imaging-native:
	CGO_ENABLED=1 FOUNDRY_TEST_VIPS_REQUIRED=1 $(GO) test -count=1 ./imaging ./validation/imaging
	cd tests/fixtures/consumer && CGO_ENABLED=1 FOUNDRY_TEST_VIPS_REQUIRED=1 $(GO) test -count=1 ./profiles -run '^(TestConfiguredNativeImageService|TestImageRuntimeMissingLibrary)$$'

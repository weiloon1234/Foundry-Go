# Independent dependent plugin fixture

This library module depends on the [base plugin](../plugin_base/README.md) and
requires its compatible semantic release. It contributes an HTTP route and
middleware, typed job and event listener, policy, historical migration, asset
bundle and typed Go scaffold through public framework APIs.

Its tests use the public plugin harness. The separate
[application consumer](../consumer/pluginusage/bootstrap.go) registers both
plugins directly, executes the contributed worker job, observes reverse shutdown,
publishes distributions and verifies migration history on PostgreSQL.

Every main module owns its replacements; no workspace-only resolution or inherited
transitive replacements are assumed. Milestone 22 independent compilation,
ordinary/race tests, compiler/editor cases and full acceptance passed.

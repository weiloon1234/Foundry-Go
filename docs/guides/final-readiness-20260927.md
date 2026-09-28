# Final team readiness review — 2026-09-27

**Accepted for team boilerplate development on the supported stack.** The audit,
fixes and final source/package verification are complete. Use the [team adoption
guide](team-adoption.md) to start. The [evidence record](../evidence/final-readiness-20260927.json)
retains exact commands, source fingerprints, findings and package identities.

Scope: use Foundry-Go as a dependency in an independently owned team boilerplate.
The source review covers all 66 current module families. It reconciles the prior
module/security review with current file hashes, directly reviews recent runtime
changes and consumer entry points, and runs fresh whole-framework verification.
This is not a claim that every unchanged line was manually reread.

## Findings and delivered work

1. Browser plural selection could round a decimal with more than 20 fractional
   digits to zero and choose an incorrect form. The shared TypeScript message
   renderer now uses its exported English fallback when Intl cannot preserve the
   scale. Go continues to use exact decimal selection. A generated SDK regression
   covers the supported boundary, tiny positive/negative values and real HTTP.
2. Named database validation already retained explicitly injected executors, but
   lacked one consumer scenario proving the combined contract. New PostgreSQL
   tests exercise built-in/custom/batched rules, concurrent real query ownership,
   selected-pool failure, cancellation and reuse. The guide documents the pattern.
3. Prepared translation recipes now have bounded fuzz coverage for serialization,
   restored rendering, argument substitution and literal fallbacks.
4. The configured private package omitted localization source used by its
   translation test. The release packager now includes those declarations and
   catalog assets, with a regression that also preserves ordinary-profile isolation.
   A fresh candidate verifies the correction; the failed first candidate is retained.

The review found no other required framework module or ordinary implementation
TODO blocking the team's boilerplate. Deferred roadmap work remains outside the
supported first-release scope.

## Verification

| Gate | Result |
| --- | --- |
| Baseline full `make verify` | Passed in 531.61 seconds |
| Full framework/plugin/consumer `make race` | Passed in 1297.07 seconds |
| Named database validation races | Passed with real PostgreSQL pools, cancellation and reuse |
| Final plural regressions | Generated TypeScript/HTTP consumer and exact Go catalog races passed |
| Final full `make verify` | Passed in 521.47 seconds with required PostgreSQL, Redis, TypeScript and real gopls |
| Bounded fuzz campaigns | Prepared recipes, configuration, manifest, multipart cleanup and WebSocket protocol passed; 1,409,724 executions total |
| Security | Root, release tool, three fixtures, gopls, npm and packaged framework/consumer reviewed; no affected functions and zero npm vulnerabilities |
| Private module consumption | Ordinary/configured/full profiles passed download, integrity, reproducible generation, builds, real completion/hover; executable startup smoke checks passed |
| Pinned CLI and Linux builds | Module-pinned tool and configured consumer tests passed; static amd64/arm64 cross-builds passed |

The full race gate preceded the TypeScript/package corrections; final focused
client and catalog races, release-tool races and the final whole-source gate verify
those deltas. Native checks used macOS arm64, Go 1.27.1, warm build caches and the
existing services.
All 4,071 final source/build/documentation inputs stayed unchanged during
verification. The 3,710 code-input fingerprint is retained in the evidence;
final reporting-only documentation follows package preparation. No new performance
claim is made.

The private candidate selected 60 dependency modules, including framework/plugins,
without replacements. All 57 external versions and their license hashes match
the prior reviewed graph; 73 license/notice/patent files are retained. Candidate
archives and task logs contain neither the configured test URL nor its password.

The scanner used its recorded database timestamp, 2026-09-24T20:07:49Z. Existing
module-only advisories remain recorded in [dependency review](../dependency-review.md).
Final packaged import graphs exclude the affected OpenPGP packages. The gopls
result is binary-symbol evidence, not source-level reachability. A zero affected
function result is not a guarantee against unknown vulnerabilities.

The initial baseline attempt stopped because the private runner omitted the
required Redis endpoint. Restoring the existing endpoint fixed that orchestration
issue; no product behavior was changed to bypass the check.

## Adoption boundaries

Use a retrievable reviewed module version and pin the framework CLI with the
runtime. Private candidate acceptance proves module consumption; it does not
publish a version. The team owns its application settings, domain code and
production configuration.

PostgreSQL is the delivered database backend, with multiple named connections.
Concurrent validation can use separate pools; checks sharing a transaction should
remain sequential. Cross-database checks provide neither a shared snapshot nor a
distributed transaction; database constraints remain authoritative for uniqueness.

The supported deployment boundaries from the existing acceptance remain:
standalone Redis, supported macOS/Linux local persistence, bounded realtime replay
and explicit durable outbox/idempotency contracts. Linux cross-builds are compilation
evidence, not Linux runtime certification. Real AWS/R2 evidence is retained only
for unchanged adapter code; live email delivery was not newly exercised.

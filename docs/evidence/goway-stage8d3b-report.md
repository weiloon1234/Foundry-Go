# Stage 8d3b — engine retirement

Base: `bf553c7b`. No commits. Cursor wire identity and final native redundancy audit remain 8d3c.

## Implementation

- Removed all 292 engine Go files (195 implementation, 97 standalone test files). 307 tracked files are deleted in total. The accepted 8d3a ledger retains the 262 audited entry-point dispositions; native/shared replacements remain executable. Removed the completed one-time migration tool and its now-historical source-existence guard.
- Closed all 13 bridge importer files: 11 files deleted; modelbinding/nested.go and testkit/factory/factory.go retain only native operations. Removed indirect attachments.AddFiles. Native validated Foundry capabilities and model identity protocols remain.
- Removed the compatibility-only orm.Mutate constructor. Removed its private option storage and configuration path too. Named model mutators retain ordinary-column rejection coverage, and metadata capability tests reject empty codecs and unissued capabilities.
- Replaced the 8d1 dependency allowlist with internal/retirement.TestRetiredDataLayerAbsent, reached through root make test and make verify. The scanner parses every Go file across module boundaries without build selection, testdata, vendor or source exemptions. It rejects retired directories/imports, Legacy-prefixed identifiers, retired filenames, exported deprecations, and declarations/references in the denial ledger. Aliased and escaped imports cannot hide removed-path aliases.
- Negative fixtures cover ordinary/test/ignored/compile-fail/nested-module/tool imports, dot/blank/renamed/escaped imports, directories, identifiers, filenames, generic/chained aliases, exported/grouped/spec deprecations, neutral-file bridges, forwarding methods and imported bridge aliases. Every denial-ledger entry has a generated negative. Historical .go.txt data is not executable source.
- Eight aggregate compile-fail inputs had stale orm.Load import anchors; changed only the anchor to LoadFrom, preserving each intended negative expression and diagnostic.
- Docs-check rejects retired API spellings in current guides, package READMEs and AGENTS.md; only labelled, fenced migration-before examples may contain them. Historical build measurements moved to evidence; current recommendations use native owners. Historical source links are explicitly labelled rather than dangling.
- Synthetic generation defaults to native declarations. Historical generation requires an explicit separate checkout, whose source fingerprints remain recorded. Migrated the metadata editor expectation from the removed key method to the native package function, with a separate completion/hover/definition probe. Scaffold and release runtime paths already use native declarations; generation discovery no longer loads the retired package.

## Surface and evidence

Exact snapshot subtractions (three; no unrelated changes):

```text
orm.LegacyIdentityScope func[M any, K comparable](d github.com/weiloon1234/Foundry-Go/database/orm.ModelIdentity[M, K], keys ...K) github.com/weiloon1234/Foundry-Go/database/query.Predicate[M]
orm.Extension func[M, S any](slot github.com/weiloon1234/Foundry-Go/database/orm.Slot[M, S], source github.com/weiloon1234/Foundry-Go/database/query.ExtensionSlot[M]) github.com/weiloon1234/Foundry-Go/database/orm.ExtensionRelation[M]
orm.Mutate func[V any](fn func(V) (V, error)) github.com/weiloon1234/Foundry-Go/database/orm.ColumnOption[V]
```
Native-only convertibility exceptions remain; all pairs involving the removed package were subtracted.

All 552 fixture census source hashes match. No fixture test was removed or changed; the eight negative inputs are outside that comparison census. Frozen records and receipts are unchanged.

## Footprint

148 identical probes and unchanged ceilings, measured on the same Go toolchain/settings against an isolated archive of the base. 53 shrink; 95 are unchanged; none grow. Full per-probe counts: [footprint evidence](goway-stage8d3b-footprint.json).

| Probe | Base | Current | Ceiling |
| --- | ---: | ---: | ---: |
| extension_declaration | 11,396 | 8,680 | 35,000 |
| extension_matching | 8,481 | 5,683 | 9,704 |
| aggregate_families | 4,339 | 4,331 | 4,369 |
| q_with | 1,173 | 1,173 | 1,211 |

These are archive function symbols before linker elimination, not runtime speed. Removing the owner query bridge breaks its generic engine reachability; native-only probes need not shrink merely because an unreachable package is deleted. Removing stored-transform option machinery accounts for smaller column/input-family archives.

## Verification

| Check | Result |
| --- | --- |
| Root build and vet | PASS; focused vet repeated after final guard/editor changes |
| Root make test | PASS, including the absence guard and native assertion owners |
| Touched package races, PostgreSQL required | PASS; ORM/expr/rows, pruning, attachments/extensions/metadata/translations, modelbinding/factory, generator, querycore, guard and docs owner |
| Four fixture modules: build, vet, tests/replay | PASS; final build and make fixture-check after compatibility cleanup |
| Catalogue | PASS: 1,032 rows, 587 ported, 445 unaffected; 582 distinct native cases |
| Footprint | PASS: all 148 ceilings unchanged; measured base/current above |
| Generate-check | PASS; retained generated/contract outputs unchanged |
| Release tools | PASS: Go vet/tests and 37 Python tests |
| Docs-check | PASS |
| Agent smoke | PASS, segmented: full three-probe run had one stale metadata completion expectation; corrected value and replacement matching probes pass under race; all other probes passed unchanged |
| Formatting | PASS: make fmt-check and clean diff whitespace |

The initial release policy rejected a standalone .txt evidence artifact; the exact subtraction lines now live in this report using the existing supported Markdown format. Docs failures identified stale removed-method guidance, now corrected. No checks were skipped or deferred to integration.
Environment: `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r19 GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`; PostgreSQL required for execution gates.

## Docs notes

Lead-owned blueprint index/master/Go-way overview: replace current coexistence claims with native-only handwritten declarations and shared internal SQL owners; keep dated historical acceptance evidence. CHANGELOG: retire the engine, extension/identity bridges, generated binding/factory/pruning adapters and compatibility-only Mutate option; native semantics and stored identities remain. The cursor contract-ID change belongs to 8d3c.

Literal removed-path text remains in historical evidence, historical synthetic templates, rejection assertions, and the explicitly deferred cursor wire ID/protected artifacts. There are no executable imports or removed-package directory. A literal grep is not yet empty because of that deferred wire contract; do not rewrite it or frozen evidence in this slice.

## Review 46 correction (superseded by review 47 below)

The guard now rejects deprecated anonymous exported fields, including generic pointer embeddings. Its method-denial pass resolves aliases with `go/types` and checks both value and pointer method sets, including promoted members. All collected source files participate, including build-ignored files; unresolved declarations fail closed. The ledger still determines the public owner/type/member, so unrelated types may use the same method name.

New negatives cover anonymous fields, generic aliases and alias chains, value/pointer promotion, and build-ignored alias declarations. A positive keeps an unrelated type's `Not` method valid. Guard and docs-owner race tests pass on r19. The affected root `make test` (retirement and checkdocs packages), `make docs-check` and `make fmt-check` also pass on r19; exact scoped commands are in the accompanying evidence. Runtime APIs, footprint ceilings, fixture census, frozen records and surface subtractions are unchanged by this correction.

## Review 47 correction

Replaced the method-only pass with one exposure walker over every package's `go/types` scope: exported types (including aliases), variables, constants and functions. It checks exposed selectors against the denial ledger using value/pointer method sets, interface methods, and direct/promoted fields resolved with `LookupFieldOrMethod`, preserving shadowing and ambiguity. Source comments remain attached to their declarations; the walker follows aliases, fields, embeddings, signatures and containers to find exposed deprecations wherever declared in the tree. Anonymous interface embeddings participate despite having no field object.

All source files still participate, including intentionally invalid compiler inputs and mutually exclusive build files. Their ordinary typing diagnostics belong to the compiler-case/build gates; the exposure pass visits their resolved declarations. Import resolution failures stop the check. The guard doc comment names `database/orm/testdata/public-methods.txt` and `database/orm/method_surface_test.go` as the exact-surface backstop beyond this check.

Regressions cover all three reported shapes, interface/pointer variables, constant methods, alias and pointer-promoted fields, hidden type/method/field deprecations, function results, anonymous interface embeddings, and imported comments. Positive cases preserve private unexposed declarations, unrelated selectors and ambiguous promotions. Existing negatives remain. PASS on r19: guard tests and all negatives (including final negatives under race), affected root `make test`, `make docs-check`, and `make fmt-check`. Exact commands are recorded in the JSON evidence. No runtime, fixture census, frozen record, footprint or public-snapshot changes.

## Review 48 final guard correction

The shared interface-annotation helper now runs throughout recursive embedding traversal as well as the main exposure walk, including annotations on generic origins. New regressions cover nested interfaces, generic instances and generic aliases.

A variant-independent syntactic pass uses the denial ledger to inspect every field/method declaration on the denied type or its local alias closure across all files. It preserves competing declarations instead of relying on the single object retained by `go/types`. Negatives cover the two-variant `datatable.Config` case, direct and alias fields, cross-file alias chains, generic methods and interface methods. A positive accepts benign mutually exclusive declarations; ordinary type-check errors remain nonfatal.

The guard doc comment records the lead-approved residual limit: exposure assembled only across mutually exclusive build variants is outside the semantic pass. The exact snapshot remains its stated backstop, and the final 8d3c audit reviews the public surface. No runtime, census, frozen evidence, footprint or public snapshot changed. PASS on r19: guard tests and negatives, affected root `make test`, `make docs-check`, and `make fmt-check`. Exact commands are in the JSON evidence.

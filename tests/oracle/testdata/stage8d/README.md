# Framework legacy observations

These 41 records were captured from the legacy branches of the seven framework
comparisons (and statement/binding retention checks) at `e7b4d8a4`. Identity and
plan aliases remain live native-to-owner assertions. The records cover five report
queries, ten keyed-write outcomes, ten bounded record streams, four extension
loading cases, four identity/token/SQL cases, six field method signatures and two
retention binding/statement cases.

`records.json` uses the existing `tests/oracle` format. `state.json` is the existing
publication receipt. Every observation includes its actual command, Go build
environment, commit and complete source-manifest digest. `sources/` retains the
instrumented capture files as text; overlay those files on the recorded commit to
reproduce the legacy observations. Unarchived inputs are identified by the commit
and their manifest hashes. No archived file is compiled by ordinary tests.

Capture used r15, GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off, GOENV=off and the local
toolchain. Each capture call receives the legacy value; both sides still passed
their original assertions before publication. The exact command is in provenance.
The stream subtest label `legacy` remains an observation identifier; its current
implementation runs only native rows terminals.

Publication, from `tests/oracle`, was explicitly authorized by Stage 8d decision 4:

```sh
go run ./cmd/freeze-oracles --freeze --input /private/tmp/goway-8d2-capture/bundle.json
```

The temporary input was assembled from the emitted observations, not from native
results. The publisher delegates validation and atomic publication to the shared
owner. Ordinary replay cannot publish or succeed against pending/missing evidence.
`TestFrameworkCaptureIntegrity` also verifies archived source hashes; replay tracks
every selected occurrence and fails on omitted assertions.

The independent [extension classification capture](../stage8d-matching/README.md)
records a later command without rewriting this capture's source manifest.

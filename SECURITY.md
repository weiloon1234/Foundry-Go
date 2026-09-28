# Security policy

## Supported versions

Foundry-Go is currently pre-release. Security corrections target the current
maintained development branch; historical private candidates are not supported
release lines. When public versions are published, maintainers must list their
supported ranges here. This policy does not announce a release or a response-time
SLA. The [compatibility policy](docs/compatibility.md) owns API rollout rules.

## Report privately

Use [GitHub private vulnerability reporting](https://github.com/weiloon1234/Foundry-Go/security/advisories/new)
for this repository. Include the affected revision, reproducible steps or a small
private test, expected/actual behavior, impact and deployment prerequisites.
Do not include working credentials or unrelated personal/customer data.

Do not open a public issue containing exploit details. Coordinate disclosure and
fix validation privately with the maintainer. A report should distinguish a
framework defect from an application trust boundary or unsafe deployment option.

## Continuous checks

The repository workflow (`.github/workflows/verify.yml`) runs existing framework, consumer,
backend, editor/client and security gates on changes and a weekly schedule.
Third-party actions are pinned to reviewed commits, workflow permissions are
read-only, and checked-out credentials are not persisted. Ephemeral CI services
use only runner-local test data; production secrets are not required.

`make security-check` uses explicitly selected installed tools and preserves the
actual scanner findings. It fails on affected function findings, tool/incomplete
output errors, and npm vulnerabilities. Module/package-only reports remain in
its evidence for review. Scans cannot prove the absence of unknown defects.

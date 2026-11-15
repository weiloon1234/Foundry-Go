# Managed DNS live certification

On 2026-11-15, three live runs ran against the owner's Cloudflare test zone.
Zone, domain, and token details are omitted.

The first run failed with Cloudflare code 9313 because the ownership comment
exceeded 100 characters. The fix uses a 75-character comment: `foundry:v1:`
followed by a 64-character SHA-256 digest of the instance, ownership, and creation
operation identities.

Two later runs passed create, update, verify, and deletion of owned records only.
Both runs left no test records behind. Cleanup checks the creation receipt,
random hostname, and ownership comment before deleting a provider record ID.
The lead ran these against the owner's test zone from the lane commits that
were merged here.

The runs loaded `FOUNDRY_TEST_CLOUDFLARE_ZONE_ID`, `FOUNDRY_TEST_CLOUDFLARE_API_TOKEN`
and `FOUNDRY_TEST_CLOUDFLARE_DOMAIN` from a private mode-0600 environment file,
never printing them, and ran from the repository root:

```sh
cd tools/deploy/tests
set -a; . ~/.foundry-secrets/<private-test>.env; set +a
FOUNDRY_TEST_CLOUDFLARE_REQUIRED=1 python3 -B -m unittest -v test_dns_live
```

# Bundled country reference

`seed.json` is the 250-entry country snapshot copied verbatim from the supplied
Rust Foundry reference's `src/countries/seed.json`. Its SHA-256 is
`8839a7f4c6a0406dc772be081cf2e442a28170e280befe0e454411e001b06d58`.
The reference repository does not record an upstream dataset version in this
file. Foundry-Go identifies this fixed input as `foundry-countries-v1`; it is not
a claim that every administrative, currency or geographic field is current.

`iana-zone-2026a.tab` is the reference's pinned IANA tzdb 2026a country/zone map,
copied verbatim with its public-domain notice. SHA-256:
`586b4207e6c76722de82adcda6bf49d761f668517f45a673f64da83b333eecc4`.
Country seeding uses these named zones. It retains the reference's explicit
`XK` mapping to `Europe/Belgrade`; `BV` and `HM` have empty arrays.

The seeder reads embedded files only. Data upgrades require a reviewed version
change and an explicit seeder invocation. Existing activation status, default
selection and conversion rates are application-managed and preserved.

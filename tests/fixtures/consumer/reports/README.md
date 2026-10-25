# Shared report schemas

The original result structs and package paths remain unchanged. Use these
handwritten rows declarations with `rows.Select` and `rows.Map`/`MapSelected`:

| Result | Schema |
| --- | --- |
| UserSummary | UserSummaries |
| BuyerTotals | BuyerTotalRows |
| OrderSummary | OrderSummaries |
| ReferralRow | ReferralRows |
| UserPair | UserPairs |
| OrderBuyerRow | OrderBuyerRows |
| ReferralOrderRow | ReferralOrderRows |
| LocationRow | LocationRows |
| UserOrderStats | UserOrderStatsRows |
| ScalarReport | ScalarReports |

`UserSummary.Email` retains its `contact_email` SQL alias. Nullable fields,
model-owned IDs, enum codecs and exact decimal aggregates keep their types.
Legacy projection directives and generated APIs remain until the lead's cleanup
barrier; the SQL parity tests retain legacy calls as comparison oracles.

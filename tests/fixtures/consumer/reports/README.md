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
The cleanup barrier has removed generation for the seven join/subquery result
types. Their handwritten schemas above are the supported SQL entry points.
`UserSummary`, `BuyerTotals` and `OrderSummary` still retain generated projection
oracles until their SQL/binding expectations are frozen; their structs and rows
schemas do not change at that boundary.

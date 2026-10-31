# Expression testdata migration and footprint follow-up — 2026-10-31

Audited all 1,949 active Go files under every repository `testdata` directory, including all tracked testdata Go files. Migrated 103 receiver-resolved calls across 29 goway footprint probes with `tools/exprmigrate`, using retained pre-migration export metadata and current source fingerprints. Reviewed and migrated another 13 compile-negative cases whose deliberately invalid inputs prevent complete receiver resolution (nine goway, four consumer). The invalid conversions and query arguments are unchanged. No archived `.go.txt` sources, frozen evidence, generated output, compiler diagnostic expectations, or footprint budgets changed.

Final forms used: `expr.AsSelected`, `expr.ParamOf`, `expr.Filter`, `expr.Key`, `expr.EqValue`, `expr.GtValue`, `expr.IsNotNullValue`, `expr.AscValue`, and `expr.DescValue`. Retained `ValueSetQuery.Value`, `ValueCursorQuery.Value`, `JoinOn.Not`, ORM table keys, and websocket registration methods belong to exported types and remain valid.

The measurements below precede the subsequent phase-transition rename to `expr.AsSelected`. The footprint tests were not rerun for that rename; these numbers describe the earlier follow-up.

## Verification

Both authorized tests passed on Go 1.27.3 darwin/arm64, using the existing native Go cache, one successful run per module:

```sh
cd tests/fixtures/goway
go test -count=1 -v -run '^TestFootprintBudgets$' .
cd ../consumer
go test -count=1 -v -run '^TestFootprintBudgets$' .
```

The initial sandboxed goway invocation failed to open native cache files. The successful run used approved cache access. No other tests ran. Compile-negative harnesses were not rerun.

The existing harnesses compile the actual probes using `go list -export`, then count defined archive symbols with `go tool nm`; they reject failed compilation or measurement. These are package archive measurements, not runtime performance or linked binary sizes. Comparisons are against the recorded v0.2 baselines from 2026-10-30 across the current worktree, rather than an isolated measurement of this caller-only follow-up. The machine had 157 GiB free before running; load averages were 5.50/6.67/5.17. Core-count inspection was denied by the sandbox. Timing is not used for any claim.

## Increases and budget decisions

Goway: 148 probes measured; 106 have fewer functions, 40 are unchanged, and two have more. Consumer: all seven function counts are unchanged; three endpoint probes have more total symbols and archive bytes. Every measurement remains within its existing ceiling. **All budgets remain unchanged.**

| Fixture / probe | Metric | Recorded baseline → current | Existing ceiling |
| --- | --- | ---: | ---: |
| goway / `framework_application_control` | functions | 3003 → 3013 | 3454 |
| goway / `framework_registration` | functions | 3135 → 3145 | 3606 |
| consumer / `endpoint_plain` | symbols | 3669 → 3673 | 4220 |
| consumer / `endpoint_plain` | archive bytes | 2122528 → 2124834 | 2440908 |
| consumer / `endpoint_ordinary` | symbols | 5321 → 5329 | 6120 |
| consumer / `endpoint_ordinary` | archive bytes | 3265298 → 3267884 | 3755093 |
| consumer / `endpoint_authenticated` | symbols | 7198 → 7206 | 8278 |
| consumer / `endpoint_authenticated` | archive bytes | 4569480 → 4572106 | 5254902 |

Consumer baseline counts are recovered exactly from the documented ceiling formula `ceil(baseline × 1.15)`; each ceiling has a unique integer baseline, and the recovered function/symbol counts agree with the table in `consumer/footprint_test.go`. The historical goway table does not retain archive-byte baselines, so no before/after byte comparison is claimed there.

## Goway measurements

| Probe | Baseline functions | Current functions | Existing ceiling | Current archive bytes |
| --- | ---: | ---: | ---: | ---: |
| `aggregate_families` | 3623 | 2382 | 4167 | 10505982 |
| `aggregate_group` | 2168 | 1528 | 2494 | 6515300 |
| `aggregate_slot_declaration` | 608 | 569 | 700 | 2354442 |
| `aggregate_slot_direct` | 1074 | 961 | 1236 | 3745584 |
| `aggregate_slot_nested` | 1187 | 1074 | 1366 | 4159534 |
| `aggregate_slot_pivot` | 1220 | 1107 | 1403 | 4365586 |
| `audit` | 793 | 793 | 912 | 3604332 |
| `column_encrypted_json` | 795 | 693 | 915 | 2872120 |
| `column_encrypted_text` | 705 | 604 | 811 | 2594774 |
| `column_enum` | 785 | 683 | 903 | 2820000 |
| `column_interval` | 706 | 606 | 812 | 2612636 |
| `column_json` | 821 | 699 | 945 | 2902852 |
| `column_nullable_encrypted_json` | 1022 | 869 | 1176 | 3777544 |
| `column_nullable_encrypted_text` | 931 | 779 | 1071 | 3466826 |
| `column_nullable_interval` | 931 | 779 | 1071 | 3477052 |
| `column_nullable_json` | 1061 | 892 | 1221 | 3797882 |
| `column_nullable_password` | 962 | 797 | 1107 | 3493382 |
| `column_nullable_text` | 955 | 802 | 1099 | 3248698 |
| `column_number` | 673 | 570 | 774 | 2398764 |
| `column_password` | 737 | 623 | 848 | 2668710 |
| `column_time` | 723 | 622 | 832 | 2560220 |
| `conditional_row` | 1216 | 1003 | 1399 | 4300438 |
| `conditional_selected` | 1237 | 971 | 1423 | 4120342 |
| `conflict` | 474 | 474 | 546 | 2195546 |
| `conflict_advanced` | 1327 | 798 | 1527 | 3889668 |
| `correlation` | 1993 | 1357 | 2292 | 5875406 |
| `cte_recursive` | 885 | 865 | 1018 | 3908264 |
| `cursor` | 533 | 533 | 613 | 2296744 |
| `cursor_terminal` | 1198 | 894 | 1378 | 3714974 |
| `datatable_projection` | 2236 | 1982 | 2572 | 8344250 |
| `derived_aggregates` | 2172 | 1443 | 2498 | 5909174 |
| `distinct` | 823 | 669 | 947 | 2977810 |
| `each_soft_writes` | 987 | 834 | 1136 | 3470992 |
| `each_writes` | 283 | 283 | 326 | 1379838 |
| `expr_arithmetic` | 882 | 697 | 1015 | 2983976 |
| `expr_selected` | 1284 | 1015 | 1477 | 4139340 |
| `extension_declaration` | 5795 | 5756 | 6665 | 28516562 |
| `extension_files` | 1479 | 1479 | 1701 | 6348652 |
| `extension_handles` | 968 | 900 | 1114 | 3915750 |
| `extension_matching` | 3553 | 3553 | 4086 | 18214572 |
| `extension_slots` | 2109 | 2109 | 2426 | 9537764 |
| `framework_application_control` | 3003 | 3013 | 3454 | 14980980 |
| `framework_attachmentstore` | 3032 | 2427 | 3487 | 10469256 |
| `framework_auditstore` | 4077 | 3192 | 4689 | 13840450 |
| `framework_challengestore` | 1394 | 1190 | 1604 | 5494178 |
| `framework_countries` | 3380 | 2839 | 3887 | 11712102 |
| `framework_extensionstore` | 3209 | 2564 | 3691 | 11869010 |
| `framework_idempotencystore` | 1698 | 1411 | 1953 | 5922608 |
| `framework_jobarchive` | 1514 | 1227 | 1742 | 5248428 |
| `framework_mfastore` | 2072 | 1660 | 2383 | 6967280 |
| `framework_notificationstore` | 2780 | 2301 | 3197 | 10354840 |
| `framework_outbox` | 2055 | 1665 | 2364 | 7108390 |
| `framework_pagination` | 2144 | 2108 | 2466 | 11263312 |
| `framework_registration` | 3135 | 3145 | 3606 | 15968388 |
| `framework_registration_option` | 475 | 475 | 547 | 3432438 |
| `framework_seeder` | 266 | 266 | 306 | 1305870 |
| `framework_sessionstore` | 2523 | 2063 | 2902 | 8725852 |
| `framework_tokenstore` | 2849 | 2355 | 3277 | 9871980 |
| `framework_webhookstore` | 2539 | 2100 | 2920 | 9132754 |
| `global_scopes` | 491 | 491 | 565 | 2123988 |
| `has` | 870 | 767 | 1001 | 3398578 |
| `has_one` | 887 | 784 | 1021 | 3438050 |
| `input_column_kinds` | 3795 | 3069 | 4365 | 12746232 |
| `input_key` | 600 | 532 | 690 | 2469280 |
| `json_functions` | 2424 | 1754 | 2788 | 7183784 |
| `json_paths` | 5344 | 3770 | 6146 | 15698824 |
| `key_chunk` | 489 | 489 | 563 | 2191138 |
| `key_clauses` | 1700 | 1269 | 1955 | 5607436 |
| `lateral` | 2043 | 1536 | 2350 | 7352756 |
| `lifecycle_changes` | 558 | 558 | 642 | 2239848 |
| `lifecycle_write` | 391 | 391 | 450 | 1786882 |
| `load_query` | 599 | 599 | 689 | 2590472 |
| `model_type` | 299 | 299 | 344 | 1251676 |
| `models` | 4098 | 3526 | 4713 | 15249870 |
| `models_1` | 1048 | 861 | 1206 | 3525522 |
| `models_3` | 1178 | 991 | 1355 | 4119624 |
| `morph` | 638 | 638 | 734 | 2761538 |
| `mutator_explicit` | 989 | 836 | 1138 | 3401130 |
| `mutators` | 245 | 245 | 282 | 1149522 |
| `nested_with` | 892 | 789 | 1026 | 3477248 |
| `nullable_join_chain` | 1242 | 997 | 1429 | 4643706 |
| `observer_counts` | 63 | 63 | 73 | 208510 |
| `on_comparisons` | 966 | 696 | 1111 | 3029234 |
| `onequery` | 457 | 457 | 526 | 2036384 |
| `orm_binding` | 640 | 572 | 736 | 2365640 |
| `orm_factory` | 650 | 582 | 748 | 2577056 |
| `outbox_publisher` | 756 | 670 | 870 | 3227000 |
| `outer_join` | 1213 | 968 | 1395 | 4391154 |
| `pilot_expressions` | 1222 | 975 | 1406 | 3924620 |
| `pivot` | 652 | 652 | 750 | 2919180 |
| `prune_registry` | 320 | 320 | 368 | 1469102 |
| `q_join` | 774 | 694 | 891 | 3128882 |
| `q_select` | 500 | 500 | 575 | 2168386 |
| `q_with` | 551 | 551 | 634 | 2379282 |
| `raw` | 693 | 627 | 797 | 2803006 |
| `raw_fragments` | 1066 | 890 | 1226 | 3745170 |
| `read_record` | 562 | 562 | 647 | 2550072 |
| `read_scalar` | 761 | 675 | 876 | 2845044 |
| `read_set` | 648 | 648 | 746 | 2961298 |
| `record_distinct_on` | 1089 | 911 | 1253 | 4447194 |
| `record_sets` | 1134 | 1072 | 1305 | 4938160 |
| `records` | 1106 | 1030 | 1272 | 4771944 |
| `related_value` | 1173 | 982 | 1349 | 4010298 |
| `relation_expression_order` | 1943 | 1523 | 2235 | 6430562 |
| `relation_filter` | 822 | 703 | 946 | 3035976 |
| `relation_with` | 1038 | 935 | 1194 | 3929900 |
| `relation_write_attach` | 620 | 620 | 713 | 2796960 |
| `relation_write_declaration` | 361 | 361 | 416 | 1499496 |
| `relation_write_sync` | 625 | 625 | 719 | 2844638 |
| `report_filters` | 1204 | 915 | 1385 | 3781598 |
| `report_pipeline` | 1077 | 925 | 1239 | 4837586 |
| `report_reads` | 701 | 691 | 807 | 2996092 |
| `result_clauses` | 1534 | 1105 | 1765 | 4621400 |
| `result_cursor` | 1258 | 954 | 1447 | 4273540 |
| `result_reads` | 1021 | 925 | 1175 | 4063444 |
| `retrieval_read` | 364 | 364 | 419 | 1655510 |
| `role_input_same` | 742 | 640 | 854 | 2583188 |
| `role_lookup_input` | 599 | 531 | 689 | 2217930 |
| `role_primary_number` | 590 | 522 | 679 | 2203036 |
| `role_primary_text` | 591 | 522 | 680 | 2192406 |
| `role_soft_read` | 953 | 800 | 1096 | 3357220 |
| `role_soft_write` | 262 | 262 | 302 | 1146336 |
| `role_source` | 569 | 397 | 655 | 1803730 |
| `role_time_read` | 742 | 641 | 854 | 2652584 |
| `rows_each` | 626 | 626 | 720 | 2680722 |
| `scalar_temporal` | 2348 | 1563 | 2701 | 6675188 |
| `scalar_text` | 1824 | 1280 | 2098 | 5096210 |
| `scoped_write` | 481 | 481 | 554 | 2100418 |
| `sensitive_models` | 1884 | 1552 | 2167 | 7126980 |
| `source_clauses` | 1848 | 1295 | 2126 | 5996504 |
| `source_write` | 790 | 544 | 909 | 2607762 |
| `subqueries` | 2110 | 1459 | 2427 | 5913082 |
| `table_ref` | 449 | 449 | 517 | 2037122 |
| `terminal_adjustment` | 592 | 592 | 681 | 2711838 |
| `terminal_create_or_first` | 279 | 279 | 321 | 1371950 |
| `terminal_explain` | 652 | 652 | 750 | 2679460 |
| `terminal_lookup` | 308 | 308 | 355 | 1556432 |
| `terminal_prune` | 317 | 317 | 365 | 1458912 |
| `through` | 571 | 571 | 657 | 2487432 |
| `transaction_correlation` | 1922 | 1462 | 2211 | 7312784 |
| `transaction_joins` | 1555 | 1271 | 1789 | 6050234 |
| `transaction_records` | 1094 | 989 | 1259 | 4343226 |
| `uuid_seeks` | 653 | 532 | 751 | 2459062 |
| `validation_database` | 746 | 744 | 858 | 3195574 |
| `value_sets` | 1501 | 1131 | 1727 | 4711426 |
| `window_frames` | 1488 | 1182 | 1712 | 4996206 |
| `window_rank` | 1061 | 922 | 1221 | 3921058 |
| `write_unset` | 171 | 171 | 197 | 774940 |

## Consumer measurements

| Probe | Functions / ceiling | Symbols / ceiling | Archive bytes / ceiling |
| --- | ---: | ---: | ---: |
| `model_type` | 915 / 1053 | 5846 / 6723 | 3720172 / 4279107 |
| `model_isolated` | 106 / 122 | 799 / 919 | 454658 / 522974 |
| `model_assignments` | 200 / 230 | 1398 / 1608 | 882340 / 1015395 |
| `endpoint_plain` | 454 / 523 | 3673 / 4220 | 2124834 / 2440908 |
| `endpoint_ordinary` | 695 / 800 | 5329 / 6120 | 3267884 / 3755093 |
| `endpoint_authenticated` | 982 / 1130 | 7206 / 8278 | 4572106 / 5254902 |
| `dto_validated` | 376 / 433 | 2744 / 3156 | 1888706 / 2172424 |

## Files changed in this follow-up

The following 42 Go files were migrated and formatted. This report is the only additional file; the list excludes inherited changes elsewhere in the worktree.

- [tests/fixtures/consumer/testdata/compilefail/transaction_correlated_value_ordinary/invalid.go](../../tests/fixtures/consumer/testdata/compilefail/transaction_correlated_value_ordinary/invalid.go)
- [tests/fixtures/consumer/testdata/compilefail/transaction_correlated_value_unbound/invalid.go](../../tests/fixtures/consumer/testdata/compilefail/transaction_correlated_value_unbound/invalid.go)
- [tests/fixtures/consumer/testdata/compilefail/transaction_value_ordinary_anchor/invalid.go](../../tests/fixtures/consumer/testdata/compilefail/transaction_value_ordinary_anchor/invalid.go)
- [tests/fixtures/consumer/testdata/compilefail/transaction_value_scalar_escape/invalid.go](../../tests/fixtures/consumer/testdata/compilefail/transaction_value_scalar_escape/invalid.go)
- [tests/fixtures/goway/testdata/compilefail/correlation_scalar_ordinary/invalid.go](../../tests/fixtures/goway/testdata/compilefail/correlation_scalar_ordinary/invalid.go)
- [tests/fixtures/goway/testdata/compilefail/scalar_nullable_input/invalid.go](../../tests/fixtures/goway/testdata/compilefail/scalar_nullable_input/invalid.go)
- [tests/fixtures/goway/testdata/compilefail/set_record_scalar/invalid.go](../../tests/fixtures/goway/testdata/compilefail/set_record_scalar/invalid.go)
- [tests/fixtures/goway/testdata/compilefail/text_comparison_nullable_owner_conversion/invalid.go](../../tests/fixtures/goway/testdata/compilefail/text_comparison_nullable_owner_conversion/invalid.go)
- [tests/fixtures/goway/testdata/compilefail/text_comparison_owner_conversion/invalid.go](../../tests/fixtures/goway/testdata/compilefail/text_comparison_owner_conversion/invalid.go)
- [tests/fixtures/goway/testdata/compilefail/transaction_correlated_value_ordinary/invalid.go](../../tests/fixtures/goway/testdata/compilefail/transaction_correlated_value_ordinary/invalid.go)
- [tests/fixtures/goway/testdata/compilefail/transaction_correlated_value_unbound/invalid.go](../../tests/fixtures/goway/testdata/compilefail/transaction_correlated_value_unbound/invalid.go)
- [tests/fixtures/goway/testdata/compilefail/transaction_value_ordinary_anchor/invalid.go](../../tests/fixtures/goway/testdata/compilefail/transaction_value_ordinary_anchor/invalid.go)
- [tests/fixtures/goway/testdata/compilefail/transaction_value_scalar_escape/invalid.go](../../tests/fixtures/goway/testdata/compilefail/transaction_value_scalar_escape/invalid.go)
- [tests/fixtures/goway/testdata/footprint/aggregate_families/probe.go](../../tests/fixtures/goway/testdata/footprint/aggregate_families/probe.go)
- [tests/fixtures/goway/testdata/footprint/aggregate_group/probe.go](../../tests/fixtures/goway/testdata/footprint/aggregate_group/probe.go)
- [tests/fixtures/goway/testdata/footprint/conditional_row/probe.go](../../tests/fixtures/goway/testdata/footprint/conditional_row/probe.go)
- [tests/fixtures/goway/testdata/footprint/conditional_selected/probe.go](../../tests/fixtures/goway/testdata/footprint/conditional_selected/probe.go)
- [tests/fixtures/goway/testdata/footprint/conflict_advanced/probe.go](../../tests/fixtures/goway/testdata/footprint/conflict_advanced/probe.go)
- [tests/fixtures/goway/testdata/footprint/correlation/probe.go](../../tests/fixtures/goway/testdata/footprint/correlation/probe.go)
- [tests/fixtures/goway/testdata/footprint/cursor_terminal/probe.go](../../tests/fixtures/goway/testdata/footprint/cursor_terminal/probe.go)
- [tests/fixtures/goway/testdata/footprint/datatable_projection/probe.go](../../tests/fixtures/goway/testdata/footprint/datatable_projection/probe.go)
- [tests/fixtures/goway/testdata/footprint/derived_aggregates/probe.go](../../tests/fixtures/goway/testdata/footprint/derived_aggregates/probe.go)
- [tests/fixtures/goway/testdata/footprint/distinct/probe.go](../../tests/fixtures/goway/testdata/footprint/distinct/probe.go)
- [tests/fixtures/goway/testdata/footprint/expr_arithmetic/probe.go](../../tests/fixtures/goway/testdata/footprint/expr_arithmetic/probe.go)
- [tests/fixtures/goway/testdata/footprint/expr_selected/probe.go](../../tests/fixtures/goway/testdata/footprint/expr_selected/probe.go)
- [tests/fixtures/goway/testdata/footprint/json_functions/probe.go](../../tests/fixtures/goway/testdata/footprint/json_functions/probe.go)
- [tests/fixtures/goway/testdata/footprint/json_paths/probe.go](../../tests/fixtures/goway/testdata/footprint/json_paths/probe.go)
- [tests/fixtures/goway/testdata/footprint/key_clauses/probe.go](../../tests/fixtures/goway/testdata/footprint/key_clauses/probe.go)
- [tests/fixtures/goway/testdata/footprint/lateral/probe.go](../../tests/fixtures/goway/testdata/footprint/lateral/probe.go)
- [tests/fixtures/goway/testdata/footprint/pilot_expressions/probe.go](../../tests/fixtures/goway/testdata/footprint/pilot_expressions/probe.go)
- [tests/fixtures/goway/testdata/footprint/read_scalar/p.go](../../tests/fixtures/goway/testdata/footprint/read_scalar/p.go)
- [tests/fixtures/goway/testdata/footprint/result_clauses/probe.go](../../tests/fixtures/goway/testdata/footprint/result_clauses/probe.go)
- [tests/fixtures/goway/testdata/footprint/result_cursor/probe.go](../../tests/fixtures/goway/testdata/footprint/result_cursor/probe.go)
- [tests/fixtures/goway/testdata/footprint/result_reads/probe.go](../../tests/fixtures/goway/testdata/footprint/result_reads/probe.go)
- [tests/fixtures/goway/testdata/footprint/scalar_temporal/probe.go](../../tests/fixtures/goway/testdata/footprint/scalar_temporal/probe.go)
- [tests/fixtures/goway/testdata/footprint/scalar_text/probe.go](../../tests/fixtures/goway/testdata/footprint/scalar_text/probe.go)
- [tests/fixtures/goway/testdata/footprint/source_clauses/probe.go](../../tests/fixtures/goway/testdata/footprint/source_clauses/probe.go)
- [tests/fixtures/goway/testdata/footprint/subqueries/probe.go](../../tests/fixtures/goway/testdata/footprint/subqueries/probe.go)
- [tests/fixtures/goway/testdata/footprint/transaction_correlation/probe.go](../../tests/fixtures/goway/testdata/footprint/transaction_correlation/probe.go)
- [tests/fixtures/goway/testdata/footprint/transaction_joins/probe.go](../../tests/fixtures/goway/testdata/footprint/transaction_joins/probe.go)
- [tests/fixtures/goway/testdata/footprint/value_sets/probe.go](../../tests/fixtures/goway/testdata/footprint/value_sets/probe.go)
- [tests/fixtures/goway/testdata/footprint/window_frames/probe.go](../../tests/fixtures/goway/testdata/footprint/window_frames/probe.go)

## Open items

No caller migration or budget failure remains. The measured increases above are retained for the lead; no budget was raised. The thirteen edited negative cases still contain their original invalid operations, but their harnesses were outside the authorized verification for this follow-up.

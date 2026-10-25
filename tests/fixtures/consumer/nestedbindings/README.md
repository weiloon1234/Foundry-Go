# nestedbindings consumer

`TeamRecords`, `ProjectRecords` and `TaskRecords` preserve stored keys, soft-delete visibility and relation slots. `TeamLinks` and `ProjectLinks` scope children through their parent. Alternate slug bindings use stored text columns, `modelbinding.ByColumn` and `ThroughSelectedRelation`.

The original query counts, ambiguous-key failures, parent scoping, retrieval vetoes, eager loads and request/resource authorization checks remain. Generated relationship declarations stay until the lead's retirement barrier.

PostgreSQL sources remain opt-in; the lead runs the live service gate.

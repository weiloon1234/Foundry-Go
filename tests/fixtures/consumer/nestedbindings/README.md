# nestedbindings consumer

`TeamRecords`, `ProjectRecords` and `TaskRecords` preserve stored keys, soft-delete visibility and relation slots. `TeamLinks` and `ProjectLinks` scope children through their parent. Alternate slug bindings use stored text columns, `modelbinding.ByColumn` and `ThroughSelectedRelation`.

The original query counts, ambiguous-key failures, parent scoping, retrieval vetoes, eager loads and request/resource authorization checks remain. Model directives generate the tables; explicit relation declarations and the named retrieval factory retain their behavior. Team's handwritten `FoundryIdentity` retains authenticated binding identity.

PostgreSQL sources remain opt-in; the lead runs the live service gate.

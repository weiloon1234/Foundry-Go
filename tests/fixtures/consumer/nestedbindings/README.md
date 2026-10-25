# nestedbindings consumer

`TeamRecords`, `ProjectRecords` and `TaskRecords` preserve stored keys, soft-delete visibility and relation slots. `TeamLinks` and `ProjectLinks` scope children through their parent. Alternate slug bindings use stored text columns, `modelbinding.ByColumn` and `ThroughSelectedRelation`.

The original query counts, ambiguous-key failures, parent scoping, retrieval vetoes, eager loads and request/resource authorization checks remain. Legacy relationship/retrieval factories and model-generation directives are removed. Team's handwritten `FoundryIdentity` retains authenticated binding identity.

PostgreSQL sources remain opt-in; the lead runs the live service gate.

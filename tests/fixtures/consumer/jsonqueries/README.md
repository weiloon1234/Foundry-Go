# jsonqueries

`Documents` and `Policies` retain the model and JSON natural-key identities. Payload fields are declared next to them with `expr.JSONFieldOf`; traversal uses typed property, map-key and index steps. Missing paths, SQL NULL and JSON null remain distinct. `Snapshots` maps the retained DTO through the shared `database.JSONKind` identity; both query packages expose aliases. No DTO field declaration or wire bytes change.

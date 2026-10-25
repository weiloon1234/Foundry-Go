# authenticating consumer

Providers, proofs, session/token metadata and resource bindings use shared ORM declarations and their typed identity codecs. Request scopes retain their existing snapshots; new scopes re-resolve current authority. Permission, eligibility, credential and transport assertions remain intact. No model is implicitly a response DTO.

PostgreSQL sources remain opt-in; the lead runs the live service gate.

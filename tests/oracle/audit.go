package oracle

// ComparisonAudit is the historical capture census. Its PostgreSQL entry points
// remain the source of backend requirements for replay; source hashes describe
// the retired checkout and are not regenerated from replacement declarations.
type ComparisonAudit struct {
	Files map[string]AuditedSource `json:"files"`
}
type AuditedSource struct {
	PostgresTests []string `json:"postgres_tests,omitempty"`
	SHA256        string   `json:"sha256"`
	Tests         []string `json:"paired_tests,omitempty"`
}

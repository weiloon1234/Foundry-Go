package querycore

// Column describes one persisted column at the generated declaration boundary.
// DatabaseDefault permits omission of a non-nullable field on insert; the actual
// SQL default expression remains owned by the migration, never duplicated here.
// Encrypted marks a database/encrypted field, bound to the row's primary key.
type Column struct {
	Name            string
	Nullable        bool
	DatabaseDefault bool
	Encrypted       bool
}

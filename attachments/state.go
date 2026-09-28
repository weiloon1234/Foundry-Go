package attachments

// State is persisted, public diagnostic state, not a caller-controlled upload
// option. Writing/Uncertain require explicit settlement; a timeout is not proof
// that a remote storage request has stopped.
type State string

const (
	Writing        State = "writing"
	Stored         State = "stored"
	Ready          State = "ready"
	CleanupPending State = "cleanup"
	Cleaned        State = "cleaned"
	Retained       State = "retained"
	Uncertain      State = "uncertain"
)

func (s State) Validate() error {
	switch s {
	case Writing, Stored, Ready, CleanupPending, Cleaned, Uncertain, Retained:
		return nil
	}
	return invalid()
}

// Settlement is an operator assertion, after inspecting the writer and storage
// provider, that no request can still publish this upload. It is never inferred
// from a lease, timestamp, canceled context or one missing-object response.
type Settlement uint8

const WriterStoppedAndStorageSettled Settlement = 1

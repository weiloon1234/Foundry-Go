package outbox

// PublicationState describes transfer to a durable delivery authority, not
// successful execution of its eventual handler. Failure remains inspectable.
type PublicationState string

const (
	Pending           PublicationState = "pending"
	Published         PublicationState = "published"
	PublicationFailed PublicationState = "failed"
)

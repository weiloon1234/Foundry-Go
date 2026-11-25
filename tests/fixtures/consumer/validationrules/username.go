package validationrules

// doc-example:begin username-message-arguments
// UsernameArgs supplies the field label to the application's username-format message.
//
//foundry:message key=validation.username
type UsernameArgs struct {
	Attribute string `json:"attribute"`
}

// doc-example:end username-message-arguments

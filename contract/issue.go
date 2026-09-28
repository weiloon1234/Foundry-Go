package contract

import "github.com/weiloon1234/Foundry-Go/i18n"

// IssueCode identifies a wire-shape failure independently of a public message.
// Business validation rules add their own metadata at the validation boundary.
type IssueCode string

const (
	TypeIssue     IssueCode = "type"
	KeyIssue      IssueCode = "key"
	NullIssue     IssueCode = "null"
	RequiredIssue IssueCode = "required"
	UnknownIssue  IssueCode = "unknown"
	ValueIssue    IssueCode = "value"
	LengthIssue   IssueCode = "length"
)

// Issue identifies a location by JSON Pointer (the empty string is the root).
// Unknown object fields produce one issue at the containing object; their
// submitted names are not reflected into diagnostics. Declared field names,
// array indices and declared map keys can appear in Path. Values never do.
type Issue struct {
	Path string    `json:"path"`
	Code IssueCode `json:"code"`
	// Message is approved public text supplied by a declared validation rule.
	// Wire-shape failures leave it empty. Never copy input or error text here.
	Message string `json:"message,omitempty"`
	// Label is an optional, declared display name. Path remains the API field
	// location; labels never substitute for paths or contain submitted values.
	Label    string          `json:"label,omitempty"`
	LabelKey i18n.MessageKey `json:"label_key,omitempty"`
}

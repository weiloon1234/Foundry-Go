package pagination

import "github.com/weiloon1234/Foundry-Go/value"

// NumberedMeta preserves the ORM's exact numbered-page metadata, including zero
// last_page for an empty count. Separate count/read operations are not a snapshot.
type NumberedMeta struct {
	Number int   `json:"current_page"`
	Size   int   `json:"per_page"`
	Total  int64 `json:"total"`
	Pages  int64 `json:"last_page"`
}

// SimpleMeta retains navigation information without claiming a total count.
type SimpleMeta struct {
	Number  int  `json:"current_page"`
	Size    int  `json:"per_page"`
	HasMore bool `json:"has_more"`
}

// Links contains optional next/previous URLs. A link is a navigation hint, not
// a guarantee that data will still exist when another request is executed.
type Links struct {
	Next     value.Nullable[string] `json:"next"`
	Previous value.Nullable[string] `json:"prev"`
}

// NumberedResponse contains explicit DTO values and shared page metadata.
type NumberedResponse[T any] struct {
	Data  []T          `json:"data"`
	Meta  NumberedMeta `json:"meta"`
	Links Links        `json:"links"`
}

// SimpleResponse contains explicit DTO values and count-free metadata.
type SimpleResponse[T any] struct {
	Data  []T        `json:"data"`
	Meta  SimpleMeta `json:"meta"`
	Links Links      `json:"links"`
}

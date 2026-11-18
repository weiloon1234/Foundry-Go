package routecontract

import "testing"

func TestHelperNames(t *testing.T) {
	for _, test := range []struct{ declaration, suffix, want string }{
		{"SavePageRequest", "Body", "SavePageBody"},
		{"PageResource", "Response", "PageResponse"},
		{"UserResponse", "Response", "UserResponseResponse"},
		{"RequestMetadata", "Body", "RequestMetadataBody"},
		{"Member", "Response", "MemberResponse"},
		{"Request", "Body", "Body"},
		{"PageResourceRequest", "Body", "PageResourceBody"},
	} {
		if got := HelperName(test.declaration, test.suffix); got != test.want {
			t.Errorf("%s + %s = %s, want %s", test.declaration, test.suffix, got, test.want)
		}
	}
}

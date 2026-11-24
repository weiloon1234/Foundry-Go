package modelmeta

import "testing"

func TestIdentityVariable(t *testing.T) {
	for table, want := range map[string]string{"Users": "usersIdentity", "Pages": "pagesIdentity", "APIKeys": "apiKeysIdentity", "IDs": "iDsIdentity", "URL": "urlIdentity", "users": "usersIdentity"} {
		if got := IdentityVariable(table); got != want {
			t.Errorf("%s: %s != %s", table, got, want)
		}
	}
}
func TestFingerprint(t *testing.T) {
	fields := []FieldTag{{"ID", ""}, {"Name", `foundry:"column=label" json:"name"`}}
	want := Fingerprint(fields)
	for _, other := range [][]FieldTag{
		{{"ID", ""}, {"Name", `foundry:"column=other"`}},
		{{"ID", `foundry:"member=ID"`}, {"Name", fields[1].Tag}},
		{{"ID", ""}, {"Name", ""}},
		{fields[1], fields[0]},
		{{"Other", ""}, fields[1]},
	} {
		if Fingerprint(other) == want {
			t.Fatal("changed declaration has same fingerprint")
		}
	}
	fields[1].Tag = `foundry:"column=label" json:"other"`
	if Fingerprint(fields) != want {
		t.Fatal("transport tag affects model fingerprint")
	}
}

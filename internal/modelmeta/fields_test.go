package modelmeta

import "testing"

func TestParseTags(t *testing.T) {
	tags, err := ParseTags(`json:"name,omitempty" foundry:"column=display_name,default=database"`)
	if err != nil || tags["foundry"] != "column=display_name,default=database" || tags["json"] != "name,omitempty" {
		t.Fatalf("tags=%v err=%v", tags, err)
	}
	for _, raw := range []string{`foundry:"x`, `foundry:"x" foundry:"y"`, `foundry:x`, `foundry:"x"garbage`} {
		if _, err := ParseTags(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}
func TestSnake(t *testing.T) {
	for input, want := range map[string]string{"ID": "id", "BuyerID": "buyer_id", "HTTPServer": "http_server", "CreatedAt": "created_at", "URLValue": "url_value"} {
		if got := Snake(input); got != want {
			t.Errorf("Snake(%q)=%q want %q", input, got, want)
		}
	}
}

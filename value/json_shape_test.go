package value_test

import (
	"testing"

	"github.com/weiloon1234/Foundry-Go/value"
)

func TestJSONTagNamesStayUnambiguous(t *testing.T) {
	type valid struct {
		Odd     string `json:"odd%path"`
		Unicode string `json:"名前"`
	}
	original := valid{Odd: "quote'_%", Unicode: "日本語"}
	snapshot, err := value.NewJSON(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := snapshot.Decode()
	if err != nil || decoded != original {
		t.Fatal(decoded, err)
	}
	type malformed struct {
		Odd string `json:"odd'path"`
	}
	if _, err := value.NewJSON(malformed{Odd: "x"}); err == nil {
		t.Fatal("malformed JSON tag accepted")
	}
	if _, err := value.ParseJSON[malformed](`{"odd":"x"}`); err == nil {
		t.Fatal("partial malformed tag accepted")
	}
}

package docsource

import (
	"github.com/weiloon1234/Foundry-Go/testkit"
	"testing"
)

func TestRepositoryGuideSources(t *testing.T) {
	testkit.TrackExternalInputs(t)
	documents, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) == 0 {
		t.Fatal("no guide markdown files found")
	}
	if err := Check(documents); err != nil {
		t.Fatal(err)
	}
	if err := CheckTypeScriptInputs("../..", documents); err != nil {
		t.Fatal(err)
	}
}

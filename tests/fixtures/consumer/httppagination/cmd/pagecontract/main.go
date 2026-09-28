// pagecontract proves generated DTO source identities in a real executable.
package main

import (
	"context"
	"os"

	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

//foundry:dto
type Summary struct {
	Label string `json:"label"`
}

func main() {
	descriptor := pagination.NumberedJSON(SummaryJSON())
	info, err := descriptor.Description()
	if err != nil {
		panic(err)
	}
	item, _ := SummaryJSON().Description()
	found := false
	for _, node := range info.Types {
		if node.ID == item.Root {
			found = true
		}
	}
	if !found || item.Root != "foundry.test/consumer/httppagination/cmd/pagecontract.Summary" {
		panic("main DTO source identity was lost")
	}
	data, err := descriptor.Encode(context.Background(), pagination.NumberedResponse[Summary]{Data: []Summary{{Label: "main DTO"}}, Meta: pagination.NumberedMeta{Number: 1, Size: 20, Total: 1, Pages: 1}}, foundryhttp.DefaultEndpointLimits().Response)
	if err != nil {
		panic(err)
	}
	if _, err = os.Stdout.Write(data); err != nil {
		panic(err)
	}
}

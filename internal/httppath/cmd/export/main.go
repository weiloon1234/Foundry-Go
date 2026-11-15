// Command export publishes the router syntax for the standalone deploy bundle.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"

	"github.com/weiloon1234/Foundry-Go/internal/httppath"
)

func main() {
	check := flag.Bool("check", false, "check the exported syntax without writing it")
	flag.Parse()
	data, err := json.MarshalIndent(httppath.Syntax(), "", "  ")
	if err != nil {
		panic(err)
	}
	data = append(data, '\n')
	const output = "../../tools/deploy/lib/http-path-syntax.json"
	if *check {
		current, err := os.ReadFile(output)
		if err != nil || !bytes.Equal(current, data) {
			panic("HTTP path syntax is stale; run go generate ./internal/httppath")
		}
		return
	}
	if err := os.WriteFile(output, data, 0644); err != nil {
		panic(err)
	}
}

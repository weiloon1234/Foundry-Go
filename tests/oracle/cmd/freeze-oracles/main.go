// Command freeze-oracles publishes a reviewed framework legacy capture bundle.
// Capture sources and their exact command are retained alongside the evidence.
package main

import (
	"flag"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/tests/oracle"
	"os"
)

func main() {
	freeze := flag.Bool("freeze", false, "publish reviewed legacy observations")
	input := flag.String("input", "", "captured bundle with source provenance")
	directory := flag.String("directory", "testdata/stage8d", "reviewed capture destination")
	flag.Parse()
	if !*freeze || *input == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "use --freeze --input captured-bundle.json from tests/oracle")
		os.Exit(1)
	}
	var bundle oracle.Bundle
	if err := oracle.ReadJSON(*input, &bundle); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := oracle.Publish(*directory, bundle, false); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

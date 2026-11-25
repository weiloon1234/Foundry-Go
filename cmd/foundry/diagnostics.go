package main

import (
	"fmt"
	"io"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docrefs"
)

// applicationSkills reads the skills of the application module containing dir once.
// Without such a module there are no skills to name.
func applicationSkills(dir string) diagnostic.Skills {
	root, err := documentationModule(dir)
	if err != nil {
		return nil
	}
	return docrefs.ScanSkills(root)
}

// printFindings writes one finding per line, naming the application skills that
// declare each rule's guide anchor. Skills are read only when there are findings.
func printFindings(stdout io.Writer, dir string, findings []diagnostic.Diagnostic) error {
	if len(findings) == 0 {
		return nil
	}
	for _, line := range diagnostic.Lines(findings, applicationSkills(dir)) {
		if _, err := fmt.Fprintln(stdout, line); err != nil {
			return err
		}
	}
	return nil
}

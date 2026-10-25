package oracle

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// CountSymbols is shared by the live footprint gate and the legacy recorder.
func CountSymbols(data []byte) (functions int, definedSymbols int, auditFunctions int, err error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		var symType string
		if len(fields) >= 2 && len(fields[1]) == 1 {
			symType = fields[1]
		} else if len(fields[0]) == 1 {
			symType = fields[0]
		} else {
			return 0, 0, 0, fmt.Errorf("unrecognized nm symbol format: %q", scanner.Text())
		}
		if symType != "U" {
			definedSymbols++
			if symType == "T" || symType == "t" {
				functions++
				if strings.Contains(scanner.Text(), "/audit/record.") || strings.Contains(scanner.Text(), "/database/orm.Audit") {
					auditFunctions++
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, 0, fmt.Errorf("scanning nm output: %w", err)
	}
	return functions, definedSymbols, auditFunctions, nil
}

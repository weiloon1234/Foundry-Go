package main

import (
	"regexp"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/docsource"
)

var referenceDefinition = regexp.MustCompile(`^[ \t]{0,3}\[([^\]]+)\]:[ \t]*(<[^>]+>|[^ \t]+)(?:[ \t]+.*)?$`)
var referenceLink = regexp.MustCompile(`\[([^\]]+)\](?:\[([^\]]*)\])?`)

func referenceLabel(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(label), " "))
}

func linkDefinitions(data []byte) map[string]string {
	definitions := make(map[string]string)
	fence := ""
	for _, line := range strings.Split(string(data), "\n") {
		if fence != "" {
			if docsource.ClosesFence(line, fence) {
				fence = ""
			}
			continue
		}
		if marker, _ := docsource.FenceStart(line); marker != "" {
			fence = marker
			continue
		}
		if match := referenceDefinition.FindStringSubmatch(line); match != nil {
			label := referenceLabel(match[1])
			if _, exists := definitions[label]; !exists {
				definitions[label] = match[2]
			}
		}
	}
	return definitions
}

// Resolve full, collapsed and shortcut references before using the same path
// and fragment checks as inline links. Unused definitions are not navigation.
func linkDestinations(line string, definitions map[string]string) []string {
	if referenceDefinition.MatchString(line) {
		return nil
	}
	var targets []string
	for _, match := range inlineLink.FindAllStringSubmatch(line, -1) {
		targets = append(targets, match[1])
	}
	line = inlineLink.ReplaceAllString(line, "")
	for _, match := range referenceLink.FindAllStringSubmatch(line, -1) {
		label := match[2]
		if label == "" {
			label = match[1]
		}
		if target, exists := definitions[referenceLabel(label)]; exists {
			targets = append(targets, target)
		}
	}
	return targets
}

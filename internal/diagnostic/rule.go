package diagnostic

import "sort"

// Rule is one check's finding kind. Its ID is stable machine-read output; Guide and
// Anchor name the framework guide page and explicit anchor that teach the fix.
type Rule struct {
	ID     string
	Guide  string
	Anchor string
}

// Reference is the `guide#anchor` token the message prints and an application skill
// declares in a `Foundry guides:` line.
func (r Rule) Reference() string { return r.Guide + "#" + r.Anchor }

// Skills maps each `guide#anchor` reference to the application skills declaring it.
type Skills map[string][]string

// Names returns the skills that declare the rule's anchor, in name order.
func (s Skills) Names(rule Rule) []string { return s[rule.Reference()] }

// Add records that skill declares reference. Names stay sorted and unique.
func (s Skills) Add(reference, skill string) {
	names := s[reference]
	i := sort.SearchStrings(names, skill)
	if i < len(names) && names[i] == skill {
		return
	}
	names = append(names, "")
	copy(names[i+1:], names[i:])
	names[i] = skill
	s[reference] = names
}

const (
	tooling = "developer-tooling-and-testing"
	authors = "recipe-author-examples"
	pinned  = "pinned-documentation-checks"
	limits  = "document-limits"
	guides  = "recipe-reference-guides"
)

// The rules of every check. Each guide anchor is validated by the framework's
// documentation gate (see Rules).
var (
	RouteNesting       = Rule{"route-nesting", "flat-routes", "staged-routes"}
	RouteHelper        = Rule{"route-helper", "flat-routes", "generated-helpers"}
	RouteHelperMissing = Rule{"route-helper-missing", "flat-routes", "generated-helpers"}
	ModelTable         = Rule{"model-table", "model-declarations", "recipe-declare-table"}
	RawSQL             = Rule{"raw-sql", "database-runtime", "no-raw-policy"}
	GeneratedStale     = Rule{"generated-stale", "model-declarations", "recipe-stale-declarations"}
	GenerateDecl       = Rule{"generate-declaration", "model-declarations", "diagnose-declarations"}

	GuidesDeclaration = Rule{"guides-declaration", tooling, guides}
	AnchorInvalid     = Rule{"anchor-invalid", tooling, authors}
	AnchorDuplicate   = Rule{"anchor-duplicate", tooling, authors}
	AnchorMissing     = Rule{"anchor-missing", tooling, guides}
	GuideName         = Rule{"guide-name", tooling, guides}
	GuideMissing      = Rule{"guide-missing", tooling, guides}
	RetiredCommand    = Rule{"retired-command", tooling, pinned}
	RetiredAPI        = Rule{"retired-api", tooling, pinned}
	AmbiguousAPI      = Rule{"ambiguous-api", tooling, pinned}
	DocLimit          = Rule{"doc-limit", tooling, limits}
	DocFile           = Rule{"doc-file", tooling, pinned}
	DocComment        = Rule{"doc-comment", tooling, limits}

	DocRendered       = Rule{"doc-rendered", tooling, authors}
	DocMarker         = Rule{"doc-marker", tooling, authors}
	DocRegion         = Rule{"doc-region", tooling, authors}
	DocBinding        = Rule{"doc-binding", tooling, authors}
	DocSectionAnchor  = Rule{"doc-section-anchor", tooling, authors}
	DocAgentEntry     = Rule{"doc-agent-entry", tooling, authors}
	DocInputs         = Rule{"doc-inputs", tooling, authors}
	DocLink           = Rule{"doc-link", tooling, authors}
	DocMilestone      = Rule{"doc-milestone", tooling, authors}
	DocModelExample   = Rule{"doc-model-example", "model-declarations", "recipe-declare-table"}
	FixtureGoVersion  = Rule{"fixture-go-version", tooling, "documentation-gate"}
	BlueprintNumbered = Rule{"blueprint-numbered", tooling, "documentation-gate"}
	RuleCatalogue     = Rule{"rule-catalogue", tooling, "diagnostic-rules"}
)

// Rules lists every rule once. The framework's documentation gate checks that each
// guide and anchor exists and that the tooling guide lists each ID.
func Rules() []Rule {
	return []Rule{
		RouteNesting, RouteHelper, RouteHelperMissing, ModelTable, RawSQL, GeneratedStale, GenerateDecl,
		GuidesDeclaration, AnchorInvalid, AnchorDuplicate, AnchorMissing, GuideName, GuideMissing,
		RetiredCommand, RetiredAPI, AmbiguousAPI, DocLimit, DocFile, DocComment,
		DocRendered, DocMarker, DocRegion, DocBinding, DocSectionAnchor, DocAgentEntry, DocInputs,
		DocLink, DocMilestone, DocModelExample, FixtureGoVersion, BlueprintNumbered, RuleCatalogue,
	}
}

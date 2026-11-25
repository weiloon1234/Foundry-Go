package docrefs

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
)

const skillsDirectory = ".claude/skills"

// SkillIndex maps each `guide#anchor` that an application skill declares to the
// skills declaring it. Only `.claude/skills/<name>/SKILL.md` declarations count,
// and a declaration without an anchor names no anchor.
func SkillIndex(refs []Reference) diagnostic.Skills {
	skills := make(diagnostic.Skills)
	for _, ref := range refs {
		rest, found := strings.CutPrefix(ref.File, skillsDirectory+"/")
		name, file, nested := strings.Cut(rest, "/")
		if !found || !nested || file != "SKILL.md" || ref.Anchor == "" {
			continue
		}
		skills.Add(ref.Guide+"#"+ref.Anchor, name)
	}
	return skills
}

// ScanSkills reads the application's skill declarations once. Anything it cannot
// read is skipped: reading and judging skills is the job of `foundry docs check`,
// and a finding must print without them. Work is bounded by markdown.MaxDocuments
// skills and one run budget.
func ScanSkills(root string) diagnostic.Skills {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(skillsDirectory)))
	if err != nil {
		return nil
	}
	cache := markdown.NewCache()
	var budget markdown.Budget
	var refs []Reference
	for i, entry := range entries {
		if i == markdown.MaxDocuments {
			break
		}
		if !entry.IsDir() {
			continue
		}
		name := skillsDirectory + "/" + entry.Name() + "/SKILL.md"
		data, err := markdown.ReadFile(root, name, markdown.DocumentLimit(name))
		if err != nil || budget.Take(name, len(data)) != nil {
			continue
		}
		parsed, _ := Parse(cache, name, data)
		refs = append(refs, parsed...)
	}
	return SkillIndex(refs)
}

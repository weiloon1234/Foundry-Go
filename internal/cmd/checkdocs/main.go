// Command checkdocs checks local documentation links and repository consistency.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/diagnostic"
	"github.com/weiloon1234/Foundry-Go/internal/docrefs"
	"github.com/weiloon1234/Foundry-Go/internal/docsource"
	"github.com/weiloon1234/Foundry-Go/internal/docsource/markdown"
	"github.com/weiloon1234/Foundry-Go/internal/retirement"
)

var (
	blueprintName = regexp.MustCompile(`^([0-9]{2})-.+\.md$`)
)

func main() {
	flags := flag.NewFlagSet("checkdocs", flag.ContinueOnError)
	inputs := flags.String("go-inputs", "", "check guide coverage against fixture go list JSON")
	if err := docsource.ParseFlags(flags, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, docsource.Diagnostics(err))
		os.Exit(1)
	}

	if err := run(".", *inputs); err != nil {
		fmt.Fprintln(os.Stderr, docsource.Diagnostics(err))
		os.Exit(1)
	}
	fmt.Println("Documentation links, current guide milestone claims, blueprint numbering, and fixture Go requirements are valid.")
}

func run(root, inputs string) error {
	if inputs == "" {
		return errors.Join(checkRepository(root), checkRules(root))
	}
	documents, err := docsource.Load(root, nil)
	if err != nil {
		return err
	}
	file, err := markdown.OpenRegular(filepath.Dir(inputs), filepath.Base(inputs), os.O_RDONLY)
	if err != nil {
		return err
	}
	defer file.Close()
	return errors.Join(docsource.CheckBindings(documents), docsource.CheckGoInputs(root, documents, file))
}

func checkRepository(root string) error {
	// One parse per document serves the binding, anchor, retirement, reference
	// and link checks of the run; one budget bounds every document read.
	cache := markdown.NewCache()
	var budget markdown.Budget
	checker, err := retirement.NewDocumentChecker(true, cache)
	if err != nil {
		return err
	}
	targets := docrefs.NewPages(root, cache)
	var failures []error
	var references []docrefs.Reference
	agentLines := make(map[string]map[int]bool)
	documents, sourceErr := docsource.Load(root, cache)
	if sourceErr != nil {
		failures = append(failures, sourceErr)
	}
	failures = append(failures, docsource.CheckBindings(documents), docsource.Check(documents), docsource.CheckTypeScriptInputs(root, documents))
	for _, document := range documents {
		path := filepath.Join(root, filepath.FromSlash(document.Path))
		_, err := targets.Index(path, document.Original)
		failures = append(failures, err)
		lines := make(map[int]bool, len(document.AgentLines))
		for _, line := range document.AgentLines {
			lines[line] = true
		}
		agentLines[path] = lines
	}
	completed, err := completedMilestones(root, cache)
	if err != nil {
		failures = append(failures, err)
	}
	if err := checkBlueprints(root); err != nil {
		failures = append(failures, err)
	}
	version, err := goRequirement(filepath.Join(root, "go.mod"))
	if err != nil {
		failures = append(failures, err)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if strings.HasPrefix(entry.Name(), ".env") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			owned := relative == "docs/guides" || strings.HasPrefix(relative, "docs/guides/") || relative == ".claude/skills" || strings.HasPrefix(relative, ".claude/skills/")
			if !owned {
				switch entry.Name() {
				case ".git", ".cache", "node_modules", "vendor", ".deploy": // .deploy holds local deploy build copies
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			data, err := markdown.ReadFile(root, relative, markdown.DocumentLimit(relative))
			if err != nil {
				return err
			}
			if err := budget.Take(relative, len(data)); err != nil {
				return err
			}
			current := currentGuide(root, path)
			active := current || entry.Name() == "AGENTS.md" || entry.Name() == "README.md" && !strings.HasPrefix(relative, "blueprint/") && !strings.HasPrefix(relative, "docs/evidence/") && !strings.Contains(relative, "/testdata/")
			if active || strings.HasPrefix(relative, ".claude/skills/") {
				parsed, err := docrefs.Parse(cache, relative, data)
				references = append(references, parsed...)
				failures = append(failures, err, checkModelGuide(cache, relative, data), checker.Check(relative, data))
			}
			if err := checkDocument(cache, root, path, data, current, completed, agentLines[path], targets); err != nil {
				failures = append(failures, err)
			}
		}
		if entry.Name() == "go.mod" && path != filepath.Join(root, "go.mod") && version != "" {
			fixtureVersion, err := goRequirement(path)
			if err != nil {
				failures = append(failures, err)
			} else if fixtureVersion != version {
				failures = append(failures, diagnostic.FixtureGoVersion.At(path, goRequirementLine(path), 0).Say("go directive", fmt.Sprintf("%s where the root go.mod requires %s", fixtureVersion, version)).To("align the fixture go directive with the root go.mod").Err())
			}
		}
		return nil
	})
	if err != nil {
		failures = append(failures, err)
	}
	failures = append(failures, targets.CheckReferences(filepath.Join(root, "docs", "guides"), references))
	return errors.Join(failures...)
}

func checkBlueprints(root string) error {
	entries, err := os.ReadDir(filepath.Join(root, "blueprint"))
	if err != nil {
		return err
	}
	next := 0
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "README.md" || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		match := blueprintName.FindStringSubmatch(entry.Name())
		if match == nil {
			return diagnostic.BlueprintNumbered.At("blueprint/"+entry.Name(), 0, 0).Say("blueprint filename", "not NN-descriptive-name.md").To("rename it to NN-descriptive-name.md").Err()
		}
		number, _ := strconv.Atoi(match[1])
		if number != next {
			return diagnostic.BlueprintNumbered.At("blueprint/"+entry.Name(), 0, 0).Say("blueprint number", fmt.Sprintf("should be %02d (missing or duplicate number)", next)).To("restore sequential blueprint numbering").Err()
		}
		next++
	}
	if next == 0 {
		return diagnostic.BlueprintNumbered.At("blueprint", 0, 0).Say("blueprint directory", "no numbered blueprints").To("restore the numbered blueprint files").Err()
	}
	return nil
}

func goRequirement(path string) (string, error) {
	data, err := markdown.ReadPath(path, markdown.MaxManifestBytes)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "go" {
			return fields[1], nil
		}
	}
	return "", diagnostic.FixtureGoVersion.At(path, 0, 0).Say("go.mod", "no go directive").To("add the go directive matching the root go.mod").Err()
}

func checkDocument(cache *markdown.Cache, root, path string, data []byte, current bool, completed map[string]bool, agentLines map[int]bool, targets *docrefs.Pages) error {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	parsed, err := cache.Read(filepath.ToSlash(relative), data)
	if err != nil {
		return err
	}
	var failures []error
	if current {
		inlineIndex := 0
		for _, block := range parsed.Blocks {
			if block.Kind != markdown.Prose {
				continue
			}
			var prose strings.Builder
			for inlineIndex < len(parsed.Inlines) && parsed.Inlines[inlineIndex].Start < block.End {
				node := parsed.Inlines[inlineIndex]
				inlineIndex++
				if node.Kind == markdown.Text && node.Start >= block.Start && node.End <= block.End {
					prose.WriteString(node.Text)
					prose.WriteByte(' ')
				}
			}
			for _, match := range pendingMilestone.FindAllStringSubmatch(prose.String(), -1) {
				number := match[1]
				if number == "" {
					number = match[2]
				}
				if len(number) == 1 {
					number = "0" + number
				}
				if completed[number] {
					failures = append(failures, diagnostic.DocMilestone.At(path, block.First, 0).Say("future-work claim", fmt.Sprintf("names completed milestone %s", number)).To("describe its current API or move dated evidence to a dated record").Err())
				}
			}
		}
	}
	for _, node := range parsed.Inlines {
		if node.Kind != markdown.Link {
			continue
		}
		lineNumber := node.First
		destination := node.Destination
		target := strings.Trim(strings.TrimSpace(destination), "<>")
		link, err := url.Parse(target)
		if err != nil {
			failures = append(failures, diagnostic.DocLink.At(path, lineNumber, 0).Say(fmt.Sprintf("link %q", target), "not a valid URL").To("use a valid local path or URL").Caused(err))
			continue
		}
		explicit := link.Fragment == "agent-entry" || strings.HasPrefix(link.Fragment, "recipe-")
		if agentLines[lineNumber] && (!explicit || link.IsAbs() || link.Host != "") {
			failures = append(failures, diagnostic.DocLink.At(path, lineNumber, 0).Say(fmt.Sprintf("Agent entry link %q", target), "must target a local guide's explicit agent-entry or recipe-* anchor").To("select an existing task anchor").Err())
			continue
		}
		if link.IsAbs() || link.Host != "" || link.Path == "" && !explicit {
			continue
		}
		local := filepath.Join(filepath.Dir(path), filepath.FromSlash(link.Path))
		if link.Path == "" {
			local = path
		}
		relative, err := filepath.Rel(root, local)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			failures = append(failures, diagnostic.DocLink.At(path, lineNumber, 0).Say(fmt.Sprintf("local link %q", target), "targets a file outside the repository root").To("link to a file inside the repository").Err())
			continue
		}
		if historicalTeachingTarget(root, path, relative) {
			failures = append(failures, diagnostic.DocLink.At(path, lineNumber, 0).Say(fmt.Sprintf("teaching link %q", target), "targets historical material").To("link to the current task guide or keep an unlinked historical citation").Err())
			continue
		}
		if _, err := os.Stat(local); err != nil {
			failures = append(failures, diagnostic.DocLink.At(path, lineNumber, 0).Say(fmt.Sprintf("local link %q", target), "cannot be read").To("correct the link target or restore the missing file").Caused(err))
			continue
		}
		resolvedRoot, rootErr := filepath.EvalSymlinks(root)
		resolvedLocal, localErr := filepath.EvalSymlinks(local)
		if rootErr != nil || localErr != nil {
			failures = append(failures, diagnostic.DocLink.At(path, lineNumber, 0).Say(fmt.Sprintf("local link %q", target), "cannot be resolved").To("correct the link target or restore the missing file").Caused(errors.Join(rootErr, localErr)))
			continue
		}
		relative, err = filepath.Rel(resolvedRoot, resolvedLocal)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			failures = append(failures, diagnostic.DocLink.At(path, lineNumber, 0).Say(fmt.Sprintf("local link %q", target), "targets a file outside the repository root").To("link to a file inside the repository").Err())
			continue
		}
		if agentLines[lineNumber] && (filepath.Dir(local) != filepath.Join(root, "docs", "guides") || filepath.Ext(local) != ".md") {
			failures = append(failures, diagnostic.DocLink.At(path, lineNumber, 0).Say(fmt.Sprintf("Agent entry link %q", target), "must target a local guide").To("link to an existing task in docs/guides").Err())
			continue
		}
		if explicit {
			if err := checkExplicitFragment(targets, path, lineNumber, local, link.Fragment); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func historicalTeachingTarget(root, source, target string) bool {
	relative, err := filepath.Rel(root, source)
	if err != nil {
		return false
	}
	relative = filepath.ToSlash(relative)
	target = filepath.ToSlash(target)
	teaching := strings.HasPrefix(relative, "docs/guides/") || strings.HasPrefix(relative, ".claude/skills/")
	return teaching && (strings.HasPrefix(target, "docs/evidence/") || strings.HasPrefix(target, "blueprint/") || target == "CHANGELOG.md")
}

func goRequirementLine(path string) int {
	data, err := markdown.ReadPath(path, markdown.MaxManifestBytes)
	if err != nil {
		return 0
	}
	for i, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "go" {
			return i + 1
		}
	}
	return 0
}

// Package frameworkinfo owns the framework module identity used by development
// tooling. Go requirements remain owned by the selected module's go.mod.
package frameworkinfo

const ModulePath = "github.com/weiloon1234/Foundry-Go"
const GenerationManifest = ".foundry-gen.json"

// GenerationGuard is the persistent advisory-lock inode retained by generation.
const GenerationGuard = ".foundry-generate.guard"
const DeployEntry = "tools/deploy/local/cli.py"

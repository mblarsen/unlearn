package inventory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// skillInstall is the scanner/parser seam for one encountered install.
// Scanner owns root traversal; skillInstallParser owns interpreting the install
// as a Skill with referenced support material, token cost, activation risk, and
// provenance. Keeping this seam local gives the parser module more depth without
// exposing new public API.
type skillInstall struct {
	Root            string
	EncounteredPath string
	ResolvedPath    string
	PrimaryPath     string
	Kind            SkillKind
	IsSymlink       bool
	ReadOnly        bool
	Ownership       RootOwnership
	RootKnown       bool
}

type skillInstallParser struct {
	now func() time.Time
}

func newSkillInstallParser() skillInstallParser {
	return skillInstallParser{now: time.Now}
}

func (p skillInstallParser) Parse(install skillInstall) (Skill, error) {
	data, err := os.ReadFile(install.PrimaryPath)
	if err != nil {
		return Skill{}, err
	}

	front, body := ParseSkillMarkdown(data)
	name := front["name"]
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(install.EncounteredPath), filepath.Ext(install.EncounteredPath))
	}
	description := front["description"]

	refs := ExtractReferences(string(data))
	supportRefs, brokenRefs, supportTokens, supportHashParts := p.resolveSupportRefs(install, refs)
	lower := EstimateTokens(data)
	upper := lower + supportTokens
	activation := AssessActivationRisk(description, body)
	hashParts := [][]byte{data}
	hashParts = append(hashParts, supportHashParts...)

	return p.skill(install, name, skillContent{
		Description:           description,
		Frontmatter:           front,
		Body:                  body,
		SupportRefs:           supportRefs,
		BrokenRefs:            brokenRefs,
		LowerTokens:           lower,
		UpperTokens:           upper,
		ContentHash:           ContentHash(hashParts...),
		ActivationRisk:        activation.Risk,
		ActivationRiskSignals: activation.Signals,
	}), nil
}

func (p skillInstallParser) Unknown(install skillInstall, name string) Skill {
	return p.skill(install, name, skillContent{ActivationRisk: "unknown"})
}

func (p skillInstallParser) Broken(install skillInstall, name string) Skill {
	install.ReadOnly = true
	content := skillContent{ActivationRisk: "unknown"}
	skill := p.skill(install, name, content)
	skill.Broken = true
	return skill
}

type skillContent struct {
	Description           string
	Frontmatter           map[string]string
	Body                  string
	SupportRefs           []SupportRef
	BrokenRefs            []string
	LowerTokens           int
	UpperTokens           int
	ContentHash           string
	ActivationRisk        string
	ActivationRiskSignals []string
}

func (p skillInstallParser) skill(install skillInstall, name string, content skillContent) Skill {
	return Skill{
		ID:                    stableID(install.EncounteredPath, install.ResolvedPath),
		Name:                  name,
		Description:           content.Description,
		Kind:                  install.Kind,
		Root:                  install.Root,
		EncounteredPath:       install.EncounteredPath,
		ResolvedPath:          install.ResolvedPath,
		IsSymlink:             install.IsSymlink,
		ReadOnly:              install.ReadOnly,
		Frontmatter:           content.Frontmatter,
		Body:                  content.Body,
		PrimaryPath:           install.PrimaryPath,
		SupportRefs:           content.SupportRefs,
		BrokenRefs:            content.BrokenRefs,
		LowerTokens:           content.LowerTokens,
		UpperTokens:           content.UpperTokens,
		ContentHash:           content.ContentHash,
		ActivationRisk:        content.ActivationRisk,
		ActivationRiskSignals: content.ActivationRiskSignals,
		Provenance:            inferProvenance(install.Root, install.EncounteredPath, install.ResolvedPath, install.IsSymlink),
		RootKnown:             install.RootKnown,
		ActiveAgents:          append([]string(nil), install.Ownership.ActiveAgents...),
		InactiveAgents:        append([]string(nil), install.Ownership.InactiveAgents...),
		ScannedAt:             p.scannedAt(),
	}
}

func (p skillInstallParser) resolveSupportRefs(install skillInstall, refs []string) ([]SupportRef, []string, int, [][]byte) {
	supportRefs := make([]SupportRef, 0, len(refs))
	var brokenRefs []string
	var supportTokens int
	var hashParts [][]byte
	baseDir := install.EncounteredPath
	if install.Kind == KindMarkdown {
		baseDir = filepath.Dir(install.EncounteredPath)
	}
	for _, ref := range refs {
		refPath := filepath.Join(baseDir, ref)
		refData, err := os.ReadFile(refPath)
		item := SupportRef{Mention: ref, Path: refPath}
		if err != nil {
			item.Broken = true
			brokenRefs = append(brokenRefs, ref)
		} else {
			item.Tokens = EstimateTokens(refData)
			supportTokens += item.Tokens
			hashParts = append(hashParts, []byte(ref), refData)
		}
		supportRefs = append(supportRefs, item)
	}
	return supportRefs, brokenRefs, supportTokens, hashParts
}

func (p skillInstallParser) scannedAt() time.Time {
	if p.now == nil {
		return time.Now()
	}
	return p.now()
}

func stableID(encountered, resolved string) string {
	if resolved == "" {
		resolved = encountered
	}
	return ContentHash([]byte(encountered), []byte(resolved))[:16]
}

func isAgentsSkillsRoot(root string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return filepath.Clean(root) == filepath.Clean(filepath.Join(home, ".agents", "skills"))
}

func inferProvenance(root, encountered, resolved string, isSymlink bool) string {
	parts := []string{}
	if isSymlink {
		parts = append(parts, fmt.Sprintf("symlink to %s", resolved))
	}
	if strings.Contains(encountered, string(filepath.Separator)+"node_modules"+string(filepath.Separator)) {
		parts = append(parts, "npm package layout")
	}
	if _, err := os.Stat(filepath.Join(encountered, ".git")); err == nil {
		parts = append(parts, "local git checkout")
	}
	if isAgentsSkillsRoot(root) {
		parts = append(parts, "shared agents skills root")
		return strings.Join(parts, "; ")
	}
	for _, agent := range AgentCatalog() {
		for _, dir := range agent.GlobalSkillDirs() {
			if filepath.Clean(root) == filepath.Clean(dir) {
				parts = append(parts, strings.ToLower(agent.DisplayName)+" global skills root")
				return strings.Join(parts, "; ")
			}
		}
	}
	switch {
	case strings.Contains(root, filepath.Join(".pi", "agent", "skills")):
		parts = append(parts, "pi global skills root")
	case strings.Contains(root, filepath.Join(".agents", "skills")):
		parts = append(parts, "agents global skills root")
	case strings.Contains(root, filepath.Join(".codex", "skills")):
		parts = append(parts, "codex global skills root")
	case strings.Contains(root, filepath.Join("opencode", "skills")):
		parts = append(parts, "opencode global skills root")
	default:
		parts = append(parts, "user-provided root")
	}
	return strings.Join(parts, "; ")
}

package inventory

import (
	"os"
	"path/filepath"
	"sort"
)

type AgentDefinition struct {
	ID                    string
	DisplayName           string
	ProjectSkillsDir      string
	GlobalSkillsDir       string
	ExtraGlobalSkillsDirs []string
	UsesAgentsSkillsRoot  bool
	DetectPaths           []string
	ShowInSetup           bool
}

type AgentStatus struct {
	AgentDefinition
	Installed bool
}

type RootOwnership struct {
	ActiveAgents   []string
	InactiveAgents []string
}

func AgentCatalog() []AgentDefinition {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	codexHome := envOrDefault("CODEX_HOME", filepath.Join(home, ".codex"))
	claudeHome := envOrDefault("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	vibeHome := envOrDefault("VIBE_HOME", filepath.Join(home, ".vibe"))
	return []AgentDefinition{
		{ID: "aider-desk", DisplayName: "AiderDesk", ProjectSkillsDir: ".aider-desk/skills", GlobalSkillsDir: filepath.Join(home, ".aider-desk/skills"), DetectPaths: []string{filepath.Join(home, ".aider-desk")}, ShowInSetup: true},
		{ID: "amp", DisplayName: "Amp", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(configHome, "agents/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(configHome, "amp")}, ShowInSetup: true},
		{ID: "antigravity", DisplayName: "Antigravity", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(home, ".gemini/antigravity/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".gemini/antigravity")}, ShowInSetup: true},
		{ID: "augment", DisplayName: "Augment", ProjectSkillsDir: ".augment/skills", GlobalSkillsDir: filepath.Join(home, ".augment/skills"), DetectPaths: []string{filepath.Join(home, ".augment")}, ShowInSetup: true},
		{ID: "bob", DisplayName: "IBM Bob", ProjectSkillsDir: ".bob/skills", GlobalSkillsDir: filepath.Join(home, ".bob/skills"), DetectPaths: []string{filepath.Join(home, ".bob")}, ShowInSetup: true},
		{ID: "claude-code", DisplayName: "Claude Code", ProjectSkillsDir: ".claude/skills", GlobalSkillsDir: filepath.Join(claudeHome, "skills"), DetectPaths: []string{claudeHome}, ShowInSetup: true},
		{ID: "openclaw", DisplayName: "OpenClaw", ProjectSkillsDir: "skills", GlobalSkillsDir: openClawSkillsDir(home), DetectPaths: []string{filepath.Join(home, ".openclaw"), filepath.Join(home, ".clawdbot"), filepath.Join(home, ".moltbot")}, ShowInSetup: true},
		{ID: "cline", DisplayName: "Cline", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(home, ".agents/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".cline")}, ShowInSetup: true},
		{ID: "codearts-agent", DisplayName: "CodeArts Agent", ProjectSkillsDir: ".codeartsdoer/skills", GlobalSkillsDir: filepath.Join(home, ".codeartsdoer/skills"), DetectPaths: []string{filepath.Join(home, ".codeartsdoer")}, ShowInSetup: true},
		{ID: "codebuddy", DisplayName: "CodeBuddy", ProjectSkillsDir: ".codebuddy/skills", GlobalSkillsDir: filepath.Join(home, ".codebuddy/skills"), DetectPaths: []string{filepath.Join(home, ".codebuddy")}, ShowInSetup: true},
		{ID: "codemaker", DisplayName: "Codemaker", ProjectSkillsDir: ".codemaker/skills", GlobalSkillsDir: filepath.Join(home, ".codemaker/skills"), DetectPaths: []string{filepath.Join(home, ".codemaker")}, ShowInSetup: true},
		{ID: "codestudio", DisplayName: "Code Studio", ProjectSkillsDir: ".codestudio/skills", GlobalSkillsDir: filepath.Join(home, ".codestudio/skills"), DetectPaths: []string{filepath.Join(home, ".codestudio")}, ShowInSetup: true},
		{ID: "codex", DisplayName: "Codex", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(codexHome, "skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{codexHome, "/etc/codex"}, ShowInSetup: true},
		{ID: "command-code", DisplayName: "Command Code", ProjectSkillsDir: ".commandcode/skills", GlobalSkillsDir: filepath.Join(home, ".commandcode/skills"), DetectPaths: []string{filepath.Join(home, ".commandcode")}, ShowInSetup: true},
		{ID: "continue", DisplayName: "Continue", ProjectSkillsDir: ".continue/skills", GlobalSkillsDir: filepath.Join(home, ".continue/skills"), DetectPaths: []string{filepath.Join(home, ".continue")}, ShowInSetup: true},
		{ID: "cortex", DisplayName: "Cortex Code", ProjectSkillsDir: ".cortex/skills", GlobalSkillsDir: filepath.Join(home, ".snowflake/cortex/skills"), DetectPaths: []string{filepath.Join(home, ".snowflake/cortex")}, ShowInSetup: true},
		{ID: "crush", DisplayName: "Crush", ProjectSkillsDir: ".crush/skills", GlobalSkillsDir: filepath.Join(configHome, "crush/skills"), DetectPaths: []string{filepath.Join(configHome, "crush")}, ShowInSetup: true},
		{ID: "cursor", DisplayName: "Cursor", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(home, ".cursor/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".cursor")}, ShowInSetup: true},
		{ID: "deepagents", DisplayName: "Deep Agents", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(home, ".deepagents/agent/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".deepagents")}, ShowInSetup: true},
		{ID: "devin", DisplayName: "Devin for Terminal", ProjectSkillsDir: ".devin/skills", GlobalSkillsDir: filepath.Join(configHome, "devin/skills"), DetectPaths: []string{filepath.Join(configHome, "devin")}, ShowInSetup: true},
		{ID: "dexto", DisplayName: "Dexto", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(home, ".agents/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".dexto")}, ShowInSetup: true},
		{ID: "droid", DisplayName: "Droid", ProjectSkillsDir: ".factory/skills", GlobalSkillsDir: filepath.Join(home, ".factory/skills"), DetectPaths: []string{filepath.Join(home, ".factory")}, ShowInSetup: true},
		{ID: "firebender", DisplayName: "Firebender", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(home, ".firebender/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".firebender")}, ShowInSetup: true},
		{ID: "forgecode", DisplayName: "ForgeCode", ProjectSkillsDir: ".forge/skills", GlobalSkillsDir: filepath.Join(home, ".forge/skills"), DetectPaths: []string{filepath.Join(home, ".forge")}, ShowInSetup: true},
		{ID: "gemini-cli", DisplayName: "Gemini CLI", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(home, ".gemini/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".gemini")}, ShowInSetup: true},
		{ID: "github-copilot", DisplayName: "GitHub Copilot", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(home, ".copilot/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".copilot")}, ShowInSetup: true},
		{ID: "goose", DisplayName: "Goose", ProjectSkillsDir: ".goose/skills", GlobalSkillsDir: filepath.Join(configHome, "goose/skills"), DetectPaths: []string{filepath.Join(configHome, "goose")}, ShowInSetup: true},
		{ID: "hermes-agent", DisplayName: "Hermes Agent", ProjectSkillsDir: ".hermes/skills", GlobalSkillsDir: filepath.Join(home, ".hermes/skills"), DetectPaths: []string{filepath.Join(home, ".hermes")}, ShowInSetup: true},
		{ID: "junie", DisplayName: "Junie", ProjectSkillsDir: ".junie/skills", GlobalSkillsDir: filepath.Join(home, ".junie/skills"), DetectPaths: []string{filepath.Join(home, ".junie")}, ShowInSetup: true},
		{ID: "iflow-cli", DisplayName: "iFlow CLI", ProjectSkillsDir: ".iflow/skills", GlobalSkillsDir: filepath.Join(home, ".iflow/skills"), DetectPaths: []string{filepath.Join(home, ".iflow")}, ShowInSetup: true},
		{ID: "kilo", DisplayName: "Kilo Code", ProjectSkillsDir: ".kilocode/skills", GlobalSkillsDir: filepath.Join(home, ".kilocode/skills"), DetectPaths: []string{filepath.Join(home, ".kilocode")}, ShowInSetup: true},
		{ID: "kimi-cli", DisplayName: "Kimi Code CLI", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(configHome, "agents/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".kimi")}, ShowInSetup: true},
		{ID: "kiro-cli", DisplayName: "Kiro CLI", ProjectSkillsDir: ".kiro/skills", GlobalSkillsDir: filepath.Join(home, ".kiro/skills"), DetectPaths: []string{filepath.Join(home, ".kiro")}, ShowInSetup: true},
		{ID: "kode", DisplayName: "Kode", ProjectSkillsDir: ".kode/skills", GlobalSkillsDir: filepath.Join(home, ".kode/skills"), DetectPaths: []string{filepath.Join(home, ".kode")}, ShowInSetup: true},
		{ID: "mcpjam", DisplayName: "MCPJam", ProjectSkillsDir: ".mcpjam/skills", GlobalSkillsDir: filepath.Join(home, ".mcpjam/skills"), DetectPaths: []string{filepath.Join(home, ".mcpjam")}, ShowInSetup: true},
		{ID: "mistral-vibe", DisplayName: "Mistral Vibe", ProjectSkillsDir: ".vibe/skills", GlobalSkillsDir: filepath.Join(vibeHome, "skills"), DetectPaths: []string{vibeHome}, ShowInSetup: true},
		{ID: "mux", DisplayName: "Mux", ProjectSkillsDir: ".mux/skills", GlobalSkillsDir: filepath.Join(home, ".mux/skills"), DetectPaths: []string{filepath.Join(home, ".mux")}, ShowInSetup: true},
		{ID: "opencode", DisplayName: "OpenCode", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(configHome, "opencode/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(configHome, "opencode")}, ShowInSetup: true},
		{ID: "openhands", DisplayName: "OpenHands", ProjectSkillsDir: ".openhands/skills", GlobalSkillsDir: filepath.Join(home, ".openhands/skills"), DetectPaths: []string{filepath.Join(home, ".openhands")}, ShowInSetup: true},
		{ID: "pi", DisplayName: "Pi", ProjectSkillsDir: ".pi/skills", GlobalSkillsDir: filepath.Join(home, ".pi/agent/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".pi/agent")}, ShowInSetup: true},
		{ID: "qoder", DisplayName: "Qoder", ProjectSkillsDir: ".qoder/skills", GlobalSkillsDir: filepath.Join(home, ".qoder/skills"), DetectPaths: []string{filepath.Join(home, ".qoder")}, ShowInSetup: true},
		{ID: "qwen-code", DisplayName: "Qwen Code", ProjectSkillsDir: ".qwen/skills", GlobalSkillsDir: filepath.Join(home, ".qwen/skills"), DetectPaths: []string{filepath.Join(home, ".qwen")}, ShowInSetup: true},
		{ID: "replit", DisplayName: "Replit", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(configHome, "agents/skills"), DetectPaths: []string{filepath.Join(os.Getenv("PWD"), ".replit")}, ShowInSetup: false},
		{ID: "rovodev", DisplayName: "Rovo Dev", ProjectSkillsDir: ".rovodev/skills", GlobalSkillsDir: filepath.Join(home, ".rovodev/skills"), DetectPaths: []string{filepath.Join(home, ".rovodev")}, ShowInSetup: true},
		{ID: "roo", DisplayName: "Roo Code", ProjectSkillsDir: ".roo/skills", GlobalSkillsDir: filepath.Join(home, ".roo/skills"), DetectPaths: []string{filepath.Join(home, ".roo")}, ShowInSetup: true},
		{ID: "tabnine-cli", DisplayName: "Tabnine CLI", ProjectSkillsDir: ".tabnine/agent/skills", GlobalSkillsDir: filepath.Join(home, ".tabnine/agent/skills"), DetectPaths: []string{filepath.Join(home, ".tabnine")}, ShowInSetup: true},
		{ID: "trae", DisplayName: "Trae", ProjectSkillsDir: ".trae/skills", GlobalSkillsDir: filepath.Join(home, ".trae/skills"), DetectPaths: []string{filepath.Join(home, ".trae")}, ShowInSetup: true},
		{ID: "trae-cn", DisplayName: "Trae CN", ProjectSkillsDir: ".trae/skills", GlobalSkillsDir: filepath.Join(home, ".trae-cn/skills"), DetectPaths: []string{filepath.Join(home, ".trae-cn")}, ShowInSetup: true},
		{ID: "warp", DisplayName: "Warp", ProjectSkillsDir: ".agents/skills", GlobalSkillsDir: filepath.Join(home, ".agents/skills"), UsesAgentsSkillsRoot: true, DetectPaths: []string{filepath.Join(home, ".warp")}, ShowInSetup: true},
		{ID: "windsurf", DisplayName: "Windsurf", ProjectSkillsDir: ".windsurf/skills", GlobalSkillsDir: filepath.Join(home, ".codeium/windsurf/skills"), DetectPaths: []string{filepath.Join(home, ".codeium/windsurf")}, ShowInSetup: true},
		{ID: "zencoder", DisplayName: "Zencoder", ProjectSkillsDir: ".zencoder/skills", GlobalSkillsDir: filepath.Join(home, ".zencoder/skills"), DetectPaths: []string{filepath.Join(home, ".zencoder")}, ShowInSetup: true},
		{ID: "neovate", DisplayName: "Neovate", ProjectSkillsDir: ".neovate/skills", GlobalSkillsDir: filepath.Join(home, ".neovate/skills"), DetectPaths: []string{filepath.Join(home, ".neovate")}, ShowInSetup: true},
		{ID: "pochi", DisplayName: "Pochi", ProjectSkillsDir: ".pochi/skills", GlobalSkillsDir: filepath.Join(home, ".pochi/skills"), DetectPaths: []string{filepath.Join(home, ".pochi")}, ShowInSetup: true},
		{ID: "adal", DisplayName: "AdaL", ProjectSkillsDir: ".adal/skills", GlobalSkillsDir: filepath.Join(home, ".adal/skills"), DetectPaths: []string{filepath.Join(home, ".adal")}, ShowInSetup: true},
	}
}

func AgentStatuses() []AgentStatus {
	defs := AgentCatalog()
	statuses := make([]AgentStatus, 0, len(defs))
	for _, def := range defs {
		statuses = append(statuses, AgentStatus{AgentDefinition: def, Installed: agentInstalled(def)})
	}
	return statuses
}

func RootsForAgents(agentIDs []string) []string {
	selected := map[string]bool{}
	for _, id := range agentIDs {
		selected[id] = true
	}
	var roots []string
	seen := map[string]bool{}
	for _, agent := range AgentCatalog() {
		if !selected[agent.ID] {
			continue
		}
		for _, root := range agent.GlobalSkillDirs() {
			root = filepath.Clean(root)
			if root != "." && !seen[root] {
				roots = append(roots, root)
				seen[root] = true
			}
		}
	}
	sort.Strings(roots)
	return roots
}

func RootOwnershipForAgents(activeIDs, inactiveIDs []string) map[string]RootOwnership {
	active := map[string]bool{}
	inactive := map[string]bool{}
	for _, id := range activeIDs {
		active[id] = true
	}
	for _, id := range inactiveIDs {
		inactive[id] = true
	}
	owners := map[string]RootOwnership{}
	for _, agent := range AgentCatalog() {
		for _, root := range agent.GlobalSkillDirs() {
			root = filepath.Clean(root)
			owner := owners[root]
			if active[agent.ID] {
				owner.ActiveAgents = appendUniqueSorted(owner.ActiveAgents, agent.ID)
			}
			if inactive[agent.ID] {
				owner.InactiveAgents = appendUniqueSorted(owner.InactiveAgents, agent.ID)
			}
			owners[root] = owner
		}
	}
	return owners
}

func CandidateAgentIDs() (active []string, inactive []string) {
	for _, status := range AgentStatuses() {
		if !status.ShowInSetup {
			continue
		}
		if status.Installed || defaultAgent(status.ID) {
			active = append(active, status.ID)
		}
	}
	sort.Strings(active)
	return active, inactive
}

func AgentDefinitionByID(id string) (AgentDefinition, bool) {
	for _, agent := range AgentCatalog() {
		if agent.ID == id {
			return agent, true
		}
	}
	return AgentDefinition{}, false
}

func (agent AgentDefinition) GlobalSkillDirs() []string {
	dirs := make([]string, 0, 2+len(agent.ExtraGlobalSkillsDirs))
	if agent.GlobalSkillsDir != "" {
		dirs = append(dirs, agent.GlobalSkillsDir)
	}
	dirs = append(dirs, agent.ExtraGlobalSkillsDirs...)
	if agent.UsesAgentsSkillsRoot {
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".agents", "skills"))
		}
	}
	return dirs
}

func envOrDefault(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func openClawSkillsDir(home string) string {
	for _, dir := range []string{".openclaw", ".clawdbot", ".moltbot"} {
		path := filepath.Join(home, dir)
		if _, err := os.Stat(path); err == nil {
			return filepath.Join(path, "skills")
		}
	}
	return filepath.Join(home, ".openclaw/skills")
}

func agentInstalled(agent AgentDefinition) bool {
	for _, path := range agent.DetectPaths {
		if path == "" || path == "." {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func defaultAgent(id string) bool {
	switch id {
	case "pi", "codex", "opencode", "cline":
		return true
	default:
		return false
	}
}

func appendUniqueSorted(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}

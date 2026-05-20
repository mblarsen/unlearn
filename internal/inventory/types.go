package inventory

import "time"

type SkillKind string

const (
	KindDirectory SkillKind = "directory"
	KindMarkdown  SkillKind = "markdown"
	KindSkillLike SkillKind = "skill-like"
)

type Skill struct {
	ID                    string
	Name                  string
	Description           string
	Kind                  SkillKind
	Root                  string
	EncounteredPath       string
	ResolvedPath          string
	IsSymlink             bool
	Broken                bool
	ReadOnly              bool
	Frontmatter           map[string]string
	Body                  string
	PrimaryPath           string
	SupportRefs           []SupportRef
	BrokenRefs            []string
	LowerTokens           int
	UpperTokens           int
	ContentHash           string
	ActivationRisk        string
	ActivationRiskSignals []string
	Provenance            string
	RootKnown             bool
	ActiveAgents          []string
	InactiveAgents        []string
	ScannedAt             time.Time
}

type SupportRef struct {
	Mention string
	Path    string
	Tokens  int
	Broken  bool
}

type ScanOptions struct {
	Roots          []string
	RootOwnerships map[string]RootOwnership
}

type Report struct {
	Roots  []RootReport
	Skills []Skill
}

type RootReport struct {
	Path    string
	Exists  bool
	Trusted bool
	Error   string
}

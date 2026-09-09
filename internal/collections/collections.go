package collections

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mblarsen/unlearn/internal/discovery"
	"github.com/mblarsen/unlearn/internal/inventory"
)

// Collection is a user-managed organization of exact installed skills.
type Collection struct {
	Name    string   `toml:"name"`
	Members []Member `toml:"members"`
}

// Member stores the install path as its stable identity and the observed skill name as a stale-state label.
type Member struct {
	SkillName   string `toml:"skill_name"`
	InstallPath string `toml:"install_path"`
}

type CommandKind string

const (
	List         CommandKind = "list"
	Create       CommandKind = "create"
	Rename       CommandKind = "rename"
	Delete       CommandKind = "delete"
	AddMember    CommandKind = "add-member"
	RemoveMember CommandKind = "remove-member"
	Preview      CommandKind = "preview"
	Suggest      CommandKind = "suggest"
)

type Command struct {
	Kind        CommandKind
	Name        string
	NewName     string
	InstallPath string
	Query       string
}

type Result struct {
	Collections []Collection
	Preview     CollectionPreview
	Suggestions []Suggestion
	Advisory    string
	Changed     bool
	Err         error
}

type CollectionPreview struct {
	Name    string
	Members []MemberPreview
}

type MemberPreview struct {
	Member         Member
	Present        bool
	ActiveAgents   []string
	InactiveAgents []string
	OtherCopies    []CopyPreview
}

type ContentComparison string

const (
	ContentUnknown   ContentComparison = "unknown"
	ContentEqual     ContentComparison = "equal"
	ContentDivergent ContentComparison = "divergent"
)

type CopyPreview struct {
	InstallPath    string
	ActiveAgents   []string
	InactiveAgents []string
	Comparison     ContentComparison
}

type Suggestion struct {
	SkillName string
	Reason    string
	Installs  []InstallOption
	Weak      bool
}

type InstallOption struct {
	InstallPath    string
	ActiveAgents   []string
	InactiveAgents []string
	Divergent      bool
}

// Execute applies one collection command and returns a complete, normalized snapshot.
// Suggestions and previews are read-only. Adding a suggestion requires a separate AddMember command.
func Execute(current []Collection, skills []inventory.Skill, command Command) Result {
	items := cloneCollections(current)
	normalizeCollections(items)
	result := Result{Collections: items}

	switch command.Kind {
	case List:
		return result
	case Create:
		name, err := validName(command.Name)
		if err != nil {
			result.Err = err
			return result
		}
		if collectionIndex(items, name) >= 0 {
			result.Err = fmt.Errorf("collection %q already exists", name)
			return result
		}
		result.Collections = append(items, Collection{Name: name})
		result.Changed = true
	case Rename:
		idx := collectionIndex(items, command.Name)
		if idx < 0 {
			result.Err = fmt.Errorf("collection %q not found", strings.TrimSpace(command.Name))
			return result
		}
		name, err := validName(command.NewName)
		if err != nil {
			result.Err = err
			return result
		}
		if other := collectionIndex(items, name); other >= 0 && other != idx {
			result.Err = fmt.Errorf("collection %q already exists", name)
			return result
		}
		result.Collections[idx].Name = name
		result.Changed = true
	case Delete:
		idx := collectionIndex(items, command.Name)
		if idx < 0 {
			result.Err = fmt.Errorf("collection %q not found", strings.TrimSpace(command.Name))
			return result
		}
		result.Collections = append(result.Collections[:idx], result.Collections[idx+1:]...)
		result.Changed = true
	case AddMember:
		idx := collectionIndex(items, command.Name)
		if idx < 0 {
			result.Err = fmt.Errorf("collection %q not found", strings.TrimSpace(command.Name))
			return result
		}
		path := cleanPath(command.InstallPath)
		skill, ok := exactSkill(skills, path)
		if !ok {
			result.Err = fmt.Errorf("installed skill not found at %s", path)
			return result
		}
		if memberIndex(result.Collections[idx].Members, path) < 0 {
			result.Collections[idx].Members = append(result.Collections[idx].Members, Member{SkillName: skill.Name, InstallPath: path})
			result.Changed = true
		}
	case RemoveMember:
		idx := collectionIndex(items, command.Name)
		if idx < 0 {
			result.Err = fmt.Errorf("collection %q not found", strings.TrimSpace(command.Name))
			return result
		}
		path := cleanPath(command.InstallPath)
		member := memberIndex(result.Collections[idx].Members, path)
		if member < 0 {
			result.Err = fmt.Errorf("collection member not found at %s", path)
			return result
		}
		members := result.Collections[idx].Members
		result.Collections[idx].Members = append(members[:member], members[member+1:]...)
		result.Changed = true
	case Preview:
		idx := collectionIndex(items, command.Name)
		if idx < 0 {
			result.Err = fmt.Errorf("collection %q not found", strings.TrimSpace(command.Name))
			return result
		}
		result.Preview = previewCollection(items[idx], skills)
	case Suggest:
		idx := collectionIndex(items, command.Name)
		if idx < 0 {
			result.Err = fmt.Errorf("collection %q not found", strings.TrimSpace(command.Name))
			return result
		}
		result.Suggestions, result.Advisory = suggestions(items[idx], skills, command.Query)
	default:
		result.Err = fmt.Errorf("unknown collection command %q", command.Kind)
		return result
	}
	normalizeCollections(result.Collections)
	return result
}

func previewCollection(collection Collection, skills []inventory.Skill) CollectionPreview {
	preview := CollectionPreview{Name: collection.Name, Members: make([]MemberPreview, 0, len(collection.Members))}
	for _, member := range collection.Members {
		item := MemberPreview{Member: member}
		selected, present := exactSkill(skills, member.InstallPath)
		item.Present = present
		if present {
			item.ActiveAgents = sortedUnique(selected.ActiveAgents)
			item.InactiveAgents = sortedUnique(selected.InactiveAgents)
		}
		for _, skill := range skills {
			path := skillPath(skill)
			if path == member.InstallPath || !strings.EqualFold(strings.TrimSpace(skill.Name), strings.TrimSpace(member.SkillName)) {
				continue
			}
			item.OtherCopies = append(item.OtherCopies, CopyPreview{
				InstallPath:    path,
				ActiveAgents:   sortedUnique(skill.ActiveAgents),
				InactiveAgents: sortedUnique(skill.InactiveAgents),
				Comparison:     compareContent(present, selected.ContentHash, skill.ContentHash),
			})
		}
		sort.Slice(item.OtherCopies, func(i, j int) bool { return item.OtherCopies[i].InstallPath < item.OtherCopies[j].InstallPath })
		preview.Members = append(preview.Members, item)
	}
	return preview
}

func compareContent(referencePresent bool, referenceHash, copyHash string) ContentComparison {
	if !referencePresent || referenceHash == "" || copyHash == "" {
		return ContentUnknown
	}
	if referenceHash == copyHash {
		return ContentEqual
	}
	return ContentDivergent
}

func suggestions(collection Collection, skills []inventory.Skill, query string) ([]Suggestion, string) {
	search := discovery.Search(query, skills, nil)
	memberPaths := map[string]bool{}
	for _, member := range collection.Members {
		memberPaths[cleanPath(member.InstallPath)] = true
	}
	out := make([]Suggestion, 0, len(search.Matches))
	for _, match := range search.Matches {
		item := Suggestion{SkillName: match.Name, Reason: strings.Join(match.Reasons, "; "), Weak: match.Weak}
		baseHash := ""
		for _, install := range match.Installs {
			path := cleanPath(install.Path)
			if memberPaths[path] || install.Missing {
				continue
			}
			skill, ok := exactSkill(skills, path)
			if !ok {
				continue
			}
			if baseHash == "" {
				baseHash = skill.ContentHash
			}
			item.Installs = append(item.Installs, InstallOption{
				InstallPath:    path,
				ActiveAgents:   sortedUnique(install.ActiveAgents),
				InactiveAgents: sortedUnique(install.InactiveAgents),
				Divergent:      baseHash != "" && skill.ContentHash != "" && skill.ContentHash != baseHash,
			})
		}
		if len(item.Installs) > 0 {
			out = append(out, item)
		}
	}
	message := search.Message
	if len(out) == 0 && message == "" {
		message = "No observed matching unassigned install for this collection."
	}
	return out, message
}

func cloneCollections(items []Collection) []Collection {
	out := make([]Collection, len(items))
	for i, item := range items {
		out[i] = Collection{Name: item.Name, Members: append([]Member(nil), item.Members...)}
	}
	return out
}

func normalizeCollections(items []Collection) {
	for i := range items {
		items[i].Name = strings.TrimSpace(items[i].Name)
		for j := range items[i].Members {
			items[i].Members[j].SkillName = strings.TrimSpace(items[i].Members[j].SkillName)
			items[i].Members[j].InstallPath = cleanPath(items[i].Members[j].InstallPath)
		}
		sort.Slice(items[i].Members, func(a, b int) bool {
			if !strings.EqualFold(items[i].Members[a].SkillName, items[i].Members[b].SkillName) {
				return strings.ToLower(items[i].Members[a].SkillName) < strings.ToLower(items[i].Members[b].SkillName)
			}
			return items[i].Members[a].InstallPath < items[i].Members[b].InstallPath
		})
	}
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
}

func validName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", fmt.Errorf("collection name is required")
	}
	return name, nil
}

func collectionIndex(items []Collection, name string) int {
	name = strings.TrimSpace(name)
	for i, item := range items {
		if strings.EqualFold(item.Name, name) {
			return i
		}
	}
	return -1
}

func memberIndex(items []Member, path string) int {
	for i, item := range items {
		if cleanPath(item.InstallPath) == path {
			return i
		}
	}
	return -1
}

func exactSkill(skills []inventory.Skill, path string) (inventory.Skill, bool) {
	for _, skill := range skills {
		if skillPath(skill) == path {
			return skill, true
		}
	}
	return inventory.Skill{}, false
}

func skillPath(skill inventory.Skill) string {
	path := skill.EncounteredPath
	if path == "" {
		path = skill.PrimaryPath
	}
	return cleanPath(path)
}

func cleanPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Clean(path)
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

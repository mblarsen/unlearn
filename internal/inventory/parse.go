package inventory

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
)

var markdownLinkRe = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
var bareRefRe = regexp.MustCompile(`(?:references|reference|docs|scripts|examples)/[A-Za-z0-9._/-]+`)

type ActivationRiskAssessment struct {
	Risk    string
	Signals []string
}

type activationSignal struct {
	Label   string
	Pattern *regexp.Regexp
}

var universalActivationSignals = []activationSignal{
	{Label: "must use", Pattern: regexp.MustCompile(`\bmust\s+use\b`)},
	{Label: "always use", Pattern: regexp.MustCompile(`\balways\s+use\b`)},
	{Label: "before any", Pattern: regexp.MustCompile(`\bbefore\s+any\b`)},
	{Label: "for any", Pattern: regexp.MustCompile(`\bfor\s+any\b`)},
	{Label: "any task", Pattern: regexp.MustCompile(`\bany\s+(task|tasks|request|requests|work|workflow|workflows|project|projects)\b`)},
	{Label: "all tasks", Pattern: regexp.MustCompile(`\ball\s+(task|tasks|request|requests|work|workflows|projects)\b`)},
	{Label: "every", Pattern: regexp.MustCompile(`\bevery\b`)},
	{Label: "universal", Pattern: regexp.MustCompile(`\buniversal\b`)},
}

var genericActivationActionSignals = []activationSignal{
	{Label: "plan", Pattern: regexp.MustCompile(`\b(plan|plans|planned|planning)\b`)},
	{Label: "build", Pattern: regexp.MustCompile(`\b(build|builds|building|built)\b`)},
	{Label: "create", Pattern: regexp.MustCompile(`\b(create|creates|created|creating)\b`)},
	{Label: "design", Pattern: regexp.MustCompile(`\b(design|designs|designed|designing)\b`)},
	{Label: "implement", Pattern: regexp.MustCompile(`\b(implement|implements|implemented|implementing|implementation)\b`)},
	{Label: "review", Pattern: regexp.MustCompile(`\b(review|reviews|reviewed|reviewing)\b`)},
	{Label: "fix", Pattern: regexp.MustCompile(`\b(fix|fixes|fixed|fixing)\b`)},
	{Label: "debug", Pattern: regexp.MustCompile(`\b(debug|debugs|debugged|debugging)\b`)},
	{Label: "optimize", Pattern: regexp.MustCompile(`\b(optimize|optimizes|optimized|optimizing|optimise|optimises|optimised|optimising)\b`)},
	{Label: "refactor", Pattern: regexp.MustCompile(`\b(refactor|refactors|refactored|refactoring)\b`)},
}

func ParseSkillMarkdown(data []byte) (map[string]string, string) {
	front := map[string]string{}
	if !bytes.HasPrefix(data, []byte("---\n")) && !bytes.HasPrefix(data, []byte("---\r\n")) {
		return front, string(data)
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	if !scanner.Scan() {
		return front, string(data)
	}
	var fmLines []string
	var bodyLines []string
	inFM := true
	for scanner.Scan() {
		line := scanner.Text()
		if inFM && strings.TrimSpace(line) == "---" {
			inFM = false
			continue
		}
		if inFM {
			fmLines = append(fmLines, line)
		} else {
			bodyLines = append(bodyLines, line)
		}
	}
	if inFM {
		return map[string]string{}, string(data)
	}
	for _, line := range fmLines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.Trim(value, `"'`)
		front[key] = value
	}
	return front, strings.Join(bodyLines, "\n")
}

func ExtractReferences(markdown string) []string {
	seen := map[string]bool{}
	var refs []string
	add := func(ref string) {
		ref = strings.TrimSpace(ref)
		ref = strings.TrimPrefix(ref, "./")
		if ref == "" || strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "#") {
			return
		}
		if !strings.Contains(ref, "/") && !strings.HasSuffix(ref, ".md") {
			return
		}
		ref = filepath.Clean(ref)
		if ref == "." || strings.HasPrefix(ref, "..") {
			return
		}
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	for _, match := range markdownLinkRe.FindAllStringSubmatch(markdown, -1) {
		add(match[1])
	}
	for _, match := range bareRefRe.FindAllString(markdown, -1) {
		add(match)
	}
	return refs
}

func EstimateTokens(data []byte) int {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return 0
	}
	words := len(strings.Fields(text))
	chars := len([]rune(text)) / 4
	if chars > words {
		return chars
	}
	return words
}

func ContentHash(parts ...[]byte) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write(part)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ActivationRisk(description, body string) string {
	return AssessActivationRisk(description, body).Risk
}

func AssessActivationRisk(description, body string) ActivationRiskAssessment {
	text := strings.ToLower(description + "\n" + body)
	universal := matchedActivationSignals(text, universalActivationSignals, "universal")
	actions := matchedActivationSignals(text, genericActivationActionSignals, "action")

	risk := "low"
	switch {
	case hasActivationSignal(universal, "must use") || hasActivationSignal(universal, "always use") || hasActivationSignal(universal, "before any"):
		risk = "high"
	case hasActivationSignal(universal, "any task") || hasActivationSignal(universal, "all tasks"):
		risk = "high"
	case len(universal) >= 2:
		risk = "high"
	case len(universal) > 0 && len(actions) > 0:
		risk = "high"
	case len(universal) > 0 || len(actions) >= 5 || len(description) > 180:
		risk = "medium"
	}

	signals := make([]string, 0, len(universal)+len(actions))
	signals = append(signals, universal...)
	signals = append(signals, actions...)
	return ActivationRiskAssessment{Risk: risk, Signals: signals}
}

func matchedActivationSignals(text string, signals []activationSignal, category string) []string {
	seen := map[string]bool{}
	var matches []string
	for _, signal := range signals {
		if signal.Pattern.MatchString(text) {
			label := category + ` "` + signal.Label + `"`
			if !seen[label] {
				seen[label] = true
				matches = append(matches, label)
			}
		}
	}
	return matches
}

func hasActivationSignal(signals []string, label string) bool {
	needle := `"` + label + `"`
	for _, signal := range signals {
		if strings.Contains(signal, needle) {
			return true
		}
	}
	return false
}

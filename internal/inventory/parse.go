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
	text := strings.ToLower(description + "\n" + body)
	broadTerms := []string{"any", "all", "always", "must use", "before any", "every", "universal", "plan", "build", "implement", "review", "fix", "debug", "optimize"}
	count := 0
	for _, term := range broadTerms {
		if strings.Contains(text, term) {
			count++
		}
	}
	switch {
	case count >= 5 || len(description) > 350:
		return "high"
	case count >= 2 || len(description) > 180:
		return "medium"
	default:
		return "low"
	}
}

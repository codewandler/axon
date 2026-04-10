// Package commitparser implements a lenient parser for Conventional Commits
// (https://www.conventionalcommits.org/). Non-conforming messages are handled
// gracefully: CommitType is left empty and Subject holds the full first line.
package commitparser

import (
	"regexp"
	"strings"
)

// headerRe matches a conventional commit first line.
// Groups: (1) type, (3) scope (inside parens), (4) breaking !, (5) subject.
var headerRe = regexp.MustCompile(`^(\w+)(\(([^)]+)\))?(!)?: (.+)$`)

// trailerRe matches a single git trailer line.
// Accepts both "Token: value" and "Token #value" forms.
var trailerRe = regexp.MustCompile(`^([\w][\w -]*)(?:: | #)(.+)$`)

// ParsedCommit holds the structured result of parsing a commit message.
type ParsedCommit struct {
	// CommitType is the conventional commit type (e.g. "feat", "fix").
	// Empty string for non-conventional commits.
	CommitType string
	// Scope is the optional scope in parentheses (e.g. "aql", "cli").
	Scope string
	// Breaking is true when the commit includes "!" after the type/scope
	// or has a "BREAKING CHANGE" footer.
	Breaking bool
	// Subject is the description after the type/scope prefix, or the full
	// first line for non-conventional commits.
	Subject string
	// Body is the free-form paragraph(s) between the first line and the
	// footer block. May be empty.
	Body string
	// Footers contains all git trailer key/value pairs. Each key may have
	// multiple values if the same trailer appears more than once.
	Footers map[string][]string
	// Refs contains deduplicated ticket/issue references extracted from the
	// "Refs:" footer. The original "#" prefix is preserved (e.g. "#8").
	Refs []string
}

// Parse parses a raw commit message and returns a ParsedCommit.
// It never returns an error; non-conventional messages fall back gracefully.
func Parse(message string) ParsedCommit {
	result := ParsedCommit{
		Footers: make(map[string][]string),
	}

	// Normalise line endings and trim leading/trailing blank lines.
	message = strings.TrimSpace(strings.ReplaceAll(message, "\r\n", "\n"))
	if message == "" {
		return result
	}

	lines := strings.Split(message, "\n")

	// --- Parse the first line (header) ---
	header := strings.TrimSpace(lines[0])
	if m := headerRe.FindStringSubmatch(header); m != nil {
		result.CommitType = m[1]
		result.Scope = m[3]
		result.Breaking = m[4] == "!"
		result.Subject = strings.TrimSpace(m[5])
	} else {
		result.Subject = header
	}

	if len(lines) == 1 {
		return result
	}

	// --- Parse body and footer block ---
	// Everything after the first line is the rest of the message.
	rest := lines[1:]

	// Identify the contiguous trailer block at the bottom of the message.
	// We scan backwards; the moment we hit a non-trailer line we stop.
	footerStart := len(rest)
	for i := len(rest) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(rest[i])
		if trimmed == "" {
			// Blank line terminates the footer block scan.
			break
		}
		if trailerRe.MatchString(trimmed) {
			footerStart = i
		} else {
			// Non-trailer non-blank line: stop scanning.
			break
		}
	}

	// Extract footers.
	for _, line := range rest[footerStart:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if m := trailerRe.FindStringSubmatch(trimmed); m != nil {
			key := strings.TrimSpace(m[1])
			val := strings.TrimSpace(m[2])
			result.Footers[key] = append(result.Footers[key], val)
		}
	}

	// Check BREAKING CHANGE in footers.
	if _, ok := result.Footers["BREAKING CHANGE"]; ok {
		result.Breaking = true
	}

	// Extract and deduplicate Refs.
	if refsVals, ok := result.Footers["Refs"]; ok {
		seen := make(map[string]bool)
		for _, rv := range refsVals {
			// Split on comma or whitespace.
			tokens := splitRefs(rv)
			for _, tok := range tokens {
				if tok != "" && !seen[tok] {
					seen[tok] = true
					result.Refs = append(result.Refs, tok)
				}
			}
		}
	}

	// Extract body: everything between the first line and the footer block,
	// excluding leading/trailing blank lines.
	bodyLines := rest[:footerStart]
	// Strip leading and trailing blank lines from body.
	bodyStart, bodyEnd := 0, len(bodyLines)
	for bodyStart < bodyEnd && strings.TrimSpace(bodyLines[bodyStart]) == "" {
		bodyStart++
	}
	for bodyEnd > bodyStart && strings.TrimSpace(bodyLines[bodyEnd-1]) == "" {
		bodyEnd--
	}
	if bodyStart < bodyEnd {
		result.Body = strings.Join(bodyLines[bodyStart:bodyEnd], "\n")
	}

	return result
}

// splitRefs splits a Refs value string into individual tokens.
// Tokens are separated by commas, whitespace, or both.
// Leading "#" characters are preserved (they identify GitHub issues).
func splitRefs(s string) []string {
	// Replace commas with spaces, then split on whitespace.
	s = strings.ReplaceAll(s, ",", " ")
	raw := strings.Fields(s)
	tokens := make([]string, 0, len(raw))
	for _, tok := range raw {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			tokens = append(tokens, tok)
		}
	}
	return tokens
}

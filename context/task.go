package context

import (
	"regexp"
	"strings"
	"unicode"
)

// TaskIntent represents the inferred intent of a task.
type TaskIntent int

const (
	IntentGeneral    TaskIntent = iota // Default/unknown
	IntentModify                       // "add", "change", "update", "refactor"
	IntentFix                          // "fix", "debug", "resolve", "repair"
	IntentUnderstand                   // "explain", "how does", "what is", "understand"
	IntentAdd                          // "implement", "create", "new", "write"
	IntentRemove                       // "remove", "delete", "drop"
)

// String returns the string representation of the intent.
func (i TaskIntent) String() string {
	switch i {
	case IntentModify:
		return "modify"
	case IntentFix:
		return "fix"
	case IntentUnderstand:
		return "understand"
	case IntentAdd:
		return "add"
	case IntentRemove:
		return "remove"
	default:
		return "general"
	}
}

// ParsedTask holds the parsed components of a task description.
type ParsedTask struct {
	Raw      string     // Original task description
	Symbols  []string   // Extracted PascalCase/camelCase identifiers (e.g., "Storage", "NewNode")
	Keywords []string   // Meaningful lowercase words after stop-word removal
	Intent   TaskIntent // Classified intent
}

// stopWords is a set of common English stop words to filter out.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "was": true, "were": true,
	"be": true, "been": true, "being": true, "have": true, "has": true, "had": true,
	"do": true, "does": true, "did": true, "will": true, "would": true, "could": true,
	"should": true, "may": true, "might": true, "must": true, "can": true,
	"to": true, "of": true, "in": true, "for": true, "on": true, "with": true,
	"at": true, "by": true, "from": true, "as": true, "into": true, "through": true,
	"and": true, "or": true, "but": true, "if": true, "then": true, "else": true,
	"when": true, "where": true, "why": true, "how": true, "what": true, "which": true,
	"who": true, "whom": true, "whose": true, "that": true, "this": true, "these": true,
	"those": true, "it": true, "its": true, "i": true, "me": true, "my": true,
	"we": true, "us": true, "our": true, "you": true, "your": true, "he": true,
	"she": true, "they": true, "them": true, "their": true,
	"all": true, "any": true, "both": true, "each": true, "few": true, "more": true,
	"most": true, "other": true, "some": true, "such": true, "no": true, "not": true,
	"only": true, "same": true, "so": true, "than": true, "too": true, "very": true,
	"just": true, "also": true, "now": true, "here": true, "there": true,
}

// intentVerbs maps verbs to intents.
var intentVerbs = map[string]TaskIntent{
	// Modify intent
	"add": IntentModify, "change": IntentModify, "update": IntentModify, "modify": IntentModify,
	"refactor": IntentModify, "improve": IntentModify, "enhance": IntentModify, "extend": IntentModify,
	"edit": IntentModify, "adjust": IntentModify, "optimize": IntentModify,

	// Fix intent
	"fix": IntentFix, "debug": IntentFix, "resolve": IntentFix, "repair": IntentFix,
	"correct": IntentFix, "patch": IntentFix, "troubleshoot": IntentFix,

	// Understand intent
	"explain": IntentUnderstand, "understand": IntentUnderstand, "describe": IntentUnderstand,
	"analyze": IntentUnderstand, "review": IntentUnderstand, "examine": IntentUnderstand,
	"investigate": IntentUnderstand, "explore": IntentUnderstand, "study": IntentUnderstand,

	// Add intent (new functionality)
	"implement": IntentAdd, "create": IntentAdd, "write": IntentAdd, "build": IntentAdd,
	"new": IntentAdd, "introduce": IntentAdd, "develop": IntentAdd,

	// Remove intent
	"remove": IntentRemove, "delete": IntentRemove, "drop": IntentRemove,
	"eliminate": IntentRemove, "deprecate": IntentRemove,
}

// backtickPattern matches backtick-quoted identifiers.
var backtickPattern = regexp.MustCompile("`([^`]+)`")

// pascalCasePattern matches PascalCase or camelCase identifiers.
// Matches words starting with uppercase, or lowercase followed by uppercase.
var pascalCasePattern = regexp.MustCompile(`\b([A-Z][a-zA-Z0-9]*|[a-z]+[A-Z][a-zA-Z0-9]*)\b`)

// dottedPathPattern matches dotted paths like "graph.Storage".
var dottedPathPattern = regexp.MustCompile(`([a-z]+)\.([A-Z][a-zA-Z0-9]*)`)

// ParseTask parses a task description and extracts symbols, keywords, and intent.
func ParseTask(description string) *ParsedTask {
	task := &ParsedTask{
		Raw:    description,
		Intent: IntentGeneral,
	}

	// Extract backtick-quoted identifiers first
	backtickMatches := backtickPattern.FindAllStringSubmatch(description, -1)
	for _, match := range backtickMatches {
		if len(match) > 1 {
			task.Symbols = appendUnique(task.Symbols, match[1])
		}
	}

	// Extract dotted paths (e.g., "graph.Storage" -> Symbol: "Storage", Keyword: "graph")
	dottedMatches := dottedPathPattern.FindAllStringSubmatch(description, -1)
	for _, match := range dottedMatches {
		if len(match) > 2 {
			task.Keywords = appendUnique(task.Keywords, strings.ToLower(match[1]))
			task.Symbols = appendUnique(task.Symbols, match[2])
		}
	}

	// Extract PascalCase/camelCase identifiers
	pascalMatches := pascalCasePattern.FindAllString(description, -1)
	for _, match := range pascalMatches {
		// Skip common English words that happen to match
		if isCommonWord(match) {
			continue
		}
		task.Symbols = appendUnique(task.Symbols, match)
	}

	// Extract keywords (lowercase words after removing stop words)
	words := tokenizeWords(description)
	for _, word := range words {
		lower := strings.ToLower(word)
		if !stopWords[lower] && len(lower) > 2 && !containsSymbol(task.Symbols, word) {
			// Check if it's an intent verb first
			if intent, ok := intentVerbs[lower]; ok {
				if task.Intent == IntentGeneral {
					task.Intent = intent
				}
			}
			// Only add as keyword if it's not purely alphanumeric noise
			if isKeywordWorthy(lower) {
				task.Keywords = appendUnique(task.Keywords, lower)
			}
		}
	}

	return task
}

// tokenizeWords splits text into words, handling punctuation.
func tokenizeWords(text string) []string {
	var words []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words
}

// isCommonWord checks if a PascalCase match is actually a common English word.
var commonPascalWords = map[string]bool{
	"The": true, "This": true, "That": true, "These": true, "Those": true,
	"With": true, "From": true, "Into": true, "Through": true,
	"Should": true, "Could": true, "Would": true, "Must": true,
	"Have": true, "Has": true, "Had": true,
	"Does": true, "Did": true,
	"Will": true, "Can": true, "May": true, "Might": true,
	"Here": true, "There": true, "Where": true, "When": true,
}

func isCommonWord(word string) bool {
	return commonPascalWords[word]
}

// isKeywordWorthy checks if a word is worth keeping as a keyword.
func isKeywordWorthy(word string) bool {
	// Skip very short words
	if len(word) < 3 {
		return false
	}
	// Skip purely numeric
	allDigits := true
	for _, r := range word {
		if !unicode.IsDigit(r) {
			allDigits = false
			break
		}
	}
	return !allDigits
}

// containsSymbol checks if symbols already contains the word (case-insensitive).
func containsSymbol(symbols []string, word string) bool {
	lower := strings.ToLower(word)
	for _, s := range symbols {
		if strings.ToLower(s) == lower {
			return true
		}
	}
	return false
}

// appendUnique appends a value to a slice if it's not already present.
func appendUnique(slice []string, value string) []string {
	for _, v := range slice {
		if v == value {
			return slice
		}
	}
	return append(slice, value)
}

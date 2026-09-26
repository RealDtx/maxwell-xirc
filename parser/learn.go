package parser

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Annotation is a user-labeled span within a raw IRC line.
// Start and End are Unicode code-point (rune) offsets, zero-based, End exclusive.
type Annotation struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Field string `json:"field"`
}

// LearnResult holds the generated regex; fields are named capture groups.
type LearnResult struct {
	Regex string `json:"regex"`
}

func GeneratePatternFromAnnotations(rawLine string, annotations []Annotation) (*LearnResult, error) {
	if len(annotations) == 0 {
		return nil, fmt.Errorf("annotations cannot be empty")
	}

	rawRunes := []rune(rawLine)
	lineLen := len(rawRunes)
	sorted := make([]Annotation, len(annotations))
	copy(sorted, annotations)

	for _, ann := range sorted {
		if ann.Start < 0 || ann.End < 0 || ann.Start >= ann.End || ann.End > lineLen {
			return nil, fmt.Errorf("invalid annotation bounds: start=%d end=%d", ann.Start, ann.End)
		}
		if _, ok := annotationPatternFragment(ann.Field); !ok {
			return nil, fmt.Errorf("invalid annotation field: %s", ann.Field)
		}
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Start < sorted[j].Start
	})

	for i := 1; i < len(sorted); i++ {
		if sorted[i].Start < sorted[i-1].End {
			return nil, fmt.Errorf("overlapping annotations")
		}
	}

	var b strings.Builder
	seen := map[string]bool{}
	cursor := 0

	for _, ann := range sorted {
		if ann.Start > cursor {
			b.WriteString(escapeLiteralForRegex(string(rawRunes[cursor:ann.Start])))
		}

		fragment, _ := annotationPatternFragment(ann.Field)
		if fragment[0] == '(' && fragment[1] != '?' {
			if seen[ann.Field] {
				return nil, fmt.Errorf("field %s annotated more than once", ann.Field)
			}
			seen[ann.Field] = true
			fragment = "(?<" + ann.Field + ">" + fragment[1:]
		}
		b.WriteString(fragment)

		cursor = ann.End
	}

	if cursor < lineLen {
		b.WriteString(escapeLiteralForRegex(string(rawRunes[cursor:])))
	}

	generatedRegex := b.String()
	if _, err := regexp.Compile(generatedRegex); err != nil {
		return nil, fmt.Errorf("generated regex is invalid: %w", err)
	}

	return &LearnResult{Regex: generatedRegex}, nil
}

func annotationPatternFragment(field string) (string, bool) {
	switch field {
	case "pack_number", "downloads_count":
		return `(\d+)`, true
	case "filesize", "bot_nick":
		return `(\S+)`, true
	case "filename":
		return `(.+?)`, true
	case "skip":
		return `(?:\S+)`, true
	case "skip_number":
		return `(?:\d+)`, true
	default:
		return "", false
	}
}

func escapeLiteralForRegex(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return `\s*`
	}

	tokens := strings.Fields(trimmed)
	for i := range tokens {
		tokens[i] = regexp.QuoteMeta(tokens[i])
	}

	return `\s*` + strings.Join(tokens, `\s+`) + `\s*`
}

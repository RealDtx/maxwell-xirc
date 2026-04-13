package parser

import (
	"encoding/json"
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

// LearnResult holds the generated regex and field_mapping JSON.
type LearnResult struct {
	Regex        string `json:"regex"`
	FieldMapping string `json:"field_mapping"`
}

type learnFieldMapping struct {
	PackNumber     int `json:"pack_number,omitempty"`
	DownloadsCount int `json:"downloads_count,omitempty"`
	Filesize       int `json:"filesize,omitempty"`
	Filename       int `json:"filename,omitempty"`
	BotNick        int `json:"bot_nick,omitempty"`
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
	mapping := learnFieldMapping{}
	groupIndex := 1
	cursor := 0

	for _, ann := range sorted {
		if ann.Start > cursor {
			b.WriteString(escapeLiteralForRegex(string(rawRunes[cursor:ann.Start])))
		}

		fragment, _ := annotationPatternFragment(ann.Field)
		b.WriteString(fragment)
		switch ann.Field {
		case "pack_number":
			mapping.PackNumber = groupIndex
			groupIndex++
		case "downloads_count":
			mapping.DownloadsCount = groupIndex
			groupIndex++
		case "filesize":
			mapping.Filesize = groupIndex
			groupIndex++
		case "filename":
			mapping.Filename = groupIndex
			groupIndex++
		case "bot_nick":
			mapping.BotNick = groupIndex
			groupIndex++
		}

		cursor = ann.End
	}

	if cursor < lineLen {
		b.WriteString(escapeLiteralForRegex(string(rawRunes[cursor:])))
	}

	generatedRegex := b.String()
	if _, err := regexp.Compile(generatedRegex); err != nil {
		return nil, fmt.Errorf("generated regex is invalid: %w", err)
	}

	fieldMappingJSON, err := json.Marshal(mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal field mapping: %w", err)
	}

	return &LearnResult{
		Regex:        generatedRegex,
		FieldMapping: string(fieldMappingJSON),
	}, nil
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

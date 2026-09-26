package parser

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestGeneratePatternFromAnnotations(t *testing.T) {
	tests := []struct {
		name            string
		rawLine         string
		annotations     []Annotation
		expectErr       bool
		expectedMapping map[string]int
	}{
		{
			name:    "BotReign full example",
			rawLine: "001) 116x | 2.3G | Some.Movie.mkv | /msg [MG]-Bot XDCC SEND 989",
			annotations: []Annotation{
				mustAnnotationSpan("001) 116x | 2.3G | Some.Movie.mkv | /msg [MG]-Bot XDCC SEND 989", "001", "skip_number"),
				mustAnnotationSpan("001) 116x | 2.3G | Some.Movie.mkv | /msg [MG]-Bot XDCC SEND 989", "116", "downloads_count"),
				mustAnnotationSpan("001) 116x | 2.3G | Some.Movie.mkv | /msg [MG]-Bot XDCC SEND 989", "2.3G", "filesize"),
				mustAnnotationSpan("001) 116x | 2.3G | Some.Movie.mkv | /msg [MG]-Bot XDCC SEND 989", "Some.Movie.mkv", "filename"),
				mustAnnotationSpan("001) 116x | 2.3G | Some.Movie.mkv | /msg [MG]-Bot XDCC SEND 989", "[MG]-Bot", "bot_nick"),
				mustAnnotationSpan("001) 116x | 2.3G | Some.Movie.mkv | /msg [MG]-Bot XDCC SEND 989", "989", "pack_number"),
			},
			expectedMapping: map[string]int{
				"downloads_count": 1,
				"filesize":        2,
				"filename":        3,
				"bot_nick":        4,
				"pack_number":     5,
			},
		},
		{
			name:    "Minimal - filename only",
			rawLine: "File: Some.Movie.mkv",
			annotations: []Annotation{
				mustAnnotationSpan("File: Some.Movie.mkv", "Some.Movie.mkv", "filename"),
			},
			expectedMapping: map[string]int{"filename": 1},
		},
		{
			name:    "Overlapping annotations error",
			rawLine: "abcdef",
			annotations: []Annotation{
				{Start: 0, End: 3, Field: "filename"},
				{Start: 2, End: 5, Field: "filesize"},
			},
			expectErr: true,
		},
		{
			name:    "Out of bounds annotation error",
			rawLine: "abc",
			annotations: []Annotation{
				{Start: 0, End: 5, Field: "filename"},
			},
			expectErr: true,
		},
		{
			name:        "No annotations error",
			rawLine:     "anything",
			annotations: []Annotation{},
			expectErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := GeneratePatternFromAnnotations(tc.rawLine, tc.annotations)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			re := regexp.MustCompile(result.Regex)
			if !re.MatchString(tc.rawLine) {
				t.Fatalf("generated regex did not match original line: %q", result.Regex)
			}

			got := map[string]int{}
			for i, name := range re.SubexpNames() {
				if name != "" {
					got[name] = i
				}
			}
			if !reflect.DeepEqual(got, tc.expectedMapping) {
				t.Fatalf("unexpected named groups: got %v want %v", got, tc.expectedMapping)
			}
		})
	}
}

func mustAnnotationSpan(rawLine, token, field string) Annotation {
	startByte := strings.Index(rawLine, token)
	if startByte == -1 {
		panic("token not found in raw line: " + token)
	}

	start := len([]rune(rawLine[:startByte]))
	end := start + len([]rune(token))
	return Annotation{Start: start, End: end, Field: field}
}

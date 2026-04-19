package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/dop251/goja"
)

func mustParseRFC3339(t *testing.T, iso string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		t.Fatalf("failed to parse ISO timestamp %q: %v", iso, err)
	}
	return parsed.In(time.Local)
}

func loadSearchResultsJS(t *testing.T) *goja.Runtime {
	t.Helper()
	src, err := os.ReadFile("web/js/search-results.js")
	if err != nil {
		t.Fatalf("failed to read search-results.js: %v", err)
	}
	vm := goja.New()
	if _, err := vm.RunString(string(src)); err != nil {
		t.Fatalf("failed to eval search-results.js: %v", err)
	}
	return vm
}

func normalizeSearchRow(t *testing.T, vm *goja.Runtime, input map[string]interface{}) map[string]interface{} {
	t.Helper()
	fn, ok := goja.AssertFunction(vm.Get("normalizeSearchRow"))
	if !ok {
		t.Fatal("normalizeSearchRow is not a function")
	}
	arg := vm.ToValue(input)
	result, err := fn(goja.Undefined(), arg)
	if err != nil {
		t.Fatalf("normalizeSearchRow call failed: %v", err)
	}
	obj := result.Export().(map[string]interface{})
	return obj
}

func TestNormalizeSearchRow_NumericFilesize(t *testing.T) {
	vm := loadSearchResultsJS(t)
	row := normalizeSearchRow(t, vm, map[string]interface{}{"filesize": 1048576})
	if got := row["size_display"]; got != "1.0 MB" {
		t.Errorf("expected '1.0 MB', got %q", got)
	}
}

func TestNormalizeSearchRow_StringFilesize(t *testing.T) {
	vm := loadSearchResultsJS(t)
	row := normalizeSearchRow(t, vm, map[string]interface{}{"filesize": "204M"})
	if got := row["size_display"]; got != "204M" {
		t.Errorf("expected '204M', got %q", got)
	}
}

func TestNormalizeSearchRow_ZeroFilesize(t *testing.T) {
	vm := loadSearchResultsJS(t)
	row := normalizeSearchRow(t, vm, map[string]interface{}{"filesize": 0})
	if got := row["size_display"]; got != "-" {
		t.Errorf("expected '-', got %q", got)
	}
}

func TestNormalizeSearchRow_EmptyStringFilesize(t *testing.T) {
	vm := loadSearchResultsJS(t)
	row := normalizeSearchRow(t, vm, map[string]interface{}{"filesize": ""})
	if got := row["size_display"]; got != "-" {
		t.Errorf("expected '-', got %q", got)
	}
}

func TestNormalizeSearchRow_MissingFilesize(t *testing.T) {
	vm := loadSearchResultsJS(t)
	row := normalizeSearchRow(t, vm, map[string]interface{}{})
	if got := row["size_display"]; got != "-" {
		t.Errorf("expected '-', got %q", got)
	}
}

func TestNormalizeSearchRow_PreservesOriginalFields(t *testing.T) {
	vm := loadSearchResultsJS(t)
	row := normalizeSearchRow(t, vm, map[string]interface{}{
		"filename": "A.mkv",
		"filesize": 1234,
		"bot_nick": "BotA",
	})
	if got := row["filename"]; got != "A.mkv" {
		t.Errorf("expected filename 'A.mkv', got %q", got)
	}
	if got := row["bot_nick"]; got != "BotA" {
		t.Errorf("expected bot_nick 'BotA', got %q", got)
	}
	if _, ok := row["size_display"]; !ok {
		t.Error("expected size_display field to be present")
	}
}

func loadUtilsJS(t *testing.T) *goja.Runtime {
	t.Helper()
	src, err := os.ReadFile("web/js/utils.js")
	if err != nil {
		t.Fatalf("failed to read utils.js: %v", err)
	}
	vm := goja.New()
	if _, err := vm.RunString(string(src)); err != nil {
		t.Fatalf("failed to eval utils.js: %v", err)
	}
	return vm
}

func TestFormatDateCustom_Default(t *testing.T) {
	vm := loadUtilsJS(t)
	const iso = "2026-03-15T14:05:09Z"
	val, err := vm.RunString(`formatDateCustom("2026-03-15T14:05:09Z", "DD-MM-YYYY HH:MM:SS")`)
	if err != nil {
		t.Fatal(err)
	}
	got := val.String()
	local := mustParseRFC3339(t, iso)
	expected := fmt.Sprintf("%02d-%02d-%04d %02d:%02d:%02d", local.Day(), local.Month(), local.Year(), local.Hour(), local.Minute(), local.Second())
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestFormatDateCustom_ISO(t *testing.T) {
	vm := loadUtilsJS(t)
	const iso = "2026-03-15T14:05:09Z"
	val, err := vm.RunString(`formatDateCustom("2026-03-15T14:05:09Z", "YYYY-MM-DD HH:MM:SS")`)
	if err != nil {
		t.Fatal(err)
	}
	got := val.String()
	local := mustParseRFC3339(t, iso)
	expected := fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d", local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second())
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestFormatDateCustom_US(t *testing.T) {
	vm := loadUtilsJS(t)
	const iso = "2026-03-15T14:05:09Z"
	val, err := vm.RunString(`formatDateCustom("2026-03-15T14:05:09Z", "MM/DD/YYYY HH:MM:SS")`)
	if err != nil {
		t.Fatal(err)
	}
	got := val.String()
	local := mustParseRFC3339(t, iso)
	expected := fmt.Sprintf("%02d/%02d/%04d %02d:%02d:%02d", local.Month(), local.Day(), local.Year(), local.Hour(), local.Minute(), local.Second())
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestFormatDateCustom_EmptyInput(t *testing.T) {
	vm := loadUtilsJS(t)
	val, err := vm.RunString(`formatDateCustom("", "DD-MM-YYYY HH:MM:SS")`)
	if err != nil {
		t.Fatal(err)
	}
	if val.String() != "-" {
		t.Errorf("expected '-', got %q", val.String())
	}
}

func TestFormatDuration(t *testing.T) {
	vm := loadUtilsJS(t)
	val, err := vm.RunString(`formatDuration("2026-03-15T14:00:00Z", "2026-03-15T15:23:45Z")`)
	if err != nil {
		t.Fatal(err)
	}
	got := val.String()
	if got != "1h 23m" {
		t.Errorf("expected '1h 23m', got %q", got)
	}
}

func TestFormatRelativeTime_Nil(t *testing.T) {
	vm := loadUtilsJS(t)
	val, err := vm.RunString(`formatRelativeTime("")`)
	if err != nil {
		t.Fatal(err)
	}
	if val.String() != "-" {
		t.Errorf("expected '-', got %q", val.String())
	}
}

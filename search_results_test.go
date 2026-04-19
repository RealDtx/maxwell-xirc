package main

import (
	"os"
	"testing"

	"github.com/dop251/goja"
)

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

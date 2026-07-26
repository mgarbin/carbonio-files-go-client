package i18n

import "testing"

// TestResolveEmptyFallsBackToDefault covers Resolve("") returning the
// first supported locale (the documented fallback) without consulting the
// matcher at all.
func TestResolveEmptyFallsBackToDefault(t *testing.T) {
	if got := Resolve(""); got != SupportedLocales[0] {
		t.Fatalf("Resolve(\"\") = %q, want %q", got, SupportedLocales[0])
	}
}

// TestResolveMatchesSupportedLocales covers Resolve normalizing and
// matching POSIX/BCP-47 style OS locale hints against every locale we
// ship, in the various forms glibc/Windows/macOS may hand us.
func TestResolveMatchesSupportedLocales(t *testing.T) {
	cases := map[string]string{
		"it_IT.UTF-8": "it",
		"it-IT":       "it",
		"it":          "it",
		"en_US.UTF-8": "en",
		"en-US":       "en",
		"en":          "en",
	}
	for input, want := range cases {
		if got := Resolve(input); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestResolveUnsupportedFallsBackToDefault covers Resolve falling back to
// SupportedLocales[0] when the OS locale doesn't match any shipped
// catalog, so an unrecognized language never yields an empty/unusable
// locale code.
func TestResolveUnsupportedFallsBackToDefault(t *testing.T) {
	for _, input := range []string{"zh-CN", "xx-XX", "ja_JP.UTF-8"} {
		if got := Resolve(input); got != SupportedLocales[0] {
			t.Errorf("Resolve(%q) = %q, want fallback %q", input, got, SupportedLocales[0])
		}
	}
}

// TestNormalize covers the POSIX-locale-to-BCP-47 conversion: everything
// from the first "." or "@" onward (codeset/modifier) is stripped, and "_"
// separators become "-" so golang.org/x/text/language can parse the result.
func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"it_IT.UTF-8@euro": "it-IT",
		"it_IT.UTF-8":      "it-IT",
		"it_IT@euro":       "it-IT",
		"it_IT":            "it-IT",
		"en":               "en",
		"":                 "",
	}
	for input, want := range cases {
		if got := normalize(input); got != want {
			t.Errorf("normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestCatalogSupportedLocales covers Catalog successfully loading the
// embedded JSON catalog for every supported locale, with a shared key
// ("app.title") present and non-empty in each.
func TestCatalogSupportedLocales(t *testing.T) {
	for _, locale := range SupportedLocales {
		catalog, err := Catalog(locale)
		if err != nil {
			t.Fatalf("Catalog(%q) returned error: %v", locale, err)
		}
		if len(catalog) == 0 {
			t.Fatalf("Catalog(%q) returned an empty map", locale)
		}
		if got := catalog["app.title"]; got == "" {
			t.Errorf("Catalog(%q)[\"app.title\"] is empty, want a translated title", locale)
		}
	}
}

// TestCatalogUnknownLocaleFallsBack covers Catalog falling back to the
// SupportedLocales[0] catalog (rather than erroring) when asked for a
// locale we don't ship a JSON file for.
func TestCatalogUnknownLocaleFallsBack(t *testing.T) {
	catalog, err := Catalog("nonexistent-locale")
	if err != nil {
		t.Fatalf("Catalog(%q) returned error: %v", "nonexistent-locale", err)
	}
	fallback, err := Catalog(SupportedLocales[0])
	if err != nil {
		t.Fatalf("Catalog(%q) returned error: %v", SupportedLocales[0], err)
	}
	if len(catalog) != len(fallback) {
		t.Fatalf("Catalog(%q) has %d keys, want fallback catalog's %d keys", "nonexistent-locale", len(catalog), len(fallback))
	}
	if catalog["app.title"] != fallback["app.title"] {
		t.Errorf("Catalog(%q)[\"app.title\"] = %q, want fallback value %q", "nonexistent-locale", catalog["app.title"], fallback["app.title"])
	}
}

// TestDetectAndLoad covers the end-to-end detect-resolve-load path: the
// resolved locale must always be one of SupportedLocales (never empty or
// unrecognized) and the returned catalog must always be usable, whatever
// the test environment's OS locale happens to be.
func TestDetectAndLoad(t *testing.T) {
	locale, catalog := DetectAndLoad()

	found := false
	for _, code := range SupportedLocales {
		if code == locale {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("DetectAndLoad() locale = %q, want one of %v", locale, SupportedLocales)
	}
	if catalog == nil {
		t.Fatal("DetectAndLoad() catalog = nil, want a non-nil map")
	}
	if len(catalog) == 0 {
		t.Error("DetectAndLoad() catalog is empty, want translated entries")
	}
}

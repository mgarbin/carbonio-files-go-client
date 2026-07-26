// Package i18n resolves the desktop GUI's display language from the host
// OS locale and serves the matching translation catalog. Locale detection is
// platform-specific (see locale_linux.go, locale_darwin.go,
// locale_windows.go); everything else lives here.
package i18n

import (
	"embed"
	"encoding/json"
	"strings"

	"golang.org/x/text/language"
)

//go:embed locales/*.json
var localesFS embed.FS

// SupportedLocales lists the locale codes shipped with the application.
// The first entry is the fallback used whenever the OS locale can't be
// matched to anything we ship a catalog for.
var SupportedLocales = []string{"en", "it"}

var matcher = language.NewMatcher(supportedTags())

func supportedTags() []language.Tag {
	tags := make([]language.Tag, len(SupportedLocales))
	for i, code := range SupportedLocales {
		tags[i] = language.MustParse(code)
	}
	return tags
}

// Resolve picks the best supported locale for an OS locale hint such as
// "it_IT.UTF-8", "en-US", "de-DE", or "". Falls back to SupportedLocales[0].
func Resolve(osLocale string) string {
	if osLocale == "" {
		return SupportedLocales[0]
	}
	tag, _, confidence := matcher.Match(language.Make(normalize(osLocale)))
	if confidence == language.No {
		return SupportedLocales[0]
	}
	base, _ := tag.Base()
	for _, code := range SupportedLocales {
		if code == base.String() {
			return code
		}
	}
	return SupportedLocales[0]
}

// normalize turns a POSIX locale ("it_IT.UTF-8@euro") into something
// golang.org/x/text/language can parse ("it-IT").
func normalize(raw string) string {
	s := raw
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	return strings.ReplaceAll(s, "_", "-")
}

// Catalog returns the translation map (key -> localized string) for the
// given resolved locale code, falling back to SupportedLocales[0].
func Catalog(locale string) (map[string]string, error) {
	data, err := localesFS.ReadFile("locales/" + locale + ".json")
	if err != nil {
		data, err = localesFS.ReadFile("locales/" + SupportedLocales[0] + ".json")
		if err != nil {
			return nil, err
		}
	}
	var catalog map[string]string
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}
	return catalog, nil
}

// DetectAndLoad detects the OS locale, resolves it against the supported
// locales, and returns the resolved locale code together with its catalog.
func DetectAndLoad() (string, map[string]string) {
	locale := Resolve(DetectOSLocale())
	catalog, err := Catalog(locale)
	if err != nil {
		return locale, map[string]string{}
	}
	return locale, catalog
}

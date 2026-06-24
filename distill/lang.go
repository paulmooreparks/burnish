package distill

import (
	"fmt"
	"sort"
)

// DefaultLanguage is assumed when a profile or distill run does not specify one.
const DefaultLanguage = "en"

// implementedLanguages is the set of language codes for which a feature module
// (segmentation + statistical features + lexicon baseline) actually exists.
// Today only English. The metric-extraction code in this package IS the English
// module; adding another language means registering its module here and is the
// only change the engine, profile format, lint, judge, and discriminator need
// (DESIGN.md section 11). This set exists so distill and lint refuse to produce
// or score a profile in a language whose features they would compute wrongly,
// rather than emitting an English-measured artifact mislabeled as, say, French.
var implementedLanguages = map[string]bool{
	"en": true,
}

// LanguageImplemented reports whether a feature module exists for the code.
// An empty code means DefaultLanguage.
func LanguageImplemented(code string) bool {
	if code == "" {
		code = DefaultLanguage
	}
	return implementedLanguages[code]
}

// ImplementedLanguages returns the supported codes, sorted, for error messages.
func ImplementedLanguages() []string {
	out := make([]string, 0, len(implementedLanguages))
	for k := range implementedLanguages {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ErrUnsupportedLanguage describes a request for an unimplemented language.
func ErrUnsupportedLanguage(code string) error {
	return fmt.Errorf("language %q has no feature module yet; implemented: %v (see DESIGN.md section 11)", code, ImplementedLanguages())
}

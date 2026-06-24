package distill

import (
	"strings"
	"testing"

	"github.com/paulmooreparks/burnish/internal/text"
)

func TestUnicodeWordsNotDropped(t *testing.T) {
	// Accented Latin and non-Latin scripts must survive tokenization, not be
	// silently dropped the way the old [A-Za-z]+ regex did.
	for _, s := range []string{"café naïve résumé", "Zürich Straße", "日本語 のテスト", "Привет мир"} {
		if len(text.Words(s)) == 0 {
			t.Errorf("no words extracted from %q (Unicode dropped)", s)
		}
	}
}

func TestDistillRejectsUnimplementedLanguage(t *testing.T) {
	_, err := Distill("x", "r", "fr", []DocInput{{Name: "a", Text: "Bonjour le monde."}}, DefaultOptions())
	if err == nil {
		t.Fatal("expected error for unimplemented language fr")
	}
	if !strings.Contains(err.Error(), "fr") {
		t.Errorf("error should name the language: %v", err)
	}
}

func TestDistillDefaultsLanguage(t *testing.T) {
	p, err := Distill("x", "r", "", []DocInput{
		{Name: "a", Text: "The engine measures style. It does not guess."},
		{Name: "b", Text: "A profile records the language it was built for."},
	}, DefaultOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Language != DefaultLanguage {
		t.Errorf("language = %q, want %q", p.Language, DefaultLanguage)
	}
}

func TestLanguageImplemented(t *testing.T) {
	if !LanguageImplemented("") || !LanguageImplemented("en") {
		t.Error("en (and empty default) should be implemented")
	}
	if LanguageImplemented("zh") {
		t.Error("zh should not be implemented yet")
	}
}

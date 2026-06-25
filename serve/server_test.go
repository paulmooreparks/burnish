package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paulmooreparks/burnish/enforce"
	"github.com/paulmooreparks/burnish/stylespec"
)

func testProfiles() map[string]*stylespec.Profile {
	return map[string]*stylespec.Profile{
		"test": {
			ID:       "test",
			Register: "essay",
			Language: "en",
			Rules: []stylespec.Rule{
				{ID: "open-with-anecdote", Class: "judged", Statement: "Open with an anecdote.", Support: 0.8},
			},
		},
	}
}

// stubReviser returns a fixed rewrite without any network call.
func stubReviser() enforce.Reviser {
	return func(_ context.Context, _ string, _ enforce.Brief) (string, error) {
		return "stub revised text", nil
	}
}

func do(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	var parsed map[string]any
	if b := rec.Body.Bytes(); len(b) > 0 {
		_ = json.Unmarshal(b, &parsed)
	}
	return rec, parsed
}

func linksOf(t *testing.T, rep map[string]any) map[string]any {
	t.Helper()
	l, ok := rep["_links"].(map[string]any)
	if !ok {
		t.Fatalf("representation has no _links: %v", rep)
	}
	return l
}

// TestHypermediaNavigationFromRoot walks root -> profiles -> profile by following
// links only, never constructing URLs, and confirms every hop is hypermedia.
func TestHypermediaNavigationFromRoot(t *testing.T) {
	h := New(Options{Profiles: testProfiles(), Reviser: stubReviser()}).Handler()

	_, root := do(t, h, "GET", "/", "")
	profilesHref := href(t, linksOf(t, root), "profiles")

	_, coll := do(t, h, "GET", profilesHref, "")
	arr, ok := coll["profiles"].([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("profiles collection wrong: %v", coll["profiles"])
	}
	first := arr[0].(map[string]any)
	profHref := href(t, linksOf(t, first), "self")

	_, prof := do(t, h, "GET", profHref, "")
	pl := linksOf(t, prof)
	for _, rel := range []string{"self", "assessments", "reviews", "massages"} {
		if _, has := pl[rel]; !has {
			t.Errorf("profile missing %q link; links=%v", rel, pl)
		}
	}
}

// TestMassageLinkAbsentWithoutReviser is the core HATEOAS invariant: server
// capability drives the surface. No reviser => no massage link, and a direct POST
// is refused.
func TestMassageLinkAbsentWithoutReviser(t *testing.T) {
	h := New(Options{Profiles: testProfiles()}).Handler() // no reviser

	_, prof := do(t, h, "GET", "/profiles/test", "")
	if _, has := linksOf(t, prof)["massages"]; has {
		t.Error("massages link must be absent when no reviser is configured")
	}

	rec, rep := do(t, h, "POST", "/profiles/test/massages", `{"text":"hello"}`)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("massage POST without reviser = %d, want 405", rec.Code)
	}
	if rep["reason"] != "no_reviser" {
		t.Errorf("expected machine-readable reason=no_reviser, got %v", rep["reason"])
	}
}

func TestMassageLinkPresentWithReviser(t *testing.T) {
	h := New(Options{Profiles: testProfiles(), Reviser: stubReviser()}).Handler()
	_, prof := do(t, h, "GET", "/profiles/test", "")
	if _, has := linksOf(t, prof)["massages"]; !has {
		t.Error("massages link must be present when a reviser is configured")
	}
}

// TestCreateThenGetRoundTrips confirms POST -> 201 + Location -> GET Location for
// each result resource.
func TestCreateThenGetRoundTrips(t *testing.T) {
	h := New(Options{Profiles: testProfiles(), Reviser: stubReviser()}).Handler()

	for _, coll := range []string{"assessments", "reviews", "massages"} {
		rec, rep := do(t, h, "POST", "/profiles/test/"+coll, `{"text":"a draft to process"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s POST = %d, want 201", coll, rec.Code)
		}
		loc := rec.Header().Get("location")
		if loc == "" {
			t.Fatalf("%s POST: no Location header", coll)
		}
		if self := href(t, linksOf(t, rep), "self"); self != loc {
			t.Errorf("%s self link %q != Location %q", coll, self, loc)
		}
		getRec, getRep := do(t, h, "GET", loc, "")
		if getRec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", loc, getRec.Code)
		}
		if getRep["profile"] != "test" {
			t.Errorf("GET %s missing profile field: %v", loc, getRep)
		}
	}
}

func TestReviewIncludesJudgingPrompt(t *testing.T) {
	h := New(Options{Profiles: testProfiles()}).Handler()
	_, rep := do(t, h, "POST", "/profiles/test/reviews", `{"text":"a draft"}`)
	if _, has := rep["judging_prompt"]; !has {
		t.Error("review of a profile with judged rules should include judging_prompt")
	}
}

func TestUnknownProfileIs404WithLinks(t *testing.T) {
	h := New(Options{Profiles: testProfiles()}).Handler()
	rec, rep := do(t, h, "GET", "/profiles/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown profile = %d, want 404", rec.Code)
	}
	if _, has := linksOf(t, rep)["root"]; !has {
		t.Error("404 body should still carry a link back to a valid resource")
	}
}

func TestEmptyDraftIs400(t *testing.T) {
	h := New(Options{Profiles: testProfiles()}).Handler()
	rec, _ := do(t, h, "POST", "/profiles/test/assessments", `{"text":"   "}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty draft = %d, want 400", rec.Code)
	}
}

func href(t *testing.T, l map[string]any, rel string) string {
	t.Helper()
	lk, ok := l[rel].(map[string]any)
	if !ok {
		t.Fatalf("no %q link in %v", rel, l)
	}
	h, _ := lk["href"].(string)
	if h == "" {
		t.Fatalf("%q link has no href", rel)
	}
	return h
}

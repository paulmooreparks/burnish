package stylespec

import (
	"os"
	"path/filepath"
	"testing"
)

// writeProfile saves a minimal profile under dir/<stem>.profile.yaml.
func writeProfile(t *testing.T, dir, stem string, p *Profile) string {
	t.Helper()
	path := filepath.Join(dir, stem+".profile.yaml")
	if err := p.Save(path); err != nil {
		t.Fatalf("save %s: %v", path, err)
	}
	return path
}

func TestListProfiles(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, dir, "essays", &Profile{ID: "paul-essays", Register: "personal-essay", Language: "en"})
	writeProfile(t, dir, "design", &Profile{ID: "design-doc", Register: "long-form-design-doc", Language: "en"})

	infos, err := ListProfiles(dir)
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("want 2 profiles, got %d: %+v", len(infos), infos)
	}
	// Sorted by id: design-doc before paul-essays.
	if infos[0].ID != "design-doc" || infos[1].ID != "paul-essays" {
		t.Errorf("not sorted by id: %s, %s", infos[0].ID, infos[1].ID)
	}
	if infos[1].Register != "personal-essay" {
		t.Errorf("register not surfaced: %+v", infos[1])
	}

	// Empty dir resolves to no profiles and no error (discovery unconfigured).
	none, err := ListProfiles("")
	if err != nil || len(none) != 0 {
		t.Errorf("empty dir: want (nil, nil), got (%v, %v)", none, err)
	}
}

func TestListProfilesSkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, dir, "good", &Profile{ID: "good", Register: "r", Language: "en"})
	// A junk file matching the glob must not fail the whole listing.
	if err := os.WriteFile(filepath.Join(dir, "junk.profile.yaml"), []byte("\t\tnot: [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	infos, err := ListProfiles(dir)
	if err != nil {
		t.Fatalf("ListProfiles should skip the bad file, not error: %v", err)
	}
	if len(infos) != 1 || infos[0].ID != "good" {
		t.Errorf("want only the good profile, got %+v", infos)
	}
}

// TestLooksLikePathIsDeterministic guards the property that path-vs-name
// classification is purely syntactic: a bare name must not flip to "path" just
// because a same-named file exists in the working directory.
func TestLooksLikePathIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, dir, "house", &Profile{ID: "house", Register: "house", Language: "en"})

	// Drop a decoy file named exactly "house" into the working directory.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "house"), []byte("decoy"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(filepath.Join(cwd, "house")) })

	// "house" must still resolve to the profile in dir, not the CWD decoy.
	p, err := ResolveProfile(dir, "house")
	if err != nil {
		t.Fatalf("resolve by name: %v", err)
	}
	if p.ID != "house" || p.Register != "house" {
		t.Errorf("bare name resolved to the wrong file: %+v", p)
	}
}

func TestResolveProfile(t *testing.T) {
	dir := t.TempDir()
	path := writeProfile(t, dir, "essays", &Profile{ID: "paul-essays", Register: "personal-essay", Language: "en"})

	// By filename stem (the fast path).
	if p, err := ResolveProfile(dir, "essays"); err != nil || p.ID != "paul-essays" {
		t.Errorf("by stem: got (%v, %v)", p, err)
	}
	// By profile id (scan path).
	if p, err := ResolveProfile(dir, "paul-essays"); err != nil || p.ID != "paul-essays" {
		t.Errorf("by id: got (%v, %v)", p, err)
	}
	// By register (scan path).
	if p, err := ResolveProfile(dir, "personal-essay"); err != nil || p.ID != "paul-essays" {
		t.Errorf("by register: got (%v, %v)", p, err)
	}
	// By explicit path (dir ignored).
	if p, err := ResolveProfile("", path); err != nil || p.ID != "paul-essays" {
		t.Errorf("by path: got (%v, %v)", p, err)
	}
	// Unknown name is an error.
	if _, err := ResolveProfile(dir, "nonesuch"); err == nil {
		t.Error("unknown name should error")
	}
	// A bare name with no dir is an error, nowhere to resolve it.
	if _, err := ResolveProfile("", "paul-essays"); err == nil {
		t.Error("bare name with empty dir should error")
	}
}

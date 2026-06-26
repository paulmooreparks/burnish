package stylespec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// profileGlob is the on-disk naming convention for a distilled profile. The
// filename stem (the part before .profile.yaml) is a usable reference name in
// addition to the profile's id and register.
const profileGlob = "*.profile.yaml"

// ProfileInfo is lightweight discovery metadata about a profile on disk, so a
// caller can choose one without knowing the filesystem layout. It is what a
// "list profiles" surface returns.
type ProfileInfo struct {
	ID         string   `json:"id"`
	Register   string   `json:"register"`
	Language   string   `json:"language"`
	Path       string   `json:"path"`
	Calibrated bool     `json:"calibrated"`
	Avoided    []string `json:"avoided,omitempty"`
}

// ListProfiles scans dir for *.profile.yaml and returns metadata for each,
// sorted by id. A file that fails to load is skipped rather than failing the
// whole listing, one malformed profile must not hide the good ones. The error
// is returned only when dir itself cannot be read. An empty dir argument
// returns no profiles and no error (discovery is simply unconfigured).
func ListProfiles(dir string) ([]ProfileInfo, error) {
	if dir == "" {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, profileGlob))
	if err != nil {
		return nil, err
	}
	infos := make([]ProfileInfo, 0, len(matches))
	for _, path := range matches {
		p, err := Load(path)
		if err != nil {
			continue
		}
		infos = append(infos, ProfileInfo{
			ID:         p.ID,
			Register:   p.Register,
			Language:   p.Language,
			Path:       path,
			Calibrated: p.Discriminator != nil,
			Avoided:    p.Lexicon.Avoided,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos, nil
}

// ResolveProfile turns a profile reference into a loaded, resolved Profile so a
// caller can name a register instead of knowing where profiles live. ref is
// either:
//
//   - a filesystem path to a profile YAML (it contains a path separator or ends
//     in .yaml/.yml), loaded as-is, dir is ignored; or
//   - a profile name, resolved against dir as <dir>/<name>.profile.yaml first
//     (the filename-stem convention), then, failing that, by scanning dir for a
//     profile whose id or register equals ref.
//
// A bare name with an empty dir is an error: there is nowhere to resolve it.
func ResolveProfile(dir, ref string) (*Profile, error) {
	if ref == "" {
		return nil, fmt.Errorf("no profile reference given")
	}
	if looksLikePath(ref) {
		return Load(ref)
	}
	if dir == "" {
		return nil, fmt.Errorf("profile %q is a name but no profiles directory is configured; pass a path instead", ref)
	}
	// Fast path: the filename-stem convention, <dir>/<ref>.profile.yaml.
	stem := filepath.Join(dir, ref+".profile.yaml")
	if _, err := os.Stat(stem); err == nil {
		return Load(stem)
	}
	// Slow path: scan and match by id or register.
	matches, err := filepath.Glob(filepath.Join(dir, profileGlob))
	if err != nil {
		return nil, err
	}
	for _, path := range matches {
		p, err := Load(path)
		if err != nil {
			continue
		}
		if p.ID == ref || p.Register == ref {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no profile named %q in %s (looked for id, register, or %s.profile.yaml)", ref, dir, ref)
}

// looksLikePath reports whether ref should be treated as a filesystem path
// rather than a profile name: it contains a path separator or carries a YAML
// extension. The test is purely syntactic (it never touches the filesystem) so
// that the same ref resolves identically regardless of the process's working
// directory. A real profile_path always carries the .profile.yaml extension, so
// a bare, extension-less token is unambiguously a name to resolve against dir.
func looksLikePath(ref string) bool {
	if strings.ContainsAny(ref, `/\`) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(ref))
	return ext == ".yaml" || ext == ".yml"
}

// Package serve is the headless HTTP delivery surface: the same engine behind the
// CLI and MCP server, exposed as a REST API for agent-less callers (the .NET
// sidecar for GK Expense, CI). Its API is Fielding-style (HATEOAS): every
// response is a hypermedia representation carrying _links, a client reaches every
// resource by following links from the root, and an action that is not currently
// possible is ABSENT from the representation rather than advertised-and-rejected.
// The clearest case: the "massage" action requires a configured reviser (the
// model adapter), so its link appears on a profile only when one is configured.
//
// Documented deviations from pure REST ("Fielding-style ... wherever practical",
// DESIGN.md section 7):
//   - Link format is a bespoke `_links` convention (rel -> {href, method, title}),
//     not HAL/Siren/Collection+JSON, and the media type stays application/json.
//     The convention is the practical interop contract for the .NET sidecar.
//   - Assessment/review/massage results are ephemeral compute outputs held in a
//     bounded in-memory store, not durable resources; a Location may 404 after
//     eviction. POST creates synchronously (201) rather than 202-accepted + poll,
//     even for massage, which drives up to N model round-trips inline.
package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/paulmooreparks/burnish/enforce"
	"github.com/paulmooreparks/burnish/judge"
	"github.com/paulmooreparks/burnish/lint"
	"github.com/paulmooreparks/burnish/stylespec"
)

// defaultStoreCap bounds the in-memory result store; oldest results are dropped
// past it. Results are ephemeral compute outputs, not durable state.
const defaultStoreCap = 256

// link is one hypermedia link. Method is omitted for plain GET-able relations and
// set explicitly for state-changing transitions (POST), so a client knows how to
// follow it without out-of-band knowledge.
type link struct {
	Href   string `json:"href"`
	Method string `json:"method,omitempty"`
	Title  string `json:"title,omitempty"`
}

type links map[string]link

// Server holds the loaded profiles, the optional reviser, and the bounded store
// of created result resources.
type Server struct {
	profiles map[string]*stylespec.Profile
	reviser  enforce.Reviser // nil => deterministic-only; the massage action is absent

	mu      sync.Mutex
	store   map[string]any
	order   []string
	seq     int
	storeCap int
}

// Options configures a Server.
type Options struct {
	Profiles map[string]*stylespec.Profile
	Reviser  enforce.Reviser // optional; nil disables the massage action
	StoreCap int             // optional; defaults to defaultStoreCap
}

// New builds a Server from loaded profiles and an optional reviser.
func New(opts Options) *Server {
	cap := opts.StoreCap
	if cap <= 0 {
		cap = defaultStoreCap
	}
	return &Server{
		profiles: opts.Profiles,
		reviser:  opts.Reviser,
		store:    make(map[string]any),
		storeCap: cap,
	}
}

// LoadProfiles scans dir for *.profile.yaml, loads and resolves each, and keys
// them by profile ID. A profile whose ID collides with one already loaded is an
// error rather than a silent overwrite.
func LoadProfiles(dir string) (map[string]*stylespec.Profile, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.profile.yaml"))
	if err != nil {
		return nil, err
	}
	out := make(map[string]*stylespec.Profile)
	for _, path := range matches {
		p, err := stylespec.Load(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		if p.ID == "" {
			return nil, fmt.Errorf("load %s: profile has no id", path)
		}
		if _, dup := out[p.ID]; dup {
			return nil, fmt.Errorf("duplicate profile id %q (%s)", p.ID, path)
		}
		out[p.ID] = p
	}
	return out, nil
}

// Handler returns the HTTP handler. Routes are method+pattern (Go 1.22+ mux);
// path wildcards are read with r.PathValue.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /profiles", s.handleProfiles)
	mux.HandleFunc("GET /profiles/{id}", s.handleProfile)
	mux.HandleFunc("POST /profiles/{id}/assessments", s.handleCreateAssessment)
	mux.HandleFunc("POST /profiles/{id}/reviews", s.handleCreateReview)
	mux.HandleFunc("POST /profiles/{id}/massages", s.handleCreateMassage)
	mux.HandleFunc("GET /assessments/{n}", s.handleGetResult("assessments"))
	mux.HandleFunc("GET /reviews/{n}", s.handleGetResult("reviews"))
	mux.HandleFunc("GET /massages/{n}", s.handleGetResult("massages"))
	return mux
}

// --- root + profiles ----------------------------------------------------------

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    "burnish",
		"version": "0.1.0",
		"_links": links{
			"self":     {Href: "/"},
			"profiles": {Href: "/profiles", Title: "available style profiles"},
		},
	})
}

func (s *Server) handleProfiles(w http.ResponseWriter, _ *http.Request) {
	ids := make([]string, 0, len(s.profiles))
	for id := range s.profiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	list := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		p := s.profiles[id]
		list = append(list, map[string]any{
			"id":       id,
			"register": p.Register,
			"_links":   links{"self": {Href: "/profiles/" + id}},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"_links":   links{"self": {Href: "/profiles"}, "root": {Href: "/"}},
		"profiles": list,
	})
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, ok := s.profiles[id]
	if !ok {
		s.notFound(w, fmt.Sprintf("no profile %q", id))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       p.ID,
		"register": p.Register,
		"language": p.Language,
		"corpus":   p.Corpus,
		"features": len(p.Features),
		"rules":    len(p.Rules),
		"_links":   s.profileLinks(id),
	})
}

// profileLinks builds a profile's hypermedia links. The massages transition is
// included ONLY when a reviser is configured: the illegal-action-absent rule that
// makes server capability drive the API surface.
func (s *Server) profileLinks(id string) links {
	l := links{
		"self":        {Href: "/profiles/" + id},
		"collection":  {Href: "/profiles"},
		"assessments": {Href: "/profiles/" + id + "/assessments", Method: http.MethodPost, Title: "score a draft against this profile"},
		"reviews":     {Href: "/profiles/" + id + "/reviews", Method: http.MethodPost, Title: "get a revision payload for a draft"},
	}
	if s.reviser != nil {
		l["massages"] = link{Href: "/profiles/" + id + "/massages", Method: http.MethodPost, Title: "drive a draft toward this profile's style"}
	}
	return l
}

// --- result creation ----------------------------------------------------------

type draftRequest struct {
	Text string `json:"text"`
}

func (s *Server) handleCreateAssessment(w http.ResponseWriter, r *http.Request) {
	p, draft, ok := s.profileAndDraft(w, r)
	if !ok {
		return
	}
	res, err := lint.Check(draft, p)
	if err != nil {
		s.badRequest(w, err.Error())
		return
	}
	rv := judge.CheckRules(draft, p.Rules)
	loc, rep := s.create("assessments", p.ID, map[string]any{
		"profile":         p.ID,
		"distance":        res.Distance,
		"on_target":       res.OnTarget,
		"threshold":       res.Threshold,
		"hard_violations": res.HardViolations,
		"features":        res.Features,
		"lexical":         res.Lexical,
		"rule_violations": rv,
	}, links{
		"reviews": {Href: "/profiles/" + p.ID + "/reviews", Method: http.MethodPost},
	})
	created(w, loc, rep)
}

func (s *Server) handleCreateReview(w http.ResponseWriter, r *http.Request) {
	p, draft, ok := s.profileAndDraft(w, r)
	if !ok {
		return
	}
	res, err := lint.Check(draft, p)
	if err != nil {
		s.badRequest(w, err.Error())
		return
	}
	rv := judge.CheckRules(draft, p.Rules)
	rep := map[string]any{
		"profile":           p.ID,
		"distance":          res.Distance,
		"on_target":         res.OnTarget,
		"threshold":         res.Threshold,
		"hard_violations":   res.HardViolations,
		"features":          res.Features,
		"lexical":           res.Lexical,
		"preferred_lexicon": p.Lexicon.Preferred,
		"avoided_lexicon":   p.Lexicon.Avoided,
		"rules":             p.Rules,
		"rule_violations":   rv,
		"guidance": "Revise to bring off-target features into range, remove avoided terms, favor the preferred " +
			"lexicon, and fix the deterministic rule_violations. For judged rules, run judging_prompt in a fresh, " +
			"isolated context, never the one that wrote the draft.",
	}
	if len(judge.JudgedRules(p.Rules)) > 0 {
		rep["judging_prompt"] = judge.JudgingPrompt(draft, p.Rules)
	}
	extra := links{}
	if s.reviser != nil {
		extra["massages"] = link{Href: "/profiles/" + p.ID + "/massages", Method: http.MethodPost}
	}
	loc, full := s.create("reviews", p.ID, rep, extra)
	created(w, loc, full)
}

func (s *Server) handleCreateMassage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, ok := s.profiles[id]
	if !ok {
		s.notFound(w, fmt.Sprintf("no profile %q", id))
		return
	}
	// The massage action is hypermedia-gated: with no reviser configured its link
	// is absent from the profile representation, so a hypermedia client never
	// arrives here. A direct (out-of-band) caller gets 405, not 404: the profile
	// and the collection exist; the method is unsupported in this server's current
	// configuration. A machine-readable reason lets that caller tell this apart
	// from a missing profile without parsing prose.
	if s.reviser == nil {
		s.actionUnavailable(w, "no_reviser", "massage is unavailable: no reviser configured (set ANTHROPIC_API_KEY)", links{
			"profile": {Href: "/profiles/" + id},
		})
		return
	}
	draft, ok := readDraft(w, r, s)
	if !ok {
		return
	}
	out, err := enforce.Massage(r.Context(), draft, p, nil, s.reviser, enforce.Options{})
	if err != nil {
		s.serverError(w, err.Error())
		return
	}
	loc, rep := s.create("massages", id, map[string]any{
		"profile":    id,
		"final":      out.Final,
		"accepted":   out.Accepted,
		"revisions":  out.Revisions,
		"trajectory": out.Trajectory,
	}, links{
		"assessments": {Href: "/profiles/" + id + "/assessments", Method: http.MethodPost},
	})
	created(w, loc, rep)
}

func (s *Server) handleGetResult(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := kind + "/" + r.PathValue("n")
		s.mu.Lock()
		rep, ok := s.store[key]
		s.mu.Unlock()
		if !ok {
			s.notFound(w, fmt.Sprintf("no %s resource", kind))
			return
		}
		writeJSON(w, http.StatusOK, rep)
	}
}

// --- helpers ------------------------------------------------------------------

// create stores a representation, attaches self + profile + any extra links, and
// returns its Location and the stored representation.
func (s *Server) create(kind, profileID string, rep map[string]any, extra links) (string, map[string]any) {
	s.mu.Lock()
	s.seq++
	id := s.seq
	key := fmt.Sprintf("%s/%d", kind, id)
	loc := "/" + key

	l := links{
		"self":    {Href: loc},
		"profile": {Href: "/profiles/" + profileID},
	}
	for rel, lk := range extra {
		l[rel] = lk
	}
	rep["_links"] = l

	s.store[key] = rep
	s.order = append(s.order, key)
	for len(s.order) > s.storeCap {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.store, oldest)
	}
	s.mu.Unlock()
	return loc, rep
}

func (s *Server) profileAndDraft(w http.ResponseWriter, r *http.Request) (*stylespec.Profile, string, bool) {
	id := r.PathValue("id")
	p, ok := s.profiles[id]
	if !ok {
		s.notFound(w, fmt.Sprintf("no profile %q", id))
		return nil, "", false
	}
	draft, ok := readDraft(w, r, s)
	if !ok {
		return nil, "", false
	}
	return p, draft, true
}

func readDraft(w http.ResponseWriter, r *http.Request, s *Server) (string, bool) {
	var req draftRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		s.badRequest(w, "request body must be JSON {\"text\": \"...\"}")
		return "", false
	}
	if strings.TrimSpace(req.Text) == "" {
		s.badRequest(w, "text is required and must be non-empty")
		return "", false
	}
	return req.Text, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func created(w http.ResponseWriter, location string, rep any) {
	w.Header().Set("location", location)
	writeJSON(w, http.StatusCreated, rep)
}

func (s *Server) notFound(w http.ResponseWriter, msg string) {
	s.notFoundLinks(w, msg, links{"root": {Href: "/"}})
}

func (s *Server) notFoundLinks(w http.ResponseWriter, msg string, l links) {
	if _, has := l["root"]; !has {
		l["root"] = link{Href: "/"}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": msg, "_links": l})
}

// actionUnavailable signals that a known action is not supported in the server's
// current configuration (e.g. massage with no reviser). 405, not 404: the
// resource exists; the method is unsupported in this state. reason is a
// machine-readable discriminator for out-of-band callers.
func (s *Server) actionUnavailable(w http.ResponseWriter, reason, msg string, l links) {
	if _, has := l["root"]; !has {
		l["root"] = link{Href: "/"}
	}
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": msg, "reason": reason, "_links": l})
}

func (s *Server) badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": msg, "_links": links{"root": {Href: "/"}}})
}

func (s *Server) serverError(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": msg, "_links": links{"root": {Href: "/"}}})
}

// ListenAndServe builds the handler and serves on addr until the context is done.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	fmt.Fprintf(os.Stderr, "burnish serve listening on %s (%d profiles, reviser=%t)\n", addr, len(s.profiles), s.reviser != nil)
	return srv.ListenAndServe()
}

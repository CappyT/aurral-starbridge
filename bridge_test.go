package main

import (
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	testUser     = "CappyT"
	testPassword = "p&ss=w?rd#%+"
	testToken    = "jwt-token"
)

type fakeNavidrome struct {
	songs []song
}

func (f *fakeNavidrome) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		if err := json.UnmarshalRead(r.Body, &in); err != nil {
			t.Errorf("login body: %v", err)
		}
		if in["username"] != testUser || in["password"] != testPassword {
			http.Error(w, `{"error":"Invalid username or password"}`, http.StatusUnauthorized)
			return
		}
		json.MarshalWrite(w, map[string]string{"token": testToken})
	})
	mux.HandleFunc("GET /api/song", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-ND-Authorization") != "Bearer "+testToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		if q.Get("starred") != "true" || q.Get("missing") != "false" || q.Get("path") != flowsDir {
			t.Errorf("unexpected song query %v", q)
		}
		var out []song
		for _, s := range f.songs {
			if strings.HasPrefix(s.Path, q.Get("path")) {
				out = append(out, s)
			}
		}
		json.MarshalWrite(w, out)
	})
	return mux
}

type fakeAurral struct {
	mu        sync.Mutex
	jobs      []map[string]any
	paths     map[string]string
	owned     map[string]bool // trackName -> already in library
	fail      map[string]bool // trackName -> 500
	adds      []map[string]any
	fileCalls int
}

func (f *fakeAurral) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Forwarded-User") != testUser || r.Header.Get("X-Forwarded-Groups") != "administrators" {
				http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
				return
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			next(w, r)
		}
	}
	mux.HandleFunc("GET /api/playlists/jobs", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "done" {
			t.Errorf("jobs status = %q", r.URL.Query().Get("status"))
		}
		json.MarshalWrite(w, f.jobs)
	}))
	mux.HandleFunc("GET /api/playlists/jobs/{id}/files", auth(func(w http.ResponseWriter, r *http.Request) {
		f.fileCalls++
		p, ok := f.paths[r.PathValue("id")]
		if !ok {
			http.Error(w, `{"error":"Track not found"}`, http.StatusNotFound)
			return
		}
		json.MarshalWrite(w, map[string]any{"paths": []string{p}})
	}))
	mux.HandleFunc("POST /api/library/downloads/track", auth(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		if err := json.UnmarshalRead(r.Body, &in); err != nil {
			t.Errorf("add body: %v", err)
		}
		f.adds = append(f.adds, in)
		name, _ := in["trackName"].(string)
		switch {
		case f.fail[name]:
			http.Error(w, `{"error":"Failed to queue track acquisition"}`, http.StatusInternalServerError)
		case f.owned[name]:
			json.MarshalWrite(w, map[string]any{"success": true, "alreadyOwned": true, "queued": false})
		default:
			w.WriteHeader(http.StatusAccepted)
			json.MarshalWrite(w, map[string]any{"success": true, "queued": false, "reused": true, "jobId": "lib-" + name})
		}
	}))
	return mux
}

func flowJob(id, artist, title string) map[string]any {
	return map[string]any{
		"id": id, "artistName": artist, "trackName": title, "albumName": "Album",
		"trackMbid": "mbid-" + id, "durationMs": 180000, "trackNumber": 3,
		"albumTrackTitles": []string{"a", "b"}, "playlistType": "flow-1", "status": "done",
	}
}

func newTestBridge(t *testing.T, nd *fakeNavidrome, au *fakeAurral) *bridge {
	t.Helper()
	nsrv := httptest.NewServer(nd.handler(t))
	asrv := httptest.NewServer(au.handler(t))
	t.Cleanup(nsrv.Close)
	t.Cleanup(asrv.Close)
	return &bridge{
		nd:            &navidrome{baseURL: nsrv.URL, user: testUser, password: testPassword, http: nsrv.Client()},
		au:            &aurral{baseURL: asrv.URL, user: testUser, groups: "administrators", http: asrv.Client()},
		navidromeRoot: "/aurral",
		aurralRoot:    "/data/aurral",
		log:           slog.New(slog.NewJSONHandler(io.Discard, nil)),
		handled:       map[string]bool{},
		jobPaths:      map[string]string{},
	}
}

func TestAurralPath(t *testing.T) {
	b := &bridge{navidromeRoot: "/aurral", aurralRoot: "/data/aurral"}
	cases := []struct {
		name string
		s    song
		want string
		ok   bool
	}{
		{"flow song", song{LibraryPath: "/aurral", Path: "_flows/f1/A/B/01 - T.flac"}, "/data/aurral/_flows/f1/A/B/01 - T.flac", true},
		{"trailing slash root", song{LibraryPath: "/aurral/", Path: "_flows/f1/x.mp3"}, "/data/aurral/_flows/f1/x.mp3", true},
		{"other library", song{LibraryPath: "/music", Path: "_flows/f1/x.mp3"}, "", false},
		{"library folder of aurral", song{LibraryPath: "/aurral", Path: "Artist/Album/x.mp3"}, "", false},
		{"dot dot escape", song{LibraryPath: "/aurral", Path: "_flows/../Artist/x.mp3"}, "", false},
		{"missing file", song{LibraryPath: "/aurral", Path: "_flows/f1/x.mp3", Missing: true}, "", false},
	}
	for _, c := range cases {
		got, ok := b.aurralPath(c.s)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestCycleKeepsStarredFlowTracks(t *testing.T) {
	nd := &fakeNavidrome{songs: []song{
		{ID: "s1", LibraryPath: "/aurral", Path: "_flows/f1/A/Album/01 - One.flac", Artist: "A", Title: "One"},
		{ID: "s2", LibraryPath: "/aurral", Path: "_flows/f1/A/Album/02 - Two.flac", Artist: "A", Title: "Two"},
		{ID: "s3", LibraryPath: "/aurral", Path: "_flows/f1/A/Album/03 - Three.flac", Artist: "A", Title: "Three"},
		{ID: "s4", LibraryPath: "/music", Path: "_flows/f1/A/Album/01 - One.flac", Artist: "A", Title: "One"},
		{ID: "s5", LibraryPath: "/aurral", Path: "_flows/f1/A/Album/05 - Orphan.flac", Artist: "A", Title: "Orphan"},
		{ID: "s6", LibraryPath: "/aurral", Path: "A/Album/06 - Library.flac", Artist: "A", Title: "Library"},
	}}
	au := &fakeAurral{
		jobs: []map[string]any{
			flowJob("j1", "A", "One"), flowJob("j2", "A", "Two"), flowJob("j3", "A", "Three"),
			{"id": "lib", "artistName": "A", "trackName": "Library", "playlistType": "library", "status": "done"},
		},
		paths: map[string]string{
			"j1": "/data/aurral/_flows/f1/A/Album/01 - One.flac",
			"j2": "/data/aurral/_flows/f1/A/Album/02 - Two.flac",
			"j3": "/data/aurral/_flows/f1/A/Album/03 - Three.flac",
		},
		owned: map[string]bool{"Three": true},
		fail:  map[string]bool{"One": true},
	}
	b := newTestBridge(t, nd, au)

	st := b.cycle(t.Context())
	want := cycleStats{Starred: 4, Kept: 1, AlreadyKept: 1, Unmatched: 1, Failed: 1}
	if st != want {
		t.Fatalf("first cycle stats = %+v, want %+v", st, want)
	}
	if len(au.adds) != 3 {
		t.Fatalf("add calls = %d, want 3", len(au.adds))
	}
	for _, add := range au.adds {
		if add["trackName"] == "Two" {
			if add["trackMbid"] != "mbid-j2" || add["albumName"] != "Album" || add["durationMs"] != float64(180000) || add["trackNumber"] != float64(3) {
				t.Errorf("payload does not carry the job fields: %v", add)
			}
			if _, ok := add["id"]; ok {
				t.Errorf("payload carries the job id: %v", add)
			}
		}
	}
	if !b.handled["s2"] || !b.handled["s3"] || b.handled["s1"] || b.handled["s5"] {
		t.Errorf("handled = %v", b.handled)
	}

	// The failed track is retried, the kept ones are not, and job paths come from the cache.
	au.fail = nil
	fileCalls := au.fileCalls
	st = b.cycle(t.Context())
	want = cycleStats{Starred: 4, Kept: 1, AlreadyKept: 2, Unmatched: 1}
	if st != want {
		t.Fatalf("second cycle stats = %+v, want %+v", st, want)
	}
	if len(au.adds) != 4 || au.adds[3]["trackName"] != "One" {
		t.Errorf("second cycle adds = %v", au.adds[3:])
	}
	if au.fileCalls != fileCalls {
		t.Errorf("job files fetched again: %d -> %d", fileCalls, au.fileCalls)
	}
}

func TestCycleNoPendingSkipsAurral(t *testing.T) {
	nd := &fakeNavidrome{songs: []song{{ID: "s1", LibraryPath: "/music", Path: "_flows/x.flac"}}}
	au := &fakeAurral{}
	b := newTestBridge(t, nd, au)
	if st := b.cycle(t.Context()); st != (cycleStats{}) {
		t.Errorf("stats = %+v", st)
	}
	if au.fileCalls != 0 || len(au.adds) != 0 {
		t.Errorf("aurral called without pending songs")
	}
}

func TestCycleNavidromeLoginFails(t *testing.T) {
	b := newTestBridge(t, &fakeNavidrome{}, &fakeAurral{})
	b.nd.password = "wrong"
	if st := b.cycle(t.Context()); st.Failed != 1 {
		t.Errorf("stats = %+v, want one failure", st)
	}
}

func TestAurralSendsGroups(t *testing.T) {
	b := newTestBridge(t, &fakeNavidrome{}, &fakeAurral{})
	b.au.groups = ""
	if _, err := b.au.doneJobs(t.Context()); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("err = %v, want 403 without the admin group", err)
	}
}

func TestLoadConfig(t *testing.T) {
	env := map[string]string{"NAVIDROME_URL": "http://navidrome:4533", "NAVIDROME_USER": "CappyT", "NAVIDROME_PASSWORD": "x"}
	c, err := loadConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.AurralURL != "http://127.0.0.1:3001" || c.AurralUser != "CappyT" || c.AurralGroups != "administrators" ||
		c.NavidromeRoot != "/aurral" || c.AurralRoot != "/data/aurral" || c.Interval.Minutes() != 15 {
		t.Errorf("defaults = %+v", c)
	}
	_, err = loadConfig(func(k string) string { return map[string]string{"INTERVAL": "0s"}[k] })
	for _, want := range []string{"NAVIDROME_URL", "NAVIDROME_USER", "NAVIDROME_PASSWORD", "INTERVAL"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to name %s", err, want)
		}
	}
}

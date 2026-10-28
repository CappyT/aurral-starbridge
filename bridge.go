package main

import (
	"context"
	"log/slog"
	"path"
	"strings"
)

type bridge struct {
	nd            *navidrome
	au            *aurral
	navidromeRoot string
	aurralRoot    string
	log           *slog.Logger

	// handled holds the Navidrome song ids that Aurral already keeps.
	handled map[string]bool
	// jobPaths caches the file path of each done Aurral job.
	jobPaths map[string]string
}

type cycleStats struct {
	Starred, Kept, AlreadyKept, Unmatched, Failed int
}

// aurralPath maps a Navidrome song to the Aurral file path. ok is false when the song
// is not in the Aurral library of Navidrome or not in the flows folder.
func (b *bridge) aurralPath(s song) (string, bool) {
	if path.Clean(s.LibraryPath) != b.navidromeRoot || s.Missing {
		return "", false
	}
	rel := path.Clean(s.Path)
	if !strings.HasPrefix(rel, flowsDir) {
		return "", false
	}
	return path.Join(b.aurralRoot, rel), true
}

func (b *bridge) cycle(ctx context.Context) cycleStats {
	var st cycleStats
	songs, err := b.nd.starredFlowSongs(ctx)
	if err != nil {
		b.log.Error("list navidrome stars", "err", err)
		st.Failed++
		return st
	}
	pending := map[string]song{}
	for _, s := range songs {
		p, ok := b.aurralPath(s)
		if !ok {
			continue
		}
		st.Starred++
		if b.handled[s.ID] {
			st.AlreadyKept++
			continue
		}
		pending[p] = s
	}
	if len(pending) == 0 {
		return st
	}

	byPath, err := b.flowJobsByPath(ctx)
	if err != nil {
		b.log.Error("list aurral jobs", "err", err)
		st.Failed += len(pending)
		return st
	}
	for p, s := range pending {
		j, ok := byPath[p]
		if !ok {
			b.log.Warn("no aurral flow job for starred song", "artist", s.Artist, "title", s.Title, "path", p)
			st.Unmatched++
			continue
		}
		res, err := b.au.addToLibrary(ctx, j.trackFields)
		if err != nil {
			b.log.Error("keep track", "artist", s.Artist, "title", s.Title, "path", p, "err", err)
			st.Failed++
			continue
		}
		b.handled[s.ID] = true
		if res.AlreadyOwned {
			st.AlreadyKept++
			b.log.Info("track already in library", "artist", s.Artist, "title", s.Title, "path", p)
			continue
		}
		st.Kept++
		b.log.Info("kept track", "artist", s.Artist, "title", s.Title, "path", p, "job", j.ID, "queued", res.Queued, "reused", res.Reused)
	}
	return st
}

// flowJobsByPath indexes the done non-Library Aurral jobs by file path. It fetches the
// path of a job only once.
func (b *bridge) flowJobsByPath(ctx context.Context) (map[string]job, error) {
	jobs, err := b.au.doneJobs(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(jobs))
	byPath := make(map[string]job, len(jobs))
	for _, j := range jobs {
		seen[j.ID] = true
		p, ok := b.jobPaths[j.ID]
		if !ok {
			if p, err = b.au.jobPath(ctx, j.ID); err != nil {
				return nil, err
			}
			b.jobPaths[j.ID] = p
		}
		if p != "" {
			byPath[path.Clean(p)] = j
		}
	}
	for id := range b.jobPaths {
		if !seen[id] {
			delete(b.jobPaths, id)
		}
	}
	return byPath, nil
}

package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
)

// trackFields is the track payload of POST /api/library/downloads/track, with the same
// fields the flow page of the Aurral UI sends from a flow job.
type trackFields struct {
	ArtistName       string   `json:"artistName"`
	TrackName        string   `json:"trackName"`
	AlbumName        *string  `json:"albumName"`
	ArtistMbid       *string  `json:"artistMbid"`
	AlbumMbid        *string  `json:"albumMbid"`
	ReleaseMbid      *string  `json:"releaseMbid"`
	TrackMbid        *string  `json:"trackMbid"`
	ReleaseYear      *string  `json:"releaseYear"`
	DurationMs       *int64   `json:"durationMs"`
	TrackNumber      *int     `json:"trackNumber"`
	DiscNumber       *int     `json:"discNumber"`
	AlbumTrackCount  *int     `json:"albumTrackCount"`
	AlbumTrackTitles []string `json:"albumTrackTitles"`
}

type job struct {
	trackFields
	ID           string `json:"id"`
	PlaylistType string `json:"playlistType"`
	Status       string `json:"status"`
}

type addResult struct {
	AlreadyOwned bool `json:"alreadyOwned"`
	Queued       bool `json:"queued"`
	Reused       bool `json:"reused"`
}

// aurral calls the Aurral API through its auth proxy headers. Aurral syncs the user role
// from the groups header on every request, so a request without the admin group demotes the user.
type aurral struct {
	baseURL string
	user    string
	groups  string
	http    *http.Client
}

func (a *aurral) request(ctx context.Context, method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("X-Forwarded-User", a.user)
	req.Header.Set("X-Forwarded-Groups", a.groups)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return doJSON(a.http, req, out)
}

// doneJobs lists the completed jobs that are not Library jobs: flow and playlist tracks.
func (a *aurral) doneJobs(ctx context.Context) ([]job, error) {
	var jobs []job
	if err := a.request(ctx, http.MethodGet, "/api/playlists/jobs?status=done", nil, &jobs); err != nil {
		return nil, fmt.Errorf("aurral jobs: %w", err)
	}
	out := jobs[:0]
	for _, j := range jobs {
		if j.Status == "done" && j.PlaylistType != "library" {
			out = append(out, j)
		}
	}
	return out, nil
}

// jobPath returns the file path of a job. The endpoint needs the admin role.
func (a *aurral) jobPath(ctx context.Context, id string) (string, error) {
	var out struct {
		Paths []string `json:"paths"`
	}
	if err := a.request(ctx, http.MethodGet, "/api/playlists/jobs/"+url.PathEscape(id)+"/files", nil, &out); err != nil {
		return "", fmt.Errorf("aurral job %s files: %w", id, err)
	}
	if len(out.Paths) == 0 {
		return "", nil
	}
	return out.Paths[0], nil
}

// addToLibrary is the "Add to library" action of the flow page. Aurral moves the flow
// file into the Library when the track matches a done flow job.
func (a *aurral) addToLibrary(ctx context.Context, t trackFields) (addResult, error) {
	var out addResult
	if err := a.request(ctx, http.MethodPost, "/api/library/downloads/track", t, &out); err != nil {
		return out, fmt.Errorf("aurral add to library: %w", err)
	}
	return out, nil
}

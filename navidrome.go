package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	flowsDir = "_flows/"
	pageSize = 500
)

// song is a media file from the Navidrome native API. Path is relative to LibraryPath.
type song struct {
	ID          string `json:"id"`
	LibraryPath string `json:"libraryPath"`
	Path        string `json:"path"`
	Artist      string `json:"artist"`
	Title       string `json:"title"`
	Missing     bool   `json:"missing"`
}

type navidrome struct {
	baseURL  string
	user     string
	password string
	http     *http.Client
}

func (n *navidrome) login(ctx context.Context) (string, error) {
	body, err := json.Marshal(map[string]string{"username": n.user, "password": n.password})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+"/auth/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Token string `json:"token"`
	}
	if err := doJSON(n.http, req, &out); err != nil {
		return "", fmt.Errorf("navidrome login: %w", err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("navidrome login: empty token")
	}
	return out.Token, nil
}

// starredFlowSongs returns the songs the user starred whose path starts with _flows/.
// The library root filter runs in the caller.
func (n *navidrome) starredFlowSongs(ctx context.Context) ([]song, error) {
	token, err := n.login(ctx)
	if err != nil {
		return nil, err
	}
	var all []song
	for start := 0; ; start += pageSize {
		q := url.Values{
			"starred": {"true"},
			"missing": {"false"},
			"path":    {flowsDir},
			"_start":  {strconv.Itoa(start)},
			"_end":    {strconv.Itoa(start + pageSize)},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.baseURL+"/api/song?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-ND-Authorization", "Bearer "+token)
		var page []song
		if err := doJSON(n.http, req, &page); err != nil {
			return nil, fmt.Errorf("navidrome starred songs: %w", err)
		}
		all = append(all, page...)
		if len(page) < pageSize {
			return all, nil
		}
	}
}

func doJSON(c *http.Client, req *http.Request, out any) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &httpError{Status: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
	}
	return json.UnmarshalRead(resp.Body, out)
}

type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string { return fmt.Sprintf("http %d: %s", e.Status, e.Body) }

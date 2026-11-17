package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type PRStatus struct {
	State string `json:"state"` // open | draft | merged | closed | unknown
	Title string `json:"title,omitempty"`
}

func (s PRStatus) final() bool { return s.State == "merged" || s.State == "closed" }

type prCacheEntry struct {
	status PRStatus
	at     time.Time
}

// PRClient fetches PR status from the GitHub API with a short-lived cache.
type PRClient struct {
	mu        sync.Mutex
	cache     map[string]prCacheEntry
	ttl       time.Duration
	token     string
	tokenOnce sync.Once
	http      *http.Client
}

func NewPRClient() *PRClient {
	return &PRClient{
		cache: map[string]prCacheEntry{},
		ttl:   2 * time.Minute,
		http:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *PRClient) getToken() string {
	c.tokenOnce.Do(func() {
		if t := os.Getenv("GITHUB_TOKEN"); t != "" {
			c.token = t
			return
		}
		if t := os.Getenv("GH_TOKEN"); t != "" {
			c.token = t
			return
		}
		out, err := exec.Command("gh", "auth", "token").Output()
		if err != nil {
			log.Printf("github token not found (GITHUB_TOKEN/GH_TOKEN unset, gh auth token failed: %v); requesting unauthenticated", err)
			return
		}
		c.token = strings.TrimSpace(string(out))
	})
	return c.token
}

// Get returns statuses for refs of the form "owner/repo/number".
func (c *PRClient) Get(ctx context.Context, refs []string) map[string]PRStatus {
	out := make(map[string]PRStatus, len(refs))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, ref := range refs {
		if len(strings.Split(ref, "/")) != 3 {
			continue
		}
		wg.Add(1)
		go func(ref string) {
			defer wg.Done()
			st := c.get(ctx, ref)
			mu.Lock()
			out[ref] = st
			mu.Unlock()
		}(ref)
	}
	wg.Wait()
	return out
}

func (c *PRClient) get(ctx context.Context, ref string) PRStatus {
	c.mu.Lock()
	// merged/closed are final, so they never expire.
	if e, ok := c.cache[ref]; ok && (e.status.final() || time.Since(e.at) < c.ttl) {
		c.mu.Unlock()
		return e.status
	}
	c.mu.Unlock()

	st, err := c.fetch(ctx, ref)
	if err != nil {
		log.Printf("pr status unknown: ref=%s err=%v", ref, err)
		return PRStatus{State: "unknown"}
	}
	c.mu.Lock()
	c.cache[ref] = prCacheEntry{status: st, at: time.Now()}
	c.mu.Unlock()
	return st
}

func (c *PRClient) fetch(ctx context.Context, ref string) (PRStatus, error) {
	p := strings.Split(ref, "/")
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%s", p[0], p[1], p[2])
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return PRStatus{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if t := c.getToken(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return PRStatus{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return PRStatus{}, fmt.Errorf("github: %s (token=%t, ratelimit-remaining=%s): %s",
			res.Status, c.getToken() != "", res.Header.Get("X-RateLimit-Remaining"), strings.TrimSpace(string(snippet)))
	}
	var body struct {
		State  string `json:"state"`
		Draft  bool   `json:"draft"`
		Merged bool   `json:"merged"`
		Title  string `json:"title"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return PRStatus{}, err
	}
	st := PRStatus{State: body.State, Title: body.Title}
	switch {
	case body.Merged:
		st.State = "merged"
	case body.State == "open" && body.Draft:
		st.State = "draft"
	}
	return st, nil
}

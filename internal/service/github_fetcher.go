package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code-rag-assistant/internal/config"
)

type GitHubFetcher struct {
	cfg    config.Config
	client *http.Client
}

type GitHubRepoRef struct {
	Owner string
	Name  string
}

func NewGitHubFetcher(cfg config.Config) *GitHubFetcher {
	return &GitHubFetcher{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.GitHubTimeout},
	}
}

func (f *GitHubFetcher) Parse(raw string) (GitHubRepoRef, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return GitHubRepoRef{}, err
	}
	if parsed.Host != "github.com" {
		return GitHubRepoRef{}, errors.New("only github.com public repositories are supported")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return GitHubRepoRef{}, errors.New("expected GitHub URL format: https://github.com/{owner}/{repo}")
	}
	name := strings.TrimSuffix(parts[1], ".git")
	if parts[0] == "" || name == "" {
		return GitHubRepoRef{}, errors.New("invalid GitHub repository URL")
	}
	return GitHubRepoRef{Owner: parts[0], Name: name}, nil
}

func (f *GitHubFetcher) DownloadZip(ctx context.Context, ref GitHubRepoRef, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	branches := []string{"main", "master"}
	var lastErr error
	for _, branch := range branches {
		zipURL := fmt.Sprintf("https://github.com/%s/%s/archive/refs/heads/%s.zip", ref.Owner, ref.Name, branch)
		path := filepath.Join(destDir, fmt.Sprintf("%s-%s-%d.zip", ref.Owner, ref.Name, time.Now().UnixNano()))
		if err := f.download(ctx, zipURL, path); err != nil {
			lastErr = err
			continue
		}
		return path, nil
	}
	if lastErr == nil {
		lastErr = errors.New("download failed")
	}
	return "", lastErr
}

func (f *GitHubFetcher) download(ctx context.Context, zipURL, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zipURL, nil)
	if err != nil {
		return err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", zipURL, resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, f.cfg.MaxRepoBytes+1)
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	written, err := io.Copy(out, limited)
	if err != nil {
		return err
	}
	if written > f.cfg.MaxRepoBytes {
		_ = os.Remove(path)
		return fmt.Errorf("repository zip exceeds %d bytes", f.cfg.MaxRepoBytes)
	}
	return nil
}

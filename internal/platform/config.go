package platform

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	ServerMessage          string  `json:"server_message"`
	MaxUploads             int     `json:"max_uploads"`
	MaxUploadsPerPrincipal int     `json:"max_uploads_per_principal"`
	UploadTimeoutSeconds   int     `json:"upload_timeout_seconds"`
	Listen                 string  `json:"listen"`
	DataDir                string  `json:"data_dir"`
	BaseURL                string  `json:"base_url"`
	ContentURL             string  `json:"content_url"`
	GitLabURL              string  `json:"gitlab_url"`
	ClientID               string  `json:"client_id"`
	ClientSecret           string  `json:"client_secret"`
	AdminIDs               []int64 `json:"admin_ids"`
	MaxArchiveBytes        int64   `json:"max_archive_bytes"`
	MinFreeBytes           uint64  `json:"min_free_bytes"`
	ExtractionSeconds      int     `json:"extraction_seconds"`
	GCSeconds              int     `json:"gc_seconds"`
	WorkerMemoryMB         uint64  `json:"worker_memory_mb"`
}

func LoadConfig(path string) (Config, error) {
	c := Config{MaxUploads: 4, MaxUploadsPerPrincipal: 1, UploadTimeoutSeconds: 120, Listen: "127.0.0.1:8080", DataDir: "data", MaxArchiveBytes: 128 << 20, MinFreeBytes: 1 << 30, ExtractionSeconds: 120, GCSeconds: 600, WorkerMemoryMB: 2048}
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	for _, s := range []string{c.BaseURL, c.ContentURL, c.GitLabURL} {
		u, e := url.Parse(s)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return c, errors.New("base_url, content_url and gitlab_url must be HTTPS origins without trailing slash")
		}
	}
	if c.BaseURL == c.ContentURL {
		return c, errors.New("management and content origins must differ")
	}
	for _, uid := range c.AdminIDs {
		if uid <= 0 {
			return c, errors.New("admin_ids must be positive")
		}
	}
	if c.ClientID == "" || c.ClientSecret == "" || len(c.AdminIDs) == 0 {
		return c, errors.New("OAuth credentials and admin_ids required")
	}
	if c.MaxArchiveBytes < 1 || c.MaxArchiveBytes > 8<<30 || c.MinFreeBytes > 1<<60 || c.ExtractionSeconds < 1 || c.ExtractionSeconds > 86400 || c.GCSeconds < 1 || c.WorkerMemoryMB < 256 || c.WorkerMemoryMB > 1<<20 {
		return c, errors.New("invalid resource limits")
	}
	if c.MaxUploads < 2 || c.MaxUploads > 128 || c.MaxUploadsPerPrincipal < 1 || c.MaxUploadsPerPrincipal >= c.MaxUploads || c.UploadTimeoutSeconds < 1 || c.UploadTimeoutSeconds > 3600 {
		return c, errors.New("invalid upload limits (per principal must be less than global)")
	}
	c.ServerMessage, e = validateServerMessage(c.ServerMessage)
	if e != nil {
		return c, e
	}
	c.DataDir, e = filepath.Abs(c.DataDir)
	return c, e
}

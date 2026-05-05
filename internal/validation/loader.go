package validation

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// Router is the kin-openapi router interface used for request matching.
// It is satisfied by a single gorillamux router or a multiRouter wrapping
// several per-spec routers.
type Router = routers.Router

// multiRouter combines multiple per-spec routers into one. FindRoute tries
// each sub-router in order and returns the first match.
type multiRouter struct {
	routers []routers.Router
}

func (m *multiRouter) FindRoute(req *http.Request) (*routers.Route, map[string]string, error) {
	for _, r := range m.routers {
		route, params, err := r.FindRoute(req)
		if err == nil {
			return route, params, nil
		}
		if err != routers.ErrPathNotFound && err != routers.ErrMethodNotAllowed {
			return nil, nil, err
		}
	}
	return nil, nil, routers.ErrPathNotFound
}

// DownloadAndLoad downloads the ZIP at url, extracts the models/ subtree to
// dir, then builds a router from the extracted specs. If the download fails and
// dir already contains specs, those are used as fallback. Returns nil router
// (not an error) when no specs are available.
func DownloadAndLoad(ctx context.Context, url, dir string) (Router, error) {
	if err := downloadAndExtract(ctx, url, dir); err != nil {
		slog.Warn("validation: spec download failed, falling back to cached specs", "error", err)
	}
	return LoadFromDir(dir)
}

// LoadFromDir walks dir recursively, parses every *.json file as an OpenAPI
// spec, and builds a single router. Returns nil router (not an error) when no
// valid specs are found.
func LoadFromDir(dir string) (Router, error) {
	var docs []*openapi3.T
	loader := openapi3.NewLoader()

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.ToLower(filepath.Ext(path)) != ".json" {
			return nil
		}
		doc, loadErr := loader.LoadFromFile(path)
		if loadErr != nil {
			slog.Warn("validation: skipping invalid spec file", "path", path, "error", loadErr)
			return nil
		}
		docs = append(docs, doc)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("validation: walking spec dir: %w", err)
	}
	if len(docs) == 0 {
		return nil, nil
	}
	return buildRouter(docs)
}

func buildRouter(docs []*openapi3.T) (Router, error) {
	if len(docs) == 1 {
		r, err := gorillamux.NewRouter(docs[0])
		if err != nil {
			return nil, fmt.Errorf("validation: building router: %w", err)
		}
		return r, nil
	}

	rs := make([]routers.Router, 0, len(docs))
	for _, doc := range docs {
		r, err := gorillamux.NewRouter(doc)
		if err != nil {
			slog.Warn("validation: skipping spec when building router", "error", err)
			continue
		}
		rs = append(rs, r)
	}
	if len(rs) == 0 {
		return nil, fmt.Errorf("validation: no valid routers could be built")
	}
	return &multiRouter{routers: rs}, nil
}

func downloadAndExtract(ctx context.Context, url, dir string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: unexpected status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("opening zip: %w", err)
	}

	stage, err := os.MkdirTemp(filepath.Dir(dir), "specs-stage-*")
	if err != nil {
		return fmt.Errorf("creating staging dir: %w", err)
	}
	defer os.RemoveAll(stage)

	for _, f := range zr.File {
		if err := extractModelsFile(f, stage); err != nil {
			slog.Warn("validation: skipping zip entry", "name", f.Name, "error", err)
		}
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clearing specs dir: %w", err)
	}
	if err := os.Rename(stage, dir); err != nil {
		return fmt.Errorf("installing specs: %w", err)
	}
	return nil
}

func extractModelsFile(f *zip.File, destDir string) error {
	parts := strings.SplitN(f.Name, "/", 2)
	if len(parts) < 2 {
		return nil
	}
	rel := parts[1]

	const prefix = "models/"
	if !strings.HasPrefix(rel, prefix) {
		return nil
	}
	rel = strings.TrimPrefix(rel, prefix)
	if rel == "" || strings.HasSuffix(rel, "/") {
		return nil
	}

	dest := filepath.Join(destDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

package validation_test

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spiohq/smart-proxy/internal/validation"
)

// minimalSpec is the smallest valid OpenAPI 3.0 spec that kin-openapi accepts.
const minimalSpec = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "paths": {
    "/test/v1/items": {
      "get": {
        "operationId": "listItems",
        "parameters": [
          {
            "name": "marketplaceId",
            "in": "query",
            "required": true,
            "schema": {"type": "string"}
          }
        ],
        "responses": {"200": {"description": "OK"}}
      }
    }
  }
}`

const invalidJSON = `{ this is not valid json`

// makeZip builds an in-memory ZIP that mimics the amzn/selling-partner-api-models
// archive structure: files live under "selling-partner-api-models-main/models/...".
func makeZip(files map[string]string) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, _ := w.Create(name)
		f.Write([]byte(content))
	}
	w.Close()
	return buf.Bytes()
}

func TestLoadFromDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	router, err := validation.LoadFromDir(dir)
	require.NoError(t, err)
	assert.Nil(t, router)
}

func TestLoadFromDir_InvalidJSONSkipped(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.json"), []byte(invalidJSON), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "good.json"), []byte(minimalSpec), 0644))
	router, err := validation.LoadFromDir(dir)
	require.NoError(t, err)
	assert.NotNil(t, router)
}

func TestLoadFromDir_NestedSubdirs(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "orders", "v0")
	require.NoError(t, os.MkdirAll(sub, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sub, "orders.json"), []byte(minimalSpec), 0644))
	router, err := validation.LoadFromDir(dir)
	require.NoError(t, err)
	assert.NotNil(t, router)
}

func TestExtractAndLoadZIP_OnlyModelsExtracted(t *testing.T) {
	zipData := makeZip(map[string]string{
		"selling-partner-api-models-main/models/orders/orders.json": minimalSpec,
		"selling-partner-api-models-main/README.md":                 "readme content",
		"selling-partner-api-models-main/other/file.json":           minimalSpec,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer srv.Close()

	dir := t.TempDir()
	router, err := validation.DownloadAndLoad(context.Background(), srv.URL, dir)
	require.NoError(t, err)
	assert.NotNil(t, router)

	// Only models/ files should be on disk
	_, err = os.Stat(filepath.Join(dir, "orders", "orders.json"))
	assert.NoError(t, err, "models/ file should be extracted")
	_, err = os.Stat(filepath.Join(dir, "README.md"))
	assert.True(t, os.IsNotExist(err), "non-models file must not be extracted")
}

func TestDownloadAndLoad_DownloadFailureFallsBackToDisk(t *testing.T) {
	// Pre-populate disk cache
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.json"), []byte(minimalSpec), 0644))

	// Point at a server that always fails
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	router, err := validation.DownloadAndLoad(context.Background(), srv.URL, dir)
	require.NoError(t, err)
	assert.NotNil(t, router, "should fall back to cached specs on download failure")
}

func TestDownloadAndLoad_NoSpecsNoFallback(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	router, err := validation.DownloadAndLoad(context.Background(), srv.URL, dir)
	require.NoError(t, err)
	assert.Nil(t, router)
}

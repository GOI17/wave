package gui

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wave/internal/bundle"
	"wave/internal/models"
)

func TestSaveUploadedState(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		wantFormat string
	}{
		{name: "portable bundle", filename: "state.wave", wantFormat: "wave"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", tt.filename)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(part, "portable bundle bytes"); err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteField("dry-run", "true"); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}

			request := httptest.NewRequest("POST", "/api/apply", &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			path, format, cleanup, err := saveUploadedState(request)
			if err != nil {
				t.Fatalf("saveUploadedState() error = %v", err)
			}
			defer cleanup()

			if format != tt.wantFormat {
				t.Fatalf("format = %q, want %q", format, tt.wantFormat)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "portable bundle bytes" {
				t.Fatalf("uploaded data = %q", data)
			}
			if request.FormValue("dry-run") != "true" {
				t.Fatal("dry-run form field was not preserved")
			}
		})
	}
}

func TestSaveUploadedStateRejectsUnsupportedFile(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "state.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, "invalid")
	_ = writer.Close()

	request := httptest.NewRequest("POST", "/api/apply", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	_, _, _, err = saveUploadedState(request)
	if err == nil || !strings.Contains(err.Error(), ".wave archive") {
		t.Fatalf("error = %v, want unsupported file error", err)
	}
}

func TestServerRoutesAreReady(t *testing.T) {
	server := NewServer("0", "9.8.7")
	request := httptest.NewRequest("GET", "http://localhost/api/status", nil)
	recorder := httptest.NewRecorder()

	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 200 {
		t.Fatalf("status code = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("response = %q, want healthy status", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"version":"9.8.7"`) {
		t.Fatalf("response = %q, want runtime version", recorder.Body.String())
	}
}

func TestIndexUsesRuntimeVersionAndDarculaPalette(t *testing.T) {
	server := NewServer("0", "9.8.7")
	request := httptest.NewRequest("GET", "http://localhost/", nil)
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	for _, expected := range []string{"v9.8.7", "#2B2B2B", "#3C3F41", "#A9B7C6", "#CC7832", "#6897BB"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("index does not contain %q", expected)
		}
	}
	if strings.Contains(body, "v1.0.0") {
		t.Fatal("index contains stale hardcoded version")
	}
}

func TestIndexIncludesVimNavigation(t *testing.T) {
	server := NewServer("0", "9.8.7")
	request := httptest.NewRequest("GET", "http://localhost/", nil)
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	for _, expected := range []string{
		`data-tab="capture"`,
		`class="button vim-action`,
		`document.addEventListener('keydown'`,
		`event.key === 'h' || event.key === 'l'`,
		`event.key === 'j' || event.key === 'k'`,
		`event.key === 'Enter' || event.key === ' '`,
		`isTypingTarget(event.target)`,
		`h/l tabs • j/k actions • enter select`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("index does not contain Vim navigation marker %q", expected)
		}
	}
}

func TestIndexIncludesTabbedCaptureInventory(t *testing.T) {
	server := NewServer("0", "9.8.7")
	request := httptest.NewRequest("GET", "http://localhost/", nil)
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	for _, expected := range []string{"showInventory", "inventory.groups.forEach", "inventory-tab", "Will migrate", "Will not migrate"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("index does not contain inventory tab marker %q", expected)
		}
	}
}

func TestIndexIncludesNativeShareAction(t *testing.T) {
	server := NewServer("0", "9.8.7")
	request := httptest.NewRequest("GET", "http://localhost/", nil)
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	for _, expected := range []string{"Share Captured Archive", "function shareState()", "fetch('/api/share'"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("index does not contain native share marker %q", expected)
		}
	}
}

func TestShareHandlerUsesDefaultArchive(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	original := shareArchive
	defer func() { shareArchive = original }()
	var shared string
	shareArchive = func(path string) error {
		shared = path
		return nil
	}
	server := NewServer("0", "1.2.1")
	request := httptest.NewRequest("POST", "http://localhost/api/share", nil)
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || shared != filepath.Join(homeDir, "wave-state.wave") {
		t.Fatalf("status = %d, shared = %q", recorder.Code, shared)
	}
}

func TestStateHandlerDetectsDefaultCapture(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	statePath := createTestBundle(t, homeDir, "wave-state.wave")

	server := NewServer("0", "1.0.1")
	request := httptest.NewRequest("GET", "http://localhost/api/state", nil)
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 200 {
		t.Fatalf("status code = %d, response = %q", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Exists bool   `json:"exists"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Exists || response.Path != statePath {
		t.Fatalf("response = %#v, want detected default state", response)
	}
}

func TestApplyHandlerUsesDefaultCapture(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	_ = createTestBundle(t, homeDir, "wave-state.wave")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("use-default", "true"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	server := NewServer("0", "1.0.1")
	request := httptest.NewRequest("POST", "http://localhost/api/apply", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"success":true`) {
		t.Fatalf("status code = %d, response = %q", recorder.Code, recorder.Body.String())
	}
	for _, expected := range []string{`"summary":"Migration Preview Summary`, `"name":"Dotfiles"`, `"successful":1`} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Fatalf("response = %q, want %q", recorder.Body.String(), expected)
		}
	}
}

func TestApplyHandlerAcceptsUploadedState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "state.wave")
	if err != nil {
		t.Fatal(err)
	}
	bundleData, err := os.ReadFile(createTestBundle(t, t.TempDir(), "uploaded.wave"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bundleData); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	server := NewServer("0", "1.0.1")
	request := httptest.NewRequest("POST", "http://localhost/api/apply", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 200 {
		t.Fatalf("status code = %d, response = %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"success":true`) {
		t.Fatalf("response = %q, want success", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"summary":"Migration Preview Summary`) {
		t.Fatalf("response = %q, want shared preview summary", recorder.Body.String())
	}
}

func TestApplyHandlerRejectsLiveMigration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "state.wave")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, "not read before confirmation")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	server := NewServer("0", "1.0.1")
	request := httptest.NewRequest("POST", "http://localhost/api/apply", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 400 {
		t.Fatalf("status code = %d, response = %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "type APPLY") {
		t.Fatalf("response = %q, want confirmation rejection", recorder.Body.String())
	}
}

func TestRollbackHandlerRequiresConfirmation(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("confirmation", "no")
	_ = writer.Close()

	server := NewServer("0", "1.0.3")
	request := httptest.NewRequest("POST", "http://localhost/api/rollback", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 400 {
		t.Fatalf("status code = %d, response = %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "type ROLLBACK") {
		t.Fatalf("response = %q, want rollback confirmation rejection", recorder.Body.String())
	}
}

func createTestBundle(t *testing.T, homeDir, name string) string {
	t.Helper()
	source := filepath.Join(homeDir, ".vimrc")
	if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("theme = 'darcula'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(homeDir, name)
	state := &models.MigrationState{Version: "1.0.0", Dotfiles: models.DotfilesGroup{Files: []models.DotfileEntry{{Source: source}}}}
	if err := bundle.Create(path, homeDir, state); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServerRejectsCrossOriginAPIRequest(t *testing.T) {
	server := NewServer("0", "1.0.1")
	request := httptest.NewRequest("GET", "http://localhost/api/status", nil)
	request.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()

	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 403 {
		t.Fatalf("status code = %d, want 403", recorder.Code)
	}
}

func TestServerRejectsDifferentLoopbackOrigin(t *testing.T) {
	server := NewServer("0", "1.0.3")
	request := httptest.NewRequest("GET", "http://localhost:8080/api/status", nil)
	request.Header.Set("Origin", "http://localhost:9999")
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)
	if recorder.Code != 403 {
		t.Fatalf("status code = %d, want 403", recorder.Code)
	}
}

func TestServerRejectsNonLoopbackHost(t *testing.T) {
	server := NewServer("0", "1.0.1")
	request := httptest.NewRequest("GET", "http://example.com/api/status", nil)
	recorder := httptest.NewRecorder()

	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 403 {
		t.Fatalf("status code = %d, want 403", recorder.Code)
	}
}

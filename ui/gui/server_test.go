package gui

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveUploadedState(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		wantFormat string
	}{
		{name: "yaml", filename: "state.yaml", wantFormat: "yaml"},
		{name: "short yaml extension", filename: "state.yml", wantFormat: "yaml"},
		{name: "json", filename: "state.json", wantFormat: "json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", tt.filename)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(part, "version: 1.0.0\n"); err != nil {
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
			if string(data) != "version: 1.0.0\n" {
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
	if err == nil || !strings.Contains(err.Error(), "YAML or JSON") {
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

func TestStateHandlerDetectsDefaultCapture(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	statePath := filepath.Join(homeDir, "wave-state.yaml")
	if err := os.WriteFile(statePath, []byte("version: 1.0.0\n"), 0600); err != nil {
		t.Fatal(err)
	}

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
	statePath := filepath.Join(homeDir, "wave-state.yaml")
	state := "version: 1.0.0\ndotfiles:\n  files:\n    - source: /missing/config.lua\n      destination: /target/config.lua\n"
	if err := os.WriteFile(statePath, []byte(state), 0600); err != nil {
		t.Fatal(err)
	}

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
	for _, expected := range []string{`"summary":"Migration Preview Summary`, `"skipped":1`, `Copy config.lua`} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Fatalf("response = %q, want %q", recorder.Body.String(), expected)
		}
	}
}

func TestApplyHandlerAcceptsUploadedState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "state.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "version: 1.0.0\n"); err != nil {
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
	part, err := writer.CreateFormFile("file", "state.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(part, "version: 1.0.0\n")
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
	if !strings.Contains(recorder.Body.String(), "require dry-run") {
		t.Fatalf("response = %q, want dry-run rejection", recorder.Body.String())
	}
}

func TestApplyHandlerReturnsPartialSummaryOnError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	statePath := filepath.Join(homeDir, "wave-state.yaml")
	state := "version: 1.0.0\nenvironment:\n  shell_profile: " + filepath.Join(homeDir, ".zshrc") + "\n  env_vars:\n    'BAD;NAME': value\n"
	if err := os.WriteFile(statePath, []byte(state), 0600); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("use-default", "true")
	_ = writer.WriteField("dry-run", "true")
	_ = writer.Close()

	server := NewServer("0", "1.0.3")
	request := httptest.NewRequest("POST", "http://localhost/api/apply", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 400 {
		t.Fatalf("status code = %d, response = %q", recorder.Code, recorder.Body.String())
	}
	for _, expected := range []string{`"success":false`, `"summary":"Migration Preview Summary`, "invalid environment variable name"} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Fatalf("response = %q, want %q", recorder.Body.String(), expected)
		}
	}
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

func TestServerRejectsNonLoopbackHost(t *testing.T) {
	server := NewServer("0", "1.0.1")
	request := httptest.NewRequest("GET", "http://example.com/api/status", nil)
	recorder := httptest.NewRecorder()

	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 403 {
		t.Fatalf("status code = %d, want 403", recorder.Code)
	}
}

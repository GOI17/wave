package gui

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
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
	server := NewServer("0")
	request := httptest.NewRequest("GET", "http://localhost/api/status", nil)
	recorder := httptest.NewRecorder()

	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 200 {
		t.Fatalf("status code = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("response = %q, want healthy status", recorder.Body.String())
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

	server := NewServer("0")
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

	server := NewServer("0")
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

func TestServerRejectsCrossOriginAPIRequest(t *testing.T) {
	server := NewServer("0")
	request := httptest.NewRequest("GET", "http://localhost/api/status", nil)
	request.Header.Set("Origin", "https://example.com")
	recorder := httptest.NewRecorder()

	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 403 {
		t.Fatalf("status code = %d, want 403", recorder.Code)
	}
}

func TestServerRejectsNonLoopbackHost(t *testing.T) {
	server := NewServer("0")
	request := httptest.NewRequest("GET", "http://example.com/api/status", nil)
	recorder := httptest.NewRecorder()

	server.mux.ServeHTTP(recorder, request)

	if recorder.Code != 403 {
		t.Fatalf("status code = %d, want 403", recorder.Code)
	}
}

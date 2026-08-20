package gui

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wave/internal/analyzer"
	"wave/internal/executor"
	"wave/internal/migrator"
	"wave/internal/transaction"
)

// Server represents the GUI web server
type Server struct {
	port        string
	mux         *http.ServeMux
	operationMu sync.Mutex
	version     string
}

// NewServer creates a new GUI server
func NewServer(port, version string) *Server {
	server := &Server{
		port:    port,
		mux:     http.NewServeMux(),
		version: version,
	}
	server.setupRoutes()
	return server
}

// Start starts the GUI server
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", "127.0.0.1:"+s.port)
	if err != nil {
		return fmt.Errorf("start GUI server: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://localhost:%d", port)

	fmt.Printf("🌊 Wave GUI Server\n")
	fmt.Printf("📡 Listening on: %s\n", url)
	if err := openBrowser(url); err != nil {
		fmt.Printf("⚠ Could not open the browser: %v\n", err)
		fmt.Printf("Open manually: %s\n", url)
	}
	fmt.Println("\nPress Ctrl+C to stop")

	server := &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return server.Serve(listener)
}

func openBrowser(url string) error {
	return exec.Command("/usr/bin/open", url).Run()
}

// setupRoutes sets up HTTP routes
func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/", s.indexHandler)
	s.mux.Handle("/api/capture", s.localOnly(s.exclusive(http.HandlerFunc(s.captureHandler))))
	s.mux.Handle("/api/apply", s.localOnly(s.exclusive(http.HandlerFunc(s.applyHandler))))
	s.mux.Handle("/api/rollback", s.localOnly(s.exclusive(http.HandlerFunc(s.rollbackHandler))))
	s.mux.Handle("/api/status", s.localOnly(http.HandlerFunc(s.statusHandler)))
	s.mux.Handle("/api/state", s.localOnly(http.HandlerFunc(s.stateHandler)))
}

func (s *Server) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if parsedHost, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsedHost
	}
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func (s *Server) exclusive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.operationMu.TryLock() {
			writeJSON(w, http.StatusConflict, map[string]any{"success": false, "error": "another migration operation is running"})
			return
		}
		defer s.operationMu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// indexHandler serves the web UI
func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	html := getIndexHTML(s.version)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, html)
}

// captureHandler captures device state
func (s *Server) captureHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	homeDir, _ := os.UserHomeDir()
	outputPath := filepath.Join(homeDir, "wave-state.wave")

	analyzer := analyzer.NewMacOSAnalyzer(homeDir)
	executor := executor.NewMacOSExecutor(homeDir, false)
	mig := migrator.NewMigrator(analyzer, executor)

	if err := mig.CaptureBundle(outputPath, homeDir); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "file": outputPath})
}

// applyHandler applies captured state
func (s *Server) applyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	dryRun := r.FormValue("dry-run") == "true"
	if !dryRun && r.FormValue("confirmation") != "APPLY" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "type APPLY to confirm live migration"})
		return
	}

	inputPath := ""
	format := "wave"
	cleanup := func() {}
	if r.FormValue("use-default") == "true" {
		homeDir, _ := os.UserHomeDir()
		inputPath = filepath.Join(homeDir, "wave-state.wave")
		if _, err := os.Stat(inputPath); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "default captured state is not available"})
			return
		}
	} else {
		var err error
		inputPath, format, cleanup, err = saveUploadedState(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
			return
		}
	}
	defer cleanup()

	if format != "wave" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "apply and preview require a portable .wave archive"})
		return
	}
	if dryRun {
		result, err := transaction.Preview(inputPath)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "result": result, "summary": migrator.FormatSummary(result)})
		return
	}
	homeDir, _ := os.UserHomeDir()
	journal, err := transaction.Apply(inputPath, homeDir, filepath.Join(homeDir, ".wave", "transactions"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "transaction": journal, "summary": transaction.FormatApplySummary(journal)})
}

func (s *Server) rollbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.FormValue("confirmation") != "ROLLBACK" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "type ROLLBACK to confirm"})
		return
	}
	homeDir, _ := os.UserHomeDir()
	transactionsDir := filepath.Join(homeDir, ".wave", "transactions")
	id := r.FormValue("transaction")
	if id == "" {
		result, err := transaction.RollbackLatest(homeDir, transactionsDir)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "rollback": result, "summary": transaction.FormatRollbackSummary(result)})
		return
	}
	result, err := transaction.Rollback(id, homeDir, transactionsDir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "rollback": result, "summary": transaction.FormatRollbackSummary(result)})
}

func saveUploadedState(r *http.Request) (string, string, func(), error) {
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("read state file: %w", err)
	}
	defer file.Close()
	removeMultipartFiles := func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}

	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(header.Filename)), ".")
	if format == "yml" {
		format = "yaml"
	}
	if format != "wave" {
		removeMultipartFiles()
		return "", "", func() {}, fmt.Errorf("state file must be a .wave archive")
	}

	tempFile, err := os.CreateTemp("", "wave-state-*")
	if err != nil {
		removeMultipartFiles()
		return "", "", func() {}, fmt.Errorf("create temporary state file: %w", err)
	}

	cleanup := func() {
		_ = os.Remove(tempFile.Name())
		removeMultipartFiles()
	}
	if _, err := io.Copy(tempFile, file); err != nil {
		_ = tempFile.Close()
		cleanup()
		return "", "", func() {}, fmt.Errorf("save state file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		cleanup()
		return "", "", func() {}, fmt.Errorf("close state file: %w", err)
	}

	return tempFile.Name(), format, cleanup, nil
}

// statusHandler returns server status
func (s *Server) statusHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

// stateHandler reports whether the default captured state is available.
func (s *Server) stateHandler(w http.ResponseWriter, r *http.Request) {
	homeDir, _ := os.UserHomeDir()
	stateFile := filepath.Join(homeDir, "wave-state.wave")
	info, err := os.Stat(stateFile)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"exists": false, "path": stateFile})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"exists":   true,
		"path":     stateFile,
		"size":     info.Size(),
		"modified": info.ModTime(),
	})
}

// getIndexHTML returns the web UI HTML
func getIndexHTML(version string) string {
	page := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>🌊 Wave – macOS Device Migrator</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            background: #2B2B2B;
			color: #A9B7C6;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }

        .container {
			background: #3C3F41;
			border: 1px solid #515151;
            border-radius: 12px;
            box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
            max-width: 600px;
            width: 100%;
            overflow: hidden;
        }

        .header {
			background: #313335;
			color: #CC7832;
			border-bottom: 1px solid #515151;
            padding: 40px 30px;
            text-align: center;
        }

        .header h1 {
            font-size: 2.5em;
            margin-bottom: 10px;
            font-weight: 700;
        }

        .header p {
            opacity: 0.9;
            font-size: 0.95em;
        }

        .content {
            padding: 40px 30px;
        }

        .section {
            margin-bottom: 30px;
        }

        .section h2 {
            font-size: 1.3em;
			color: #FFC66D;
            margin-bottom: 20px;
            display: flex;
            align-items: center;
        }

        .section h2 span {
            margin-right: 10px;
        }

        .button {
            display: inline-block;
            padding: 12px 24px;
			background: #365880;
			color: #F2F2F2;
			border: 1px solid #4E78A6;
            border: none;
            border-radius: 6px;
            font-size: 1em;
            cursor: pointer;
            transition: transform 0.2s, box-shadow 0.2s;
            width: 100%;
            margin-bottom: 10px;
        }

        .button:hover {
            transform: translateY(-2px);
			background: #416A96;
			box-shadow: 0 8px 18px rgba(0, 0, 0, 0.35);
        }

        .button:active {
            transform: translateY(0);
        }

        .button.secondary {
			background: #4E5254;
			color: #A9B7C6;
            margin-top: 10px;
        }

        .button.secondary:hover {
			background: #5A5E60;
        }

        .status {
            padding: 15px;
            border-radius: 6px;
            margin-top: 15px;
            font-size: 0.95em;
        }

        .status.success {
			background: #294436;
			color: #6A8759;
			border: 1px solid #4E6B55;
        }

        .status.error {
			background: #4A2F32;
			color: #FF6B68;
			border: 1px solid #7A4548;
        }

        .status.info {
			background: #283A4D;
			color: #6897BB;
			border: 1px solid #3D5D78;
        }

		.preview-summary {
			background: #2B2B2B;
			border: 1px solid #515151;
			border-radius: 6px;
			color: #A9B7C6;
			font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
			font-size: 0.82rem;
			line-height: 1.55;
			margin-top: 15px;
			overflow-x: auto;
			padding: 15px;
			white-space: pre-wrap;
		}

        .loading {
            display: block;
            text-align: center;
            padding: 20px;
        }

        .spinner {
            border: 3px solid #f3f3f3;
			border-top: 3px solid #CC7832;
            border-radius: 50%;
            width: 40px;
            height: 40px;
            animation: spin 1s linear infinite;
            margin: 0 auto 10px;
        }

        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }

        .file-input-wrapper {
            margin: 15px 0;
        }

        .file-input-wrapper input[type="file"] {
            display: block;
            width: 100%;
            padding: 10px;
			border: 1px solid #5E6060;
			background: #2B2B2B;
			color: #A9B7C6;
            border-radius: 6px;
            font-size: 0.9em;
        }

        .checkbox {
            margin: 15px 0;
        }

        .checkbox input {
            margin-right: 10px;
        }

        .checkbox label {
            display: inline-block;
            cursor: pointer;
        }

        footer {
			background: #313335;
            padding: 20px 30px;
            text-align: center;
			color: #808080;
            font-size: 0.9em;
			border-top: 1px solid #515151;
        }

        .tabs {
            display: flex;
            gap: 10px;
            margin-bottom: 20px;
			border-bottom: 1px solid #515151;
        }

        .tab {
            padding: 10px 20px;
            background: none;
            border: none;
            cursor: pointer;
            font-size: 1em;
			color: #808080;
            border-bottom: 3px solid transparent;
            transition: all 0.2s;
        }

        .tab.active {
			color: #CC7832;
			border-bottom-color: #CC7832;
        }

        .tab-content {
            display: none;
        }

        .tab-content.active {
            display: block;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🌊 Wave</h1>
			<p>macOS Device Migrator v{{VERSION}}</p>
        </div>

        <div class="content">
            <div class="tabs">
                <button class="tab active" onclick="switchTab(this, 'capture')">📦 Capture</button>
				<button class="tab" onclick="switchTab(this, 'apply')">⚡ Preview</button>
                <button class="tab" onclick="switchTab(this, 'info')">ℹ️ Info</button>
            </div>

            <!-- Capture Tab -->
            <div id="capture" class="tab-content active">
                <div class="section">
                    <h2><span>📦</span>Capture Device State</h2>
                    <p>Export your current device configuration to a file.</p>
                    <button class="button" onclick="captureState()">Start Capture</button>
                    <div id="capture-status"></div>
                </div>
            </div>

            <!-- Apply Tab -->
            <div id="apply" class="tab-content">
                <div class="section">
					<h2><span>⚡</span>Preview, Apply & Rollback</h2>
					<div id="default-state" class="status info" hidden></div>
                    <div class="file-input-wrapper">
                        <label for="state-file">Choose portable archive:</label>
						<input type="file" id="state-file" accept=".wave">
                    </div>
                    <div class="checkbox">
                        <input type="checkbox" id="dry-run" checked disabled>
                        <label for="dry-run">Dry-run required (preview only)</label>
                    </div>
                    <button class="button" onclick="applyState()">Preview Migration</button>
					<div class="file-input-wrapper">
						<label for="apply-confirmation">Type APPLY to confirm changes:</label>
						<input type="text" id="apply-confirmation" autocomplete="off">
					</div>
					<button class="button" onclick="applyLiveState()">Apply Migration</button>
					<div class="file-input-wrapper">
						<label for="rollback-confirmation">Type ROLLBACK to restore the latest transaction:</label>
						<input type="text" id="rollback-confirmation" autocomplete="off">
					</div>
					<button class="button secondary" onclick="rollbackState()">Rollback Latest Migration</button>
                    <div id="apply-status"></div>
                </div>
            </div>

            <!-- Info Tab -->
            <div id="info" class="tab-content">
                <div class="section">
                    <h2><span>ℹ️</span>About Wave</h2>
					<p><strong>Wave v{{VERSION}}</strong> captures and previews macOS migration plans.</p>
                    <h3 style="margin-top: 20px; margin-bottom: 10px; font-size: 1.1em;">Features:</h3>
                    <ul style="margin-left: 20px; line-height: 1.8;">
                        <li>📦 Capture Homebrew apps</li>
                        <li>📝 Save dotfiles and configs</li>
                        <li>⚙️ Export system preferences</li>
                        <li>🔧 Preserve shell environment</li>
                        <li>✅ Verify migrations</li>
                    </ul>
                    <h3 style="margin-top: 20px; margin-bottom: 10px; font-size: 1.1em;">Interfaces:</h3>
                    <ul style="margin-left: 20px; line-height: 1.8;">
                        <li>💻 CLI - Command line interface</li>
                        <li>📊 TUI - Terminal user interface</li>
                        <li>🖥️ GUI - Web-based interface (this)</li>
                    </ul>
                </div>
            </div>
        </div>

        <footer>
			<p>Wave v{{VERSION}} • macOS Device Migrator • Open Source</p>
        </footer>
    </div>

    <script>
        function switchTab(button, tabName) {
            // Hide all tabs
            const contents = document.querySelectorAll('.tab-content');
            contents.forEach(c => c.classList.remove('active'));

            // Remove active class from all tabs
            const tabs = document.querySelectorAll('.tab');
            tabs.forEach(t => t.classList.remove('active'));

            // Show selected tab
            document.getElementById(tabName).classList.add('active');
            button.classList.add('active');
        }

        function showStatus(elementId, message, type) {
            const elem = document.getElementById(elementId);
            elem.replaceChildren();
            const status = document.createElement('div');
            status.className = 'status ' + type;
            status.textContent = message;
            elem.appendChild(status);
        }

		function showPreviewSummary(elementId, summary) {
			const elem = document.getElementById(elementId);
			elem.replaceChildren();
			const report = document.createElement('pre');
			report.className = 'preview-summary';
			report.textContent = summary;
			elem.appendChild(report);
		}

        function captureState() {
            showStatus('capture-status', 'Capturing device state...', 'info');

            fetch('/api/capture', { method: 'POST' })
                .then(r => r.json())
                .then(data => {
                    if (data.success) {
                        showStatus('capture-status', '✅ State captured successfully! File: ' + data.file, 'success');
						loadDefaultState();
                    } else {
                        showStatus('capture-status', '❌ Error: ' + data.error, 'error');
                    }
                })
                .catch(err => showStatus('capture-status', '❌ Error: ' + err.message, 'error'));
        }

		function migrationFormData(dryRun) {
            const fileInput = document.getElementById('state-file');
			const formData = new FormData();

            if (!fileInput.value) {
				if (window.waveDefaultState) {
					formData.append('use-default', 'true');
				} else {
					showStatus('apply-status', '❌ Please select a state file', 'error');
					return null;
				}
			} else {
				formData.append('file', fileInput.files[0]);
            }

            showStatus('apply-status', 'Applying migration preview...', 'info');

            formData.append('dry-run', dryRun);
			return formData;
		}

        function applyState() {
			const formData = migrationFormData(true);
			if (!formData) return;

			showStatus('apply-status', 'Applying migration preview...', 'info');

            fetch('/api/apply', { method: 'POST', body: formData })
                .then(r => r.json())
                .then(data => {
                    if (data.success) {
						showPreviewSummary('apply-status', data.summary);
                    } else {
						if (data.summary) {
							showPreviewSummary('apply-status', data.summary + '\nError: ' + data.error);
						} else {
							showStatus('apply-status', '❌ Error: ' + data.error, 'error');
						}
                    }
                })
                .catch(err => showStatus('apply-status', '❌ Error: ' + err.message, 'error'));
        }

		function applyLiveState() {
			const confirmation = document.getElementById('apply-confirmation').value;
			const formData = migrationFormData(false);
			if (!formData) return;
			formData.append('confirmation', confirmation);
			showStatus('apply-status', 'Applying migration...', 'info');
			fetch('/api/apply', { method: 'POST', body: formData })
				.then(r => r.json())
				.then(data => {
					if (data.success) {
						showPreviewSummary('apply-status', data.summary);
					} else {
						showStatus('apply-status', '❌ Error: ' + data.error, 'error');
					}
				});
		}

		function rollbackState() {
			const formData = new FormData();
			formData.append('confirmation', document.getElementById('rollback-confirmation').value);
			showStatus('apply-status', 'Rolling back latest migration...', 'info');
			fetch('/api/rollback', { method: 'POST', body: formData })
				.then(r => r.json())
				.then(data => {
					if (data.success) {
						showPreviewSummary('apply-status', data.summary);
					} else {
						showStatus('apply-status', '❌ Error: ' + data.error, 'error');
					}
				});
		}

		function loadDefaultState() {
			fetch('/api/state')
				.then(r => r.json())
				.then(data => {
					window.waveDefaultState = data.exists ? data : null;
					const status = document.getElementById('default-state');
					if (data.exists) {
						status.hidden = false;
						status.textContent = 'Using captured state: ' + data.path;
					} else {
						status.hidden = true;
					}
				});
		}

		loadDefaultState();
    </script>
</body>
</html>
	`
	return strings.ReplaceAll(page, "{{VERSION}}", html.EscapeString(version))
}

// StartGUI launches the web GUI
func StartGUI(port, version string) error {
	server := NewServer(port, version)
	return server.Start()
}

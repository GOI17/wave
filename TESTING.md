# Wave v1.0.0 Testing Guide

Complete testing procedures and environment setup for all three interfaces (CLI, TUI, GUI).

---

## Prerequisites

### Required
- macOS 10.14 or later (for testing; can use Docker)
- Go 1.23+ (for building)
- Homebrew (for app testing)

### Optional
- Docker (for isolated testing)
- FFmpeg (for video recording)
- scrot/screenshot tools (for screenshots)

---

## Test Environment Setup

### Option 1: Docker Container (Recommended for Clean Tests)

```bash
# Build test image
docker build -t wave-test -f Dockerfile .

# Run interactive container
docker run -it --name wave-test wave-test /bin/bash

# Inside container
cd /app
make build
```

### Option 2: Local Machine

```bash
# Install dependencies
brew install go

# Navigate to project
cd wave

# Build
make build

# Verify
./wave --version
```

---

## Test Scenarios

### Scenario 1: CLI Interface Testing

**Goal:** Verify CLI capture and apply commands work correctly

#### Test 1.1: Capture Command

```bash
# Command
./wave capture --output test-capture.yaml --format yaml

# Expected Output
# ✅ Should create test-capture.yaml
# ✅ Should contain device info, apps, dotfiles, prefs
# ✅ No errors

# Verification
ls -lah test-capture.yaml
head -30 test-capture.yaml
```

**Screenshots to capture:**
1. `cli-capture-command.png` - Command execution
2. `cli-capture-output.png` - Success output
3. `cli-capture-file.png` - Generated YAML file

#### Test 1.2: Apply Command (Dry-run)

```bash
# Command
./wave apply --input test-capture.yaml --dry-run

# Expected Output
# ✅ Should show "DRY-RUN: Would..." messages
# ✅ Should NOT apply changes
# ✅ Success completion message

# Verification
./wave apply --input test-capture.yaml --dry-run 2>&1 | grep -i "dry-run"
```

**Screenshots to capture:**
1. `cli-apply-dryrun-command.png` - Command execution
2. `cli-apply-dryrun-output.png` - Dry-run output showing what would happen
3. `cli-apply-dryrun-summary.png` - Final summary

#### Test 1.3: Help Commands

```bash
# Main help
./wave --help

# Specific command help
./wave capture --help
./wave apply --help
./wave tui --help
```

**Screenshots to capture:**
1. `cli-help-main.png` - Main help output
2. `cli-help-capture.png` - Capture help
3. `cli-help-apply.png` - Apply help

#### Test 1.4: Version Command

```bash
./wave version
```

**Screenshots to capture:**
1. `cli-version.png` - Version output

---

### Scenario 2: TUI Interface Testing

**Goal:** Verify terminal UI navigation and functionality

#### Test 2.1: TUI Launch

```bash
# Command
./wave tui

# Expected Output
# ✅ Should launch interactive TUI
# ✅ Show menu with options
# ✅ Arrow keys/j/k for navigation
# ✅ Enter to select
# ✅ 'q' to quit
```

**Recording to capture:**
- `tui-demo.mp4` (30-60 seconds)
  - Show menu navigation
  - Select "Capture Device State"
  - Show capture in progress
  - Return to menu
  - Select "Exit"

**Screenshots to capture:**
1. `tui-menu.png` - Main menu
2. `tui-navigation.png` - Menu navigation (cursor movement)
3. `tui-capture-option.png` - Capture option selected
4. `tui-exit.png` - Clean exit

---

### Scenario 3: GUI Interface Testing

**Goal:** Verify web-based GUI functionality

#### Test 3.1: GUI Launch

```bash
# Command
./wave gui --port 8080

# Expected Output
# ✅ Server should start on localhost:8080
# ✅ Web UI should be accessible
```

**Action:**
1. Open browser: http://localhost:8080
2. Verify web interface loads
3. Check tabs load correctly

**Screenshots to capture:**
1. `gui-homepage.png` - Initial page load
2. `gui-header.png` - Wave header/branding
3. `gui-capture-tab.png` - Capture tab
4. `gui-apply-tab.png` - Apply tab
5. `gui-info-tab.png` - Info tab

#### Test 3.2: Capture via GUI

```
Steps:
1. Open http://localhost:8080
2. Ensure on "Capture" tab
3. Click "Start Capture" button
4. Wait for success message
5. Verify file created
```

**Recording to capture:**
- `gui-capture-demo.mp4` (45-60 seconds)
  - Show clicking button
  - Show loading state
  - Show success message
  - Show file confirmation

**Screenshots to capture:**
1. `gui-capture-start.png` - Before capture
2. `gui-capture-loading.png` - Loading spinner
3. `gui-capture-success.png` - Success message with file path

#### Test 3.3: Apply via GUI

```
Steps:
1. Open http://localhost:8080
2. Click "Apply" tab
3. Select a state file
4. Check "Dry-run" checkbox
5. Click "Apply Migration"
6. Verify dry-run output
```

**Recording to capture:**
- `gui-apply-demo.mp4` (45-60 seconds)
  - Show file selection
  - Show dry-run checkbox
  - Show applying
  - Show results

**Screenshots to capture:**
1. `gui-apply-file-select.png` - File input
2. `gui-apply-dryrun-check.png` - Dry-run checkbox
3. `gui-apply-loading.png` - Loading state
4. `gui-apply-success.png` - Success result

#### Test 3.4: Navigation

```
Steps:
1. Click each tab: Capture, Apply, Info
2. Verify content switches correctly
3. Verify styling and layout
```

**Screenshots to capture:**
1. `gui-tab-capture.png` - Capture tab
2. `gui-tab-apply.png` - Apply tab
3. `gui-tab-info.png` - Info tab

---

## Integration Tests

### Test Suite 1: Data Integrity

```bash
# Capture state
./wave capture --output state1.yaml

# Apply (dry-run)
./wave apply --input state1.yaml --dry-run

# Verify YAML is valid
cat state1.yaml | grep -E "version|created_at|device_info"
```

### Test Suite 2: Format Conversion

```bash
# Capture as YAML
./wave capture --output state.yaml --format yaml

# Capture as JSON
./wave capture --output state.json --format json

# Verify both files exist and are valid
file state.yaml state.json
```

### Test Suite 3: Error Handling

```bash
# Try to apply non-existent file
./wave apply --input /nonexistent/file.yaml 2>&1 | grep -i error

# Try invalid format
./wave capture --format invalid 2>&1 | grep -i error

# Try bad permissions
sudo chmod 000 /tmp/test.yaml
./wave apply --input /tmp/test.yaml 2>&1 | grep -i "permission\|denied"
```

---

## Performance Testing

### Capture Performance

```bash
# Measure capture time
time ./wave capture --output state.yaml

# Expected: < 5 seconds for full capture
```

### File Size Analysis

```bash
# Check state file size
ls -lah state.yaml

# Expected: 50KB - 500KB depending on system
```

---

## Browser Compatibility Testing (GUI)

Test GUI in different browsers:

- Chrome/Chromium ✓
- Firefox ✓
- Safari ✓
- Edge ✓

**Test in each browser:**
1. Load http://localhost:8080
2. Test all tabs
3. Test buttons and interactions
4. Check responsive design (mobile)

**Screenshots per browser:**
1. `gui-chrome.png`
2. `gui-firefox.png`
3. `gui-safari.png`

---

## Recording Instructions

### For Videos (TUI & GUI)

#### Using QuickTime (macOS)
```
1. Open QuickTime Player
2. File → New Screen Recording
3. Select area/full screen
4. Click Record
5. Perform test actions
6. Stop recording
7. Save as .mp4
```

#### Using FFmpeg
```bash
# Start screen recording
ffmpeg -f avfoundation -i "1" -t 60 output.mp4

# For audio+video
ffmpeg -f avfoundation -i "1:0" -t 60 output.mp4
```

### For Screenshots

#### Using macOS Screenshot Tool
```bash
# Full screen
screenshot -x full_screenshot.png

# Region
screenshot -x region_screenshot.png

# Window
screenshot -x window_screenshot.png
```

#### Using Command Line
```bash
# Full screenshot
screencapture -x screenshot.png

# Timed (5 second delay)
screencapture -x -T 5 delayed_screenshot.png
```

---

## Test Results Documentation

### Template for Results

Create file: `TEST_RESULTS_v1.0.0.md`

```markdown
# Wave v1.0.0 Test Results

**Date:** [Date]
**Tester:** [Name]
**Environment:** [macOS version, Go version, etc.]

## CLI Tests

### Capture Command
- [x] Basic capture works
- [x] Output file created
- [x] YAML format valid
- [x] Contains all expected fields
- **Status:** ✅ PASS

### Apply Command (Dry-run)
- [x] Accepts state file
- [x] Shows dry-run messages
- [x] No changes applied
- [x] Completes successfully
- **Status:** ✅ PASS

## TUI Tests

### Menu Navigation
- [x] Launches successfully
- [x] Menu displays correctly
- [x] Arrow keys work
- [x] Enter selects options
- [x] 'q' quits cleanly
- **Status:** ✅ PASS

## GUI Tests

### Web Interface
- [x] Server starts on port 8080
- [x] Page loads in browser
- [x] All tabs functional
- [x] Responsive design works
- [x] Capture button works
- **Status:** ✅ PASS

## Overall Status
🎉 **All Tests PASSED** - Ready for v1.0.0 Release
```

---

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Wave Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v2
      - uses: actions/setup-go@v2
        with:
          go-version: 1.23
      - run: make build
      - run: make test
      - run: ./wave --version
```

---

## Test Checklist

- [ ] CLI capture command works
- [ ] CLI apply dry-run works
- [ ] CLI help commands display correctly
- [ ] TUI launches and navigates
- [ ] TUI menu options are selectable
- [ ] GUI starts server on correct port
- [ ] GUI web interface loads
- [ ] GUI capture functionality works
- [ ] GUI apply functionality works
- [ ] All tabs render correctly
- [ ] All error cases handled gracefully
- [ ] Performance is acceptable
- [ ] Output files are valid
- [ ] Cross-browser compatibility (GUI)
- [ ] Responsive design works (GUI)

---

## Known Issues

(To be updated after testing)

---

## Approved for Release

- [ ] All tests passed
- [ ] Documentation complete
- [ ] Code reviewed
- [ ] Performance acceptable
- [ ] Security verified

**Date:** ___________
**Tester:** ___________
**Approver:** ___________

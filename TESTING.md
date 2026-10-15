# Isolated migration testing

## Everyday development: disposable container

Start Docker Desktop, then run from the repository:

```sh
make test-sandbox
```

The command builds `Dockerfile.test`, compiles the portable analyzer, archive, and executor suites with race
instrumentation, and executes them in a disposable Linux container. A failed
suite makes the command and CI job fail. If Docker is unavailable, the command
stops; it never falls back to running tests on your laptop.

Isolation boundaries:

- No host directory, Docker socket, SSH agent, or credential mounts.
- The container runs as a non-root user with all Linux capabilities dropped.
- Its root filesystem is read-only; only temporary directories are writable.
- Test execution has no network. Building the image needs network access for Go
  and dependencies; build steps compile tests without executing them.
- The build context includes only project code/build files, not `.git`, `.env`,
  personal migration archives, or transaction data.
- Temporary test data is deleted with the container. Docker retains the image
  and build cache on the host for subsequent runs.

The full transaction suite uses macOS-specific atomic file operations
(`renameatx_np`). It runs on a GitHub-hosted macOS runner, not in this Linux
container. To run only the new migration scenarios **inside a disposable macOS
VM**:

```sh
go test -race ./internal/transaction -run TestSandbox -v
```

`make test`, `make test-race`, and direct `go test` still run natively. Prefer
those inside a disposable macOS VM or a GitHub-hosted macOS runner. Tests use
temporary file paths, but some analyzer smoke tests still query the OS and its
installed applications. A temporary `HOME` alone does not isolate Homebrew,
`/Applications`, `defaults`, `scutil`, or system-wide changes.

## What the automated tests prove

`internal/transaction/sandbox_test.go` creates separate synthetic source and
target machines. It uses the real dotfile analyzer, `.wave` archive code, and
transaction engine, with an in-memory application manager that never launches
Homebrew, `mas`, or macOS settings commands.

| Scenario | Assertions |
| --- | --- |
| Capture, archive, preview, apply | Target bytes and permissions match the source; source is unchanged; preview creates no target transaction |
| Formula and cask migration | Correct install commands; an existing formula with the same name does not suppress a cask |
| App Store migration | The captured numeric ID is passed to `mas install` |
| Rollback | Original bytes/permissions restored; newly created files removed; installed apps retained as specified by Wave |
| Reapply after rollback | Already installed apps are not installed again |
| Installation failure | Error is returned; the partial transaction can restore files without uninstalling apps |

The existing suites also exercise symlinks, hard links, checksums, corrupted
archives/journals, interrupted transactions, post-apply edits, preference
restoration, and CLI/TUI/GUI handlers. The AppKit/Swift compilation test runs
in the macOS CI job; it is not part of the portable container suite.

These are integration tests of Wave's migration engine. They do not prove that
Apple or Homebrew can download an application, or that real macOS settings were
applied. The CLI/TUI/GUI handler tests are not interactive desktop acceptance tests.

`.github/workflows/test-sandbox.yml` runs the portable container suite and the
full native suite on a GitHub-hosted macOS runner on pushes to `main` and pull
requests, without Apple credentials or a self-hosted runner. The sandbox
workflow does not currently gate release publication; configure required checks
before treating it as a release gate.

## Real migration acceptance: disposable macOS machines

Use a macOS VM for actual formula/cask installations, macOS preferences, and
interactive UI checks. Docker Desktop runs Linux containers and cannot validate
those macOS integrations. On Apple Silicon, [Tart](https://tart.run/quick-start/)
is one option for creating and cloning disposable macOS VMs.

1. Prepare a clean macOS VM with a test user, Go, Homebrew, `mas`, and a copy of
   the Wave revision under test. Keep personal dotfiles and iCloud data out of it.
2. Shut down and preserve that clean VM as the baseline. Create independent
   source and destination clones. Do not attach host home folders or writable
   host shares. Copy project code and `.wave` fixtures into the guests.
3. Inside the **source VM**, create synthetic dotfiles and install a small known
   set of formulas, casks, and an App Store app available to the test account.
   Record the expected bytes, modes, inventory IDs, and preferences independently
   of Wave's own output, then capture a `.wave` archive.
4. Inside the **destination VM**, start with a different `.vimrc` and one
   preinstalled package. Record its original state, preview the archive, and
   assert that preview left files, inventory, preferences, and transactions
   unchanged. Then execute the confirmed Apply inside that guest.
5. Verify files byte-for-byte and by permissions. Compare the expected inventory
   with `brew list --formula`, `brew list --cask`, and `mas list`. Read back each
   expected preference with `defaults read`; launch the migrated applications.
   Run `wave verify --input source.wave` as an additional check: it reads actual
   files, Homebrew versions, and supported preferences, and fails on differences
   or unverified items. Keep the independent comparison: an archive omits
   documents, application data, ACLs, and extended attributes, so verification
   cannot certify a whole-Mac replica. `wave diff` is unimplemented and fails.
6. Run Rollback inside the destination guest. Check the original files and
   preferences are restored and apps are retained/reported. In a separate run,
   edit a migrated file before rollback and verify that Wave reports a conflict
   and preserves the edit.
7. Preserve the logs and expected/actual comparison, discard the test clones,
   and create fresh clones from the baseline for the next run. Resetting the VM,
   rather than Wave's rollback, removes installed apps and resets the full OS.

App Store testing is a separate, interactive acceptance step: confirm that the
chosen VM configuration supports App Store sign-in and that `mas list` and an
installation work with the test account before testing Wave. Account access,
application availability, Spotlight indexing, and VM identity can affect the
result. Do not put Apple credentials in CI or assume a cloned VM retains working
authentication. See [mas requirements](https://github.com/mas-cli/mas) and
[Apple's VM account documentation](https://developer.apple.com/documentation/virtualization/using-icloud-with-macos-virtual-machines).

A second user account on your personal Mac is insufficient isolation: Homebrew,
applications in `/Applications`, and system settings may be shared. Reserve
confirmed real migrations for disposable guest machines.

## Selected application/document replica acceptance

Use `wave replica` when the fixture includes actual documents and `.app` bundle
contents. Create the destination VM from the clean baseline before populating
the source. Populate only the source using an independent copy tool. Record an
independent SHA-256 inventory, file modes, and symlink targets before capture.
Keep personal fixtures, snapshots, inventories, and VM credentials outside Git.

Inside the source guest, run `wave replica capture --output ~/sample-replica`
with repeated `--document Documents/example.pdf` and
`--application Example.app` selections. Transport the entire snapshot with the
metadata-preserving tar commands in README into a fresh destination directory.
Do not mount the host home or enable guest Internet access for personal fixtures.

Inside the destination guest:

1. Confirm the selected items are absent independently of Wave.
2. Run `wave replica apply --input ~/sample-replica --dry-run` and compare the
   destination again to prove preview did not write any selected item.
3. Run `wave replica apply --input ~/sample-replica --confirm`.
4. Run `wave replica verify --input ~/sample-replica`. It must return zero and
   report no differences in the captured content and metadata.
5. Compare the restored files against the independent original inventory.
6. Reapply and verify that no identical item is rewritten. Regression tests
   separately verify that changed documents cause conflicts and remain intact.
7. Compare application code-signature checks with the source. An existing
   source signature failure must be reported separately from transfer damage.

The macOS-only `internal/replica` suite exercises native copying, app-framework
symlinks, hard links, ACLs, extended attributes, metadata-preserving transport,
payload corruption, path traversal, symlink escapes, conflicts, and exclusive
publication. A passing sample establishes fidelity for that selection, not for
all documents, application support data, accounts, or the complete system.

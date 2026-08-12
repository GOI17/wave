# GitHub Repository Setup Instructions

## Step 1: Create Public Repository on GitHub

Go to https://github.com/new and create a new repository with these settings:

- **Repository name:** `wave`
- **Description:** "macOS device migrator with CLI, TUI, and GUI interfaces"
- **Visibility:** Public
- **Initialize with:** None (we already have git history)
- **Add .gitignore:** No (we have one)
- **Add license:** MIT (recommended) or Apache 2.0

## Step 2: Connect Local Repository to GitHub

After creating the repository, copy the remote URL and run:

```bash
cd /Users/golivas/Documents/personal/wave

# Add the remote (replace YOUR_USERNAME with your GitHub username)
git remote add origin https://github.com/YOUR_USERNAME/wave.git

# Verify the remote
git remote -v

# Push the code
git branch -M main
git push -u origin main
```

## Step 3: Create First Release (Stable)

Once pushed, create your first stable release:

```bash
# Create and push a tag
git tag -a v1.0.0 -m "Wave v1.0.0 - macOS device migrator"
git push origin v1.0.0
```

This will trigger:
- ✅ Build for macOS ARM64 and x86_64
- ✅ Publish stable release with binaries
- ✅ Publish Docker image (ghcr.io/YOUR_USERNAME/wave:v1.0.0)

## Step 4: Enable GitHub Actions

The workflow is already in `.github/workflows/build-release.yml`. GitHub Actions should:
- Automatically detect the workflow
- Run on every push to main (beta builds)
- Run on every tag push (stable builds)

No additional setup needed!

## How It Works

### Stable Builds (Release Channel)
- **Triggered by:** Git tags like `v1.0.0`, `v1.1.0`, etc.
- **Output:** 
  - GitHub Release with binaries
  - Docker image: `ghcr.io/username/wave:v1.0.0` + `latest`
- **Status:** Production-ready

### Beta Builds (Development Channel)
- **Triggered by:** Pushes to main branch
- **Output:**
  - GitHub Release (beta tag) with latest binaries
  - Docker image: `ghcr.io/username/wave:beta`
- **Status:** Latest development version, may be unstable

## Next Releases

To create a new stable release:

```bash
# Update version in your code if needed
# Commit changes
git add .
git commit -m "Release v1.0.1"

# Tag it
git tag -a v1.0.1 -m "Wave v1.0.1 - Bug fixes and improvements"
git push origin main
git push origin v1.0.1
```

Then automatically:
1. GitHub Actions detects the tag
2. Builds binaries on macOS runners
3. Creates a GitHub Release with assets
4. Publishes Docker images

## Workflow File Location

The workflow configuration is at: `.github/workflows/build-release.yml`

### Jobs Included:

1. **build** - Builds binaries for ARM64 and x86_64
2. **test** - Runs test suite
3. **publish-stable** - Creates release on version tags
4. **publish-beta** - Updates beta release on main branch pushes
5. **publish-docker** - Publishes Docker images to ghcr.io

## Troubleshooting

If builds fail:
1. Go to: https://github.com/YOUR_USERNAME/wave/actions
2. Click on the failed workflow run
3. Check the logs
4. Common issues:
   - Go version mismatch (uses 1.23)
   - Missing dependencies in go.mod
   - Makefile issues

## Docker Image Usage

After stable release:
```bash
docker pull ghcr.io/username/wave:v1.0.0
docker run --rm ghcr.io/username/wave:v1.0.0 --version
```

Beta builds:
```bash
docker pull ghcr.io/username/wave:beta
docker run --rm ghcr.io/username/wave:beta --version
```

## Security Notes

- GitHub Actions uses automatic `GITHUB_TOKEN` (no secrets needed)
- Beta releases marked as `prerelease: true`
- All builds are deterministic and reproducible
- Docker images pushed to GitHub Container Registry (ghcr.io)

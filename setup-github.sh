#!/bin/bash
# Wave - GitHub Repository Setup Script
# This script helps you push the Wave project to GitHub

set -e

echo "╔════════════════════════════════════════════════════════════════════════╗"
echo "║         WAVE - GitHub Repository Setup (Step by Step)                ║"
echo "╚════════════════════════════════════════════════════════════════════════╝"
echo ""

# Step 1: Check prerequisites
echo "📋 Step 1: Checking prerequisites..."
if ! command -v git &> /dev/null; then
    echo "❌ Git is not installed. Please install Git first."
    exit 1
fi
echo "✓ Git is installed"

# Step 2: Show current status
echo ""
echo "📊 Step 2: Current repository status..."
cd /Users/golivas/Documents/personal/wave
echo "  Branch: $(git branch --show-current)"
echo "  Commits: $(git log --oneline | wc -l)"
echo "  Files: $(git ls-files | wc -l)"
echo "✓ Repository ready"

# Step 3: Instructions
echo ""
echo "📖 Step 3: Manual setup required..."
echo ""
echo "   1. Go to: https://github.com/new"
echo "   2. Fill in:"
echo "      - Repository name: wave"
echo "      - Description: macOS device migrator with CLI, TUI, and GUI"
echo "      - Visibility: Public"
echo "      - Initialize: None (leave all unchecked)"
echo "   3. Click 'Create repository'"
echo ""
echo "   4. After GitHub creates the repo, you'll see something like:"
echo "      git remote add origin https://github.com/YOUR_USERNAME/wave.git"
echo ""
echo "   5. Run these commands in this terminal:"
echo ""

# Step 4: Show the commands
echo "════════════════════════════════════════════════════════════════════════"
echo "COMMANDS TO RUN (replace YOUR_USERNAME with your GitHub username):"
echo "════════════════════════════════════════════════════════════════════════"
echo ""
echo "cd /Users/golivas/Documents/personal/wave"
echo "git remote add origin https://github.com/YOUR_USERNAME/wave.git"
echo "git branch -M main"
echo "git push -u origin main"
echo ""
echo "════════════════════════════════════════════════════════════════════════"
echo ""

# Step 5: Offer to continue
read -p "💬 Have you created the GitHub repository and want to continue? (y/n) " -n 1 -r
echo ""

if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Please create the repository at https://github.com/new and run this script again."
    exit 0
fi

# Step 6: Get username
echo ""
read -p "📝 Enter your GitHub username: " USERNAME

if [ -z "$USERNAME" ]; then
    echo "❌ Username cannot be empty"
    exit 1
fi

# Step 7: Add remote and push
echo ""
echo "🔗 Adding remote repository..."
REMOTE_URL="https://github.com/${USERNAME}/wave.git"
echo "   URL: $REMOTE_URL"

if git remote | grep -q origin; then
    echo "   (Updating existing origin)"
    git remote remove origin
fi

git remote add origin "$REMOTE_URL"
git config user.email "golivas@github.com"
git config user.name "GitHub User"

echo ""
echo "📤 Pushing code to GitHub..."
git branch -M main
git push -u origin main

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Successfully pushed to GitHub!"
    echo ""
    echo "   Repository: https://github.com/${USERNAME}/wave"
    echo "   Branch: main"
    echo ""
    echo "🚀 Next steps:"
    echo ""
    echo "   1. To create your first stable release, run:"
    echo "      git tag -a v1.0.0 -m 'Wave v1.0.0 - macOS device migrator'"
    echo "      git push origin v1.0.0"
    echo ""
    echo "   2. GitHub Actions will automatically:"
    echo "      ✓ Build macOS ARM64 and x86_64 binaries"
    echo "      ✓ Create a GitHub Release with assets"
    echo "      ✓ Publish Docker image: ghcr.io/${USERNAME}/wave:v1.0.0"
    echo ""
    echo "   3. Watch the build progress at:"
    echo "      https://github.com/${USERNAME}/wave/actions"
    echo ""
    echo "   4. Download binaries from:"
    echo "      https://github.com/${USERNAME}/wave/releases"
    echo ""
else
    echo ""
    echo "❌ Failed to push to GitHub"
    echo ""
    echo "Troubleshooting:"
    echo "  • Make sure you have SSH key or HTTPS credentials configured"
    echo "  • Check that the repository exists: https://github.com/${USERNAME}/wave"
    echo "  • Try pushing manually: git push -u origin main"
    exit 1
fi

echo ""
echo "════════════════════════════════════════════════════════════════════════"
echo "✨ Setup complete! Your Wave repository is now on GitHub."
echo "════════════════════════════════════════════════════════════════════════"

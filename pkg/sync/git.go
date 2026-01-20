package sync

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Clone clones a git repository to the specified directory
// If GH_TOKEN environment variable is set, it will be used for authentication
func Clone(repoURL, dest string) error {
	// Inject GH_TOKEN into HTTPS URLs for authentication
	cloneURL := injectAuthToken(repoURL)

	cmd := exec.Command("git", "clone", "--depth=1", cloneURL, dest)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	// Fetch all branches (shallow clone only gets default branch)
	fetchCmd := exec.Command("git", "-C", dest, "fetch", "--all", "--depth=1")
	fetchCmd.Stderr = os.Stderr
	if err := fetchCmd.Run(); err != nil {
		// Non-fatal, continue
		fmt.Fprintf(os.Stderr, "[sync] warning: git fetch --all failed: %v\n", err)
	}

	return nil
}

// CheckoutBranch checks out a branch, creating it from baseBranch if it doesn't exist
// Handles empty repositories by creating an initial commit first
func CheckoutBranch(repoDir, baseBranch, branch string) error {
	// Check if repo is empty (no commits)
	revParseCmd := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD")
	if err := revParseCmd.Run(); err != nil {
		// Empty repo - create initial commit on base branch
		fmt.Fprintf(os.Stderr, "[sync] Empty repository detected, initializing with first commit\n")

		// Create and checkout the base branch
		checkoutOrphan := exec.Command("git", "-C", repoDir, "checkout", "--orphan", baseBranch)
		checkoutOrphan.Stderr = os.Stderr
		if err := checkoutOrphan.Run(); err != nil {
			return fmt.Errorf("failed to create orphan branch %s: %w", baseBranch, err)
		}

		// Create initial README
		readmePath := repoDir + "/README.md"
		readme := "# Shadow Manifests\n\nThis repository contains rendered Kubernetes manifests for PR review.\n"
		if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
			return fmt.Errorf("failed to write README: %w", err)
		}

		// Stage and commit
		addCmd := exec.Command("git", "-C", repoDir, "add", "README.md")
		addCmd.Stderr = os.Stderr
		if err := addCmd.Run(); err != nil {
			return fmt.Errorf("failed to stage README: %w", err)
		}

		commitCmd := exec.Command("git", "-C", repoDir, "commit", "-m", "Initial commit: Shadow repository setup")
		commitCmd.Stderr = os.Stderr
		if err := commitCmd.Run(); err != nil {
			return fmt.Errorf("failed to create initial commit: %w", err)
		}

		// Push initial commit to establish the base branch
		pushCmd := exec.Command("git", "-C", repoDir, "push", "-u", "origin", baseBranch)
		pushCmd.Stderr = os.Stderr
		if err := pushCmd.Run(); err != nil {
			return fmt.Errorf("failed to push initial commit: %w", err)
		}

		fmt.Fprintf(os.Stderr, "[sync] Initialized %s branch with initial commit\n", baseBranch)
	} else {
		// Normal case: checkout the base branch
		checkoutBase := exec.Command("git", "-C", repoDir, "checkout", baseBranch)
		checkoutBase.Stderr = os.Stderr
		if err := checkoutBase.Run(); err != nil {
			return fmt.Errorf("failed to checkout base branch %s: %w", baseBranch, err)
		}

		// Pull latest from base (ignore errors for new repos)
		pullCmd := exec.Command("git", "-C", repoDir, "pull", "--ff-only", "origin", baseBranch)
		pullCmd.Stderr = os.Stderr
		pullCmd.Run() // Ignore errors
	}

	// Create/reset the target branch from base
	// Using -B creates or resets the branch
	checkoutCmd := exec.Command("git", "-C", repoDir, "checkout", "-B", branch)
	checkoutCmd.Stderr = os.Stderr
	if err := checkoutCmd.Run(); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", branch, err)
	}

	return nil
}

// CommitAll stages all changes and commits them
// Returns (changed, sha, error) where changed indicates if there were changes to commit
func CommitAll(repoDir, message string) (bool, string, error) {
	// Stage all changes
	addCmd := exec.Command("git", "-C", repoDir, "add", "-A")
	addCmd.Stderr = os.Stderr
	if err := addCmd.Run(); err != nil {
		return false, "", fmt.Errorf("git add failed: %w", err)
	}

	// Check if there are changes to commit
	statusCmd := exec.Command("git", "-C", repoDir, "status", "--porcelain")
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return false, "", fmt.Errorf("git status failed: %w", err)
	}

	if len(strings.TrimSpace(string(statusOutput))) == 0 {
		// No changes to commit
		return false, "", nil
	}

	// Commit changes
	commitCmd := exec.Command("git", "-C", repoDir, "commit", "-m", message)
	commitCmd.Stderr = os.Stderr
	if err := commitCmd.Run(); err != nil {
		return false, "", fmt.Errorf("git commit failed: %w", err)
	}

	// Get the commit SHA
	shaCmd := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD")
	shaOutput, err := shaCmd.Output()
	if err != nil {
		return true, "", fmt.Errorf("failed to get commit SHA: %w", err)
	}

	sha := strings.TrimSpace(string(shaOutput))
	if len(sha) > 7 {
		sha = sha[:7]
	}

	return true, sha, nil
}

// Push pushes the branch to the remote
// For shadow repos (generated content), we use --force since --force-with-lease
// requires having a local ref to compare against, which we don't have after a fresh clone
func Push(repoDir, remote, branch string, force bool) error {
	args := []string{"-C", repoDir, "push", remote, branch}
	if force {
		args = append(args, "--force")
	}

	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}

	return nil
}

// GitURLFromSlug converts a GitHub slug (owner/repo) to a git URL
// Supports multiple formats:
//   - owner/repo -> https://github.com/owner/repo.git
//   - https://github.com/owner/repo -> https://github.com/owner/repo.git
//   - git@github.com:owner/repo.git -> git@github.com:owner/repo.git
func GitURLFromSlug(slug string) string {
	// Already a full URL
	if strings.HasPrefix(slug, "https://") || strings.HasPrefix(slug, "git@") {
		return slug
	}

	// Simple slug format: owner/repo
	return fmt.Sprintf("https://github.com/%s.git", slug)
}

// ParseRepoSlug extracts owner/repo from various git URL formats
func ParseRepoSlug(input string) (string, error) {
	// Already in slug format
	if !strings.Contains(input, "://") && !strings.HasPrefix(input, "git@") {
		if strings.Count(input, "/") == 1 {
			return strings.TrimSuffix(input, ".git"), nil
		}
	}

	// HTTPS URL
	if strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err != nil {
			return "", fmt.Errorf("invalid URL: %w", err)
		}
		path := strings.TrimPrefix(u.Path, "/")
		path = strings.TrimSuffix(path, "/")
		path = strings.TrimSuffix(path, ".git")
		return path, nil
	}

	// SSH URL: git@github.com:owner/repo.git
	if strings.HasPrefix(input, "git@") {
		re := regexp.MustCompile(`git@[^:]+:(.+?)(?:\.git)?$`)
		matches := re.FindStringSubmatch(input)
		if len(matches) == 2 {
			return matches[1], nil
		}
	}

	return "", fmt.Errorf("cannot parse repo slug from: %s", input)
}

// CompareURL generates a GitHub compare URL
func CompareURL(repoSlug, baseBranch, headBranch string) string {
	slug := strings.TrimSuffix(repoSlug, ".git")
	if !strings.Contains(slug, "/") {
		// Invalid slug, return empty
		return ""
	}

	// If slug is a URL, parse it
	if strings.Contains(slug, "://") || strings.HasPrefix(slug, "git@") {
		parsed, err := ParseRepoSlug(slug)
		if err != nil {
			return ""
		}
		slug = parsed
	}

	return fmt.Sprintf("https://github.com/%s/compare/%s...%s", slug, baseBranch, headBranch)
}

// injectAuthToken injects GH_TOKEN into HTTPS URLs for authentication
// Converts: https://github.com/owner/repo.git
// To:       https://x-access-token:TOKEN@github.com/owner/repo.git
func injectAuthToken(repoURL string) string {
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		return repoURL
	}

	// Only inject into HTTPS GitHub URLs
	if !strings.HasPrefix(repoURL, "https://github.com/") {
		return repoURL
	}

	// Replace https://github.com/ with https://x-access-token:TOKEN@github.com/
	return strings.Replace(repoURL, "https://github.com/", fmt.Sprintf("https://x-access-token:%s@github.com/", token), 1)
}

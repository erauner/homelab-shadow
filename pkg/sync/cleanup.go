package sync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// CleanupResult contains the results of branch cleanup
type CleanupResult struct {
	CheckedBranches []string `json:"checked_branches"`
	DeletedBranches []string `json:"deleted_branches"`
	SkippedBranches []string `json:"skipped_branches"`
	Errors          []string `json:"errors,omitempty"`
}

// PRState represents the state of a GitHub PR
type PRState struct {
	Number int
	State  string // "open", "closed", "merged"
}

// CleanupStaleBranches removes pr-* branches from the shadow repo
// where the corresponding PR in the source repo is closed/merged
func CleanupStaleBranches(shadowRepoPath, sourceRepo string, dryRun bool, verbose bool) (CleanupResult, error) {
	result := CleanupResult{
		CheckedBranches: []string{},
		DeletedBranches: []string{},
		SkippedBranches: []string{},
		Errors:          []string{},
	}

	// List all remote branches in shadow repo
	branches, err := listRemotePRBranches(shadowRepoPath)
	if err != nil {
		return result, fmt.Errorf("failed to list branches: %w", err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Found %d pr-* branches to check\n", len(branches))
	}

	// Check each PR branch
	prPattern := regexp.MustCompile(`^pr-(\d+)$`)
	for _, branch := range branches {
		result.CheckedBranches = append(result.CheckedBranches, branch)

		matches := prPattern.FindStringSubmatch(branch)
		if matches == nil {
			continue
		}
		prNumber := matches[1]

		// Check PR state via GitHub API
		state, err := getPRState(sourceRepo, prNumber)
		if err != nil {
			errMsg := fmt.Sprintf("failed to check PR #%s: %v", prNumber, err)
			result.Errors = append(result.Errors, errMsg)
			if verbose {
				fmt.Fprintf(os.Stderr, "  %s: error - %v\n", branch, err)
			}
			continue
		}

		if state == "open" {
			result.SkippedBranches = append(result.SkippedBranches, branch)
			if verbose {
				fmt.Fprintf(os.Stderr, "  %s: PR still open, skipping\n", branch)
			}
			continue
		}

		// PR is closed/merged - delete the branch
		if verbose {
			fmt.Fprintf(os.Stderr, "  %s: PR %s, ", branch, state)
			if dryRun {
				fmt.Fprintf(os.Stderr, "would delete (dry-run)\n")
			} else {
				fmt.Fprintf(os.Stderr, "deleting...\n")
			}
		}

		if !dryRun {
			if err := deleteRemoteBranch(shadowRepoPath, branch); err != nil {
				errMsg := fmt.Sprintf("failed to delete %s: %v", branch, err)
				result.Errors = append(result.Errors, errMsg)
				continue
			}
		}
		result.DeletedBranches = append(result.DeletedBranches, branch)
	}

	return result, nil
}

// listRemotePRBranches lists all pr-* branches from origin
func listRemotePRBranches(repoPath string) ([]string, error) {
	// Use git ls-remote to query the remote directly
	// This works after a fresh clone without needing to set up tracking
	// git branch -r only shows tracked branches, which doesn't include pr-* after clone
	// See: https://github.com/erauner/homelab-k8s/issues/1272
	cmd := exec.Command("git", "ls-remote", "--heads", "origin", "refs/heads/pr-*")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list remote branches: %w", err)
	}

	var branches []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Output format: "abc123\trefs/heads/pr-1234"
		parts := strings.Split(line, "\t")
		if len(parts) != 2 {
			continue
		}
		// Extract branch name from refs/heads/pr-1234
		ref := parts[1]
		branch := strings.TrimPrefix(ref, "refs/heads/")
		branches = append(branches, branch)
	}

	return branches, nil
}

// getPRState checks if a PR is open, closed, or merged via GitHub API
func getPRState(repo, prNumber string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s", repo, prNumber)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "shadow-sync")

	// Use GH_TOKEN if available for higher rate limits and private repo access
	// Fix for https://github.com/erauner/homelab-k8s/issues/1272
	if token := os.Getenv("GH_TOKEN"); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		// PR doesn't exist - treat as closed
		return "not_found", nil
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var pr struct {
		State    string `json:"state"`
		MergedAt string `json:"merged_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return "", err
	}

	if pr.MergedAt != "" {
		return "merged", nil
	}
	return pr.State, nil
}

// deleteRemoteBranch deletes a branch from origin
func deleteRemoteBranch(repoPath, branch string) error {
	cmd := exec.Command("git", "push", "origin", "--delete", branch)
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/erauner/homelab-shadow/pkg/sync"
	"github.com/spf13/cobra"
)

var (
	syncShadowRepo    string
	syncBaseBranch    string
	syncBranch        string
	syncCluster       string
	syncOutputFormat  string
	syncForcePush     bool
	syncRedactSecrets bool
	syncCleanupMerged bool
	syncPRNumber      string
	syncSourceCommit  string
	syncSourceRepo    string
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Render kustomizations and push to shadow repo for PR manifest diffs",
	Long: `Sync renders all deployment-relevant kustomizations and pushes the results
to a shadow repository branch. This enables GitHub compare URLs for reviewing
actual manifest changes in pull requests.

The shadow repo stores rendered manifests organized by their source path:
  rendered/apps/giraffe/overlays/production/manifest.yaml
  rendered/infrastructure/envoy-gateway/overlays/erauner-home/manifest.yaml

Security: Secrets are automatically redacted to prevent exposing sensitive data.

Example usage:
  # Basic usage with PR number
  shadow sync --shadow-repo erauner/homelab-k8s-shadow --pr 950

  # Specify branch explicitly
  shadow sync --shadow-repo erauner/homelab-k8s-shadow --branch pr-950

  # Output JSON for CI integration
  shadow sync --shadow-repo erauner/homelab-k8s-shadow --pr 950 --output json

  # Sync specific cluster only
  shadow sync --shadow-repo erauner/homelab-k8s-shadow --cluster erauner-home`,
	RunE: runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)

	syncCmd.Flags().StringVar(&syncShadowRepo, "shadow-repo", "", "Shadow repository (owner/repo or git URL) - required")
	syncCmd.Flags().StringVar(&syncBaseBranch, "base-branch", "main", "Base branch in shadow repo")
	syncCmd.Flags().StringVar(&syncBranch, "branch", "", "Target branch (default: pr-<number> or local-<timestamp>)")
	syncCmd.Flags().StringVar(&syncCluster, "cluster", "", "Specific cluster to sync (default: all)")
	syncCmd.Flags().StringVar(&syncOutputFormat, "output", "text", "Output format: text or json")
	syncCmd.Flags().BoolVar(&syncForcePush, "force", true, "Force push to branch (default: true)")
	syncCmd.Flags().BoolVar(&syncRedactSecrets, "redact-secrets", true, "Redact Secret data (default: true)")
	syncCmd.Flags().BoolVar(&syncCleanupMerged, "cleanup-merged", false, "Delete pr-* branches for closed/merged PRs")
	syncCmd.Flags().StringVar(&syncPRNumber, "pr", "", "PR number (used for branch naming and metadata)")
	syncCmd.Flags().StringVar(&syncSourceCommit, "source-commit", "", "Source commit SHA (for metadata)")
	syncCmd.Flags().StringVar(&syncSourceRepo, "source-repo", "", "Source repository (for metadata)")

	syncCmd.MarkFlagRequired("shadow-repo")
}

func runSync(cmd *cobra.Command, args []string) error {
	// Build clusters list
	var clusters []string
	if syncCluster != "" {
		clusters = []string{syncCluster}
	}

	// Get PR number from environment if not specified
	prNumber := syncPRNumber
	if prNumber == "" {
		prNumber = os.Getenv("CHANGE_ID")
	}

	// Get source commit from environment if not specified
	sourceCommit := syncSourceCommit
	if sourceCommit == "" {
		sourceCommit = os.Getenv("GIT_COMMIT")
	}

	// Get source repo from environment if not specified
	sourceRepo := syncSourceRepo
	if sourceRepo == "" {
		sourceRepo = os.Getenv("GIT_URL")
		// Try to extract slug from URL
		if sourceRepo != "" {
			if slug, err := sync.ParseRepoSlug(sourceRepo); err == nil {
				sourceRepo = slug
			}
		}
	}

	opts := sync.Options{
		RepoPath:      repoDir,
		Clusters:      clusters,
		ShadowRepo:    syncShadowRepo,
		BaseBranch:    syncBaseBranch,
		Branch:        syncBranch,
		ForcePush:     syncForcePush,
		RedactSecrets: syncRedactSecrets,
		CleanupMerged: syncCleanupMerged,
		PRNumber:      prNumber,
		SourceCommit:  sourceCommit,
		SourceRepo:    sourceRepo,
		Verbose:       verbose,
	}

	syncer, err := sync.New(opts)
	if err != nil {
		return fmt.Errorf("failed to initialize syncer: %w", err)
	}

	logInfo("Starting shadow sync...")
	logVerbose("Shadow repo: %s", syncShadowRepo)
	logVerbose("Base branch: %s", syncBaseBranch)
	if syncBranch != "" {
		logVerbose("Target branch: %s", syncBranch)
	}

	result, err := syncer.Run()
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// Output results
	if strings.ToLower(syncOutputFormat) == "json" {
		return outputSyncJSON(result)
	}
	return outputSyncText(result)
}

func outputSyncJSON(result sync.Result) error {
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}
	fmt.Println(string(output))
	return nil
}

func outputSyncText(result sync.Result) error {
	fmt.Fprintf(os.Stderr, "\n=== Shadow Sync Complete ===\n")
	fmt.Fprintf(os.Stderr, "Shadow repo: %s\n", result.ShadowRepoSlug)
	fmt.Fprintf(os.Stderr, "Branch: %s (base: %s)\n", result.Branch, result.BaseBranch)
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "Rendered: %d directories\n", result.RenderedDirs)
	fmt.Fprintf(os.Stderr, "Skipped:  %d directories\n", result.SkippedDirs)
	fmt.Fprintf(os.Stderr, "Failed:   %d directories\n", result.FailedDirs)

	if len(result.Failures) > 0 {
		fmt.Fprintf(os.Stderr, "\nFailures:\n")
		for _, f := range result.Failures {
			fmt.Fprintf(os.Stderr, "  - %s: %s\n", f.Directory, f.Error)
		}
	}

	if result.CommitSHA != "" {
		fmt.Fprintf(os.Stderr, "\nCommit: %s\n", result.CommitSHA)
	}

	fmt.Fprintf(os.Stderr, "\n📋 Compare URL:\n%s\n", result.CompareURL)

	// Show cleanup results if present
	if result.Cleanup != nil {
		fmt.Fprintf(os.Stderr, "\n=== Branch Cleanup ===\n")
		fmt.Fprintf(os.Stderr, "Checked:  %d branches\n", len(result.Cleanup.CheckedBranches))
		fmt.Fprintf(os.Stderr, "Deleted:  %d branches\n", len(result.Cleanup.DeletedBranches))
		fmt.Fprintf(os.Stderr, "Skipped:  %d branches (PRs still open)\n", len(result.Cleanup.SkippedBranches))

		if len(result.Cleanup.DeletedBranches) > 0 {
			fmt.Fprintf(os.Stderr, "\nDeleted branches:\n")
			for _, b := range result.Cleanup.DeletedBranches {
				fmt.Fprintf(os.Stderr, "  - %s\n", b)
			}
		}

		if len(result.Cleanup.Errors) > 0 {
			fmt.Fprintf(os.Stderr, "\nCleanup errors:\n")
			for _, e := range result.Cleanup.Errors {
				fmt.Fprintf(os.Stderr, "  - %s\n", e)
			}
		}
	}

	return nil
}

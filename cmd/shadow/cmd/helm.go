package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/erauner/homelab-shadow/pkg/argocd"
	"github.com/erauner/homelab-shadow/pkg/helm"
	"github.com/erauner/homelab-shadow/pkg/sync"
	"github.com/spf13/cobra"
)

var (
	helmOutputFormat string
	helmRetries      int
	helmRetryDelay   time.Duration
)

var helmCmd = &cobra.Command{
	Use:   "helm",
	Short: "Helm chart discovery and testing commands",
	Long: `Commands for debugging Helm chart discovery and rendering.

This helps diagnose issues with multi-source ArgoCD Applications
that use Helm charts. Use this to:
- List all discovered Helm applications
- Test rendering individual charts
- Verify value file resolution
- Debug OCI registry detection

Examples:
  shadow helm list
  shadow helm list --output json
  shadow helm test
  shadow helm test jenkins
  shadow helm test --retries 3`,
}

var helmListCmd = &cobra.Command{
	Use:   "list",
	Short: "List discovered Helm applications",
	Long: `Lists all ArgoCD Applications that have Helm chart sources.

Shows details about each application including:
- Repository URL
- Chart name and version
- Value files (with resolution status)
- OCI registry detection

Examples:
  shadow helm list
  shadow helm list --output json
  shadow helm list -v`,
	RunE: runHelmList,
}

var helmTestCmd = &cobra.Command{
	Use:   "test [app-name]",
	Short: "Test Helm chart rendering",
	Long: `Test rendering Helm charts from ArgoCD Applications.

Without arguments, tests all discovered Helm applications.
With an app name, tests only that specific application.

The test:
1. Discovers Helm sources from ArgoCD Applications
2. Resolves $values/ file references
3. Runs helm template with value files
4. Reports success/failure with timing

Supports retries for transient network issues.

Examples:
  shadow helm test
  shadow helm test jenkins
  shadow helm test --retries 3 --retry-delay 5s
  shadow helm test envoy-gateway -v`,
	RunE: runHelmTest,
}

func init() {
	rootCmd.AddCommand(helmCmd)
	helmCmd.AddCommand(helmListCmd)
	helmCmd.AddCommand(helmTestCmd)

	helmListCmd.Flags().StringVarP(&helmOutputFormat, "output", "o", "text", "Output format: text, json")
	helmTestCmd.Flags().StringVarP(&helmOutputFormat, "output", "o", "text", "Output format: text, json")
	helmTestCmd.Flags().IntVar(&helmRetries, "retries", 0, "Number of retries for transient failures")
	helmTestCmd.Flags().DurationVar(&helmRetryDelay, "retry-delay", 2*time.Second, "Delay between retries")
}

// HelmAppInfo contains information about a Helm application for listing/testing
type HelmAppInfo struct {
	Name       string           `json:"name"`
	Namespace  string           `json:"namespace"`
	Sources    []HelmSourceInfo `json:"sources"`
	IsOCI      bool             `json:"is_oci,omitempty"`
	OCINormURL string           `json:"oci_normalized_url,omitempty"`
}

// HelmSourceInfo contains information about a Helm source
type HelmSourceInfo struct {
	RepoURL        string   `json:"repo_url"`
	Chart          string   `json:"chart"`
	Version        string   `json:"version"`
	ReleaseName    string   `json:"release_name,omitempty"`
	ValueFiles     []string `json:"value_files,omitempty"`
	ResolvedFiles  []string `json:"resolved_files,omitempty"`
	InlineValues   bool     `json:"has_inline_values,omitempty"`
	IsOCI          bool     `json:"is_oci"`
	ResolutionErrs []string `json:"resolution_errors,omitempty"`
}

// HelmTestResult contains the result of testing a Helm application
type HelmTestResult struct {
	Name     string        `json:"name"`
	Passed   bool          `json:"passed"`
	Duration time.Duration `json:"duration"`
	Bytes    int           `json:"bytes,omitempty"`
	Command  string        `json:"command,omitempty"`
	Error    string        `json:"error,omitempty"`
	Retries  int           `json:"retries,omitempty"`
}

func runHelmList(cmd *cobra.Command, args []string) error {
	// Check if helm is installed
	if !helm.IsHelmInstalled() {
		return fmt.Errorf("helm CLI is not installed")
	}

	helmApps, err := argocd.DiscoverHelmApplications(repoDir)
	if err != nil {
		return fmt.Errorf("failed to discover Helm applications: %w", err)
	}

	logVerbose("Discovered %d Applications with Helm sources", len(helmApps))

	// Build info list
	var appInfos []HelmAppInfo
	for _, app := range helmApps {
		info := HelmAppInfo{
			Name:      app.Name,
			Namespace: app.Namespace,
		}

		for _, source := range app.GetHelmSources() {
			sourceInfo := HelmSourceInfo{
				RepoURL:     source.RepoURL,
				Chart:       source.Chart,
				Version:     source.TargetRevision,
				IsOCI:       sync.IsOCIRegistry(source.RepoURL),
				InlineValues: source.Helm != nil && source.Helm.Values != "",
			}

			if source.Helm != nil {
				sourceInfo.ReleaseName = source.Helm.ReleaseName
				sourceInfo.ValueFiles = source.Helm.ValueFiles

				// Try to resolve value files
				if len(source.Helm.ValueFiles) > 0 {
					resolved, err := argocd.ResolveValueFiles(source.Helm.ValueFiles, repoDir)
					if err != nil {
						sourceInfo.ResolutionErrs = append(sourceInfo.ResolutionErrs, err.Error())
					} else {
						sourceInfo.ResolvedFiles = resolved
					}
				}
			}

			info.Sources = append(info.Sources, sourceInfo)

			// Track OCI at app level
			if sourceInfo.IsOCI {
				info.IsOCI = true
				info.OCINormURL = sync.NormalizeOCIURL(source.RepoURL)
			}
		}

		appInfos = append(appInfos, info)
	}

	// Output
	switch helmOutputFormat {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(appInfos)

	case "text":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "APP\tNAMESPACE\tCHART\tVERSION\tOCI\tVALUES\n")
		fmt.Fprintf(w, "---\t---------\t-----\t-------\t---\t------\n")

		for _, app := range appInfos {
			for _, src := range app.Sources {
				ociMarker := ""
				if src.IsOCI {
					ociMarker = "✓"
				}
				valueCount := len(src.ValueFiles)
				errCount := len(src.ResolutionErrs)
				valueStatus := fmt.Sprintf("%d files", valueCount)
				if errCount > 0 {
					valueStatus = fmt.Sprintf("%d files (%d errors)", valueCount, errCount)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					app.Name, app.Namespace, src.Chart, src.Version, ociMarker, valueStatus)
			}
		}
		w.Flush()

		fmt.Printf("\nTotal: %d Helm applications\n", len(appInfos))
		return nil

	default:
		return fmt.Errorf("unknown output format: %s", helmOutputFormat)
	}
}

func runHelmTest(cmd *cobra.Command, args []string) error {
	// Check if helm is installed
	if !helm.IsHelmInstalled() {
		return fmt.Errorf("helm CLI is not installed")
	}

	version, _ := helm.HelmVersion()
	logInfo("Using: helm %s", version)

	helmApps, err := argocd.DiscoverHelmApplications(repoDir)
	if err != nil {
		return fmt.Errorf("failed to discover Helm applications: %w", err)
	}

	// Filter to specific app if provided
	var targetApp string
	if len(args) > 0 {
		targetApp = args[0]
	}

	var results []HelmTestResult
	var passed, failed int

	for _, app := range helmApps {
		// Filter by name if specified
		if targetApp != "" && app.Name != targetApp {
			continue
		}

		for _, source := range app.GetHelmSources() {
			result := testHelmSource(app, &source)
			results = append(results, result)

			if result.Passed {
				passed++
			} else {
				failed++
			}
		}
	}

	// Check if we found the target app
	if targetApp != "" && len(results) == 0 {
		return fmt.Errorf("application not found: %s", targetApp)
	}

	// Output results
	switch helmOutputFormat {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"passed":  passed,
			"failed":  failed,
			"results": results,
		})

	case "text":
		for _, r := range results {
			if r.Passed {
				fmt.Printf("✓ %s (%d bytes, %s)\n", r.Name, r.Bytes, r.Duration.Round(time.Millisecond))
			} else {
				fmt.Printf("✗ %s (%s)\n", r.Name, r.Duration.Round(time.Millisecond))
				// Show error details
				if verbose {
					fmt.Printf("  Command: %s\n", r.Command)
				}
				// Truncate long errors
				errMsg := r.Error
				if len(errMsg) > 200 && !verbose {
					errMsg = errMsg[:200] + "..."
				}
				fmt.Printf("  Error: %s\n", errMsg)
			}
		}

		fmt.Printf("\n=== Summary ===\n")
		fmt.Printf("Passed: %d\n", passed)
		fmt.Printf("Failed: %d\n", failed)

		if failed > 0 {
			return fmt.Errorf("%d Helm chart(s) failed to render", failed)
		}
		return nil

	default:
		return fmt.Errorf("unknown output format: %s", helmOutputFormat)
	}
}

func testHelmSource(app *argocd.Application, source *argocd.Source) HelmTestResult {
	start := time.Now()
	result := HelmTestResult{
		Name: app.Name,
	}

	// Resolve value files
	var valueFiles []string
	if source.Helm != nil && len(source.Helm.ValueFiles) > 0 {
		resolved, err := argocd.ResolveValueFiles(source.Helm.ValueFiles, repoDir)
		if err != nil {
			result.Duration = time.Since(start)
			result.Error = fmt.Sprintf("failed to resolve value files: %v", err)
			return result
		}
		valueFiles = resolved
	}

	// Get inline values
	var inlineValues string
	if source.Helm != nil && source.Helm.Values != "" {
		inlineValues = source.Helm.Values
	}

	// Get release name
	releaseName := app.Name
	if source.Helm != nil && source.Helm.ReleaseName != "" {
		releaseName = source.Helm.ReleaseName
	}

	// Attempt rendering with retries
	var helmResult helm.TemplateResult
	for attempt := 0; attempt <= helmRetries; attempt++ {
		if attempt > 0 {
			logVerbose("Retry %d/%d for %s after %s", attempt, helmRetries, app.Name, helmRetryDelay)
			time.Sleep(helmRetryDelay)
			result.Retries = attempt
		}

		if sync.IsOCIRegistry(source.RepoURL) {
			ociURL := sync.NormalizeOCIURL(source.RepoURL)
			helmResult = helm.Template(helm.TemplateOptions{
				ReleaseName:  releaseName,
				Namespace:    app.Namespace,
				RepoURL:      "",
				Chart:        ociURL + "/" + source.Chart,
				Version:      source.TargetRevision,
				ValueFiles:   valueFiles,
				InlineValues: inlineValues,
				Verbose:      verbose,
			})
		} else {
			helmResult = helm.Template(helm.TemplateOptions{
				ReleaseName:  releaseName,
				Namespace:    app.Namespace,
				RepoURL:      source.RepoURL,
				Chart:        source.Chart,
				Version:      source.TargetRevision,
				ValueFiles:   valueFiles,
				InlineValues: inlineValues,
				Verbose:      verbose,
			})
		}

		result.Command = helmResult.Command

		if helmResult.Passed {
			break
		}

		// Check if error is retryable (network/timeout issues)
		if !isRetryableError(helmResult.Error) {
			break
		}
	}

	result.Duration = time.Since(start)
	result.Passed = helmResult.Passed

	if helmResult.Passed {
		result.Bytes = len(helmResult.Output)
	} else if helmResult.Error != nil {
		result.Error = helmResult.Error.Error()
	}

	return result
}

// isRetryableError checks if an error is likely transient and worth retrying
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"no such host",
		"temporary failure",
		"network is unreachable",
		"i/o timeout",
		"tls handshake timeout",
		"eof",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

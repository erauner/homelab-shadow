// Package sync provides shadow repository sync functionality for manifest preview diffs
package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/erauner/homelab-shadow/pkg/argocd"
	"github.com/erauner/homelab-shadow/pkg/helm"
	"github.com/erauner/homelab-shadow/pkg/kustomize"
)

// Options configures the sync operation
type Options struct {
	// Input repo (homelab-k8s)
	RepoPath string

	// Clusters to render (empty = all discovered)
	Clusters []string

	// Shadow repo configuration
	ShadowRepo string // GitHub slug (owner/repo) or git URL
	BaseBranch string // Default: "main"
	Branch     string // Default: "pr-<id>" or "local-<timestamp>"
	OutputRoot string // Default: "rendered"

	// Behavior options
	ForcePush     bool // Default: true for PR branches
	RedactSecrets bool // Default: true
	CleanupMerged bool // Delete pr-* branches for closed PRs

	// Source metadata (for commit messages and _meta.json)
	SourceCommit string
	SourceRepo   string
	PRNumber     string

	// Runtime
	Verbose bool
}

// Result contains the outcome of a sync operation
type Result struct {
	ShadowRepoSlug string `json:"shadow_repo"`
	BaseBranch     string `json:"base_branch"`
	Branch         string `json:"branch"`
	CompareURL     string `json:"compare_url"`
	CommitSHA      string `json:"commit_sha,omitempty"`

	RenderedDirs int `json:"rendered_dirs"`
	SkippedDirs  int `json:"skipped_dirs"`
	FailedDirs   int `json:"failed_dirs"`

	// Helm rendering stats (new in #1089)
	HelmAppsRendered int `json:"helm_apps_rendered,omitempty"`
	HelmAppsFailed   int `json:"helm_apps_failed,omitempty"`

	Failures []DirFailure `json:"failures,omitempty"`

	// Cleanup results (populated if cleanup was performed)
	Cleanup *CleanupResult `json:"cleanup,omitempty"`
}

// DirFailure represents a failed directory render
type DirFailure struct {
	Directory string `json:"directory"`
	Error     string `json:"error"`
}

// Metadata stored in _meta.json in shadow repo
type Metadata struct {
	SourceRepo  string   `json:"source_repo"`
	SourceSHA   string   `json:"source_commit"`
	PRNumber    string   `json:"pr,omitempty"`
	Clusters    []string `json:"clusters"`
	GeneratedAt string   `json:"generated_at"`
}

// Syncer manages the shadow repo sync process
type Syncer struct {
	opts Options
}

// New creates a new Syncer with the given options
func New(opts Options) (*Syncer, error) {
	// Set defaults
	if opts.BaseBranch == "" {
		opts.BaseBranch = "main"
	}
	if opts.OutputRoot == "" {
		opts.OutputRoot = "rendered"
	}
	if opts.Branch == "" {
		if opts.PRNumber != "" {
			opts.Branch = fmt.Sprintf("pr-%s", opts.PRNumber)
		} else {
			opts.Branch = fmt.Sprintf("local-%d", time.Now().Unix())
		}
	}

	// Validate required fields
	if opts.RepoPath == "" {
		return nil, fmt.Errorf("RepoPath is required")
	}
	if opts.ShadowRepo == "" {
		return nil, fmt.Errorf("ShadowRepo is required")
	}

	return &Syncer{opts: opts}, nil
}

// Run executes the sync operation
func (s *Syncer) Run() (Result, error) {
	result := Result{
		ShadowRepoSlug: s.opts.ShadowRepo,
		BaseBranch:     s.opts.BaseBranch,
		Branch:         s.opts.Branch,
	}

	// 1. Discover directories to render
	dirs, err := s.discoverDirectories()
	if err != nil {
		return result, fmt.Errorf("failed to discover directories: %w", err)
	}

	s.logVerbose("Discovered %d directories to render", len(dirs))

	// 2. Clone shadow repo to temp directory
	tempDir, err := os.MkdirTemp("", "shadow-sync-*")
	if err != nil {
		return result, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	shadowDir := filepath.Join(tempDir, "shadow")
	repoURL := GitURLFromSlug(s.opts.ShadowRepo)

	s.logVerbose("Cloning shadow repo %s to %s", repoURL, shadowDir)
	if err := Clone(repoURL, shadowDir); err != nil {
		return result, fmt.Errorf("failed to clone shadow repo: %w", err)
	}

	// 3. Checkout branch (create from base if new)
	s.logVerbose("Checking out branch %s (base: %s)", s.opts.Branch, s.opts.BaseBranch)
	if err := CheckoutBranch(shadowDir, s.opts.BaseBranch, s.opts.Branch); err != nil {
		return result, fmt.Errorf("failed to checkout branch: %w", err)
	}

	// 4. Clear and recreate output directory
	outputDir := filepath.Join(shadowDir, s.opts.OutputRoot)
	if err := os.RemoveAll(outputDir); err != nil {
		return result, fmt.Errorf("failed to clear output directory: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return result, fmt.Errorf("failed to create output directory: %w", err)
	}

	// 5. Build and write manifests for each directory
	runner := kustomize.NewRunner(s.opts.RepoPath, "", s.opts.Verbose)

	for _, dir := range dirs {
		s.logVerbose("Building %s", dir)

		buildResult := runner.BuildDirectory(dir)

		if buildResult.Skipped {
			result.SkippedDirs++
			continue
		}

		if !buildResult.Passed {
			result.FailedDirs++
			result.Failures = append(result.Failures, DirFailure{
				Directory: dir,
				Error:     buildResult.Error.Error(),
			})
			continue
		}

		// Redact secrets if enabled
		manifest := buildResult.Output
		if s.opts.RedactSecrets {
			manifest = RedactSecrets(manifest)
		}

		// Write manifest to shadow repo
		manifestPath := filepath.Join(outputDir, dir, "manifest.yaml")
		if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
			result.FailedDirs++
			result.Failures = append(result.Failures, DirFailure{
				Directory: dir,
				Error:     fmt.Sprintf("failed to create directory: %v", err),
			})
			continue
		}

		if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
			result.FailedDirs++
			result.Failures = append(result.Failures, DirFailure{
				Directory: dir,
				Error:     fmt.Sprintf("failed to write manifest: %v", err),
			})
			continue
		}

		result.RenderedDirs++
	}

	// 5b. Render Helm charts from multi-source Applications (issue #1089)
	if helm.IsHelmInstalled() {
		helmApps, err := argocd.DiscoverHelmApplications(s.opts.RepoPath)
		if err != nil {
			s.logVerbose("Warning: failed to discover Helm applications: %v", err)
		} else {
			s.logVerbose("Discovered %d Applications with Helm sources", len(helmApps))

			for _, app := range helmApps {
				for _, source := range app.GetHelmSources() {
					s.logVerbose("Rendering Helm chart for %s: %s/%s@%s",
						app.Name, source.RepoURL, source.Chart, source.TargetRevision)

					helmResult := s.renderHelmSource(app, &source)

					if !helmResult.Passed {
						result.HelmAppsFailed++
						result.Failures = append(result.Failures, DirFailure{
							Directory: fmt.Sprintf("apps/%s/helm", app.Name),
							Error:     helmResult.Error.Error(),
						})
						continue
					}

					// Redact secrets if enabled
					manifest := helmResult.Output
					if s.opts.RedactSecrets {
						manifest = RedactSecrets(manifest)
					}

					// Write Helm manifest to shadow repo
					// Structure: apps/<appname>/helm/manifest.yaml
					manifestPath := filepath.Join(outputDir, "apps", app.Name, "helm", "manifest.yaml")
					if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
						result.HelmAppsFailed++
						result.Failures = append(result.Failures, DirFailure{
							Directory: fmt.Sprintf("apps/%s/helm", app.Name),
							Error:     fmt.Sprintf("failed to create directory: %v", err),
						})
						continue
					}

					if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
						result.HelmAppsFailed++
						result.Failures = append(result.Failures, DirFailure{
							Directory: fmt.Sprintf("apps/%s/helm", app.Name),
							Error:     fmt.Sprintf("failed to write manifest: %v", err),
						})
						continue
					}

					result.HelmAppsRendered++
				}
			}
		}
	} else {
		s.logVerbose("Helm not installed, skipping Helm chart rendering")
	}

	// 6. Write metadata file
	meta := Metadata{
		SourceRepo:  s.opts.SourceRepo,
		SourceSHA:   s.opts.SourceCommit,
		PRNumber:    s.opts.PRNumber,
		Clusters:    s.opts.Clusters,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	metaPath := filepath.Join(outputDir, "_meta.json")
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return result, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, metaJSON, 0644); err != nil {
		return result, fmt.Errorf("failed to write metadata: %w", err)
	}

	// 7. Commit changes
	commitMsg := s.buildCommitMessage()
	changed, sha, err := CommitAll(shadowDir, commitMsg)
	if err != nil {
		return result, fmt.Errorf("failed to commit changes: %w", err)
	}

	if !changed {
		s.logVerbose("No changes to commit")
	} else {
		result.CommitSHA = sha
		s.logVerbose("Committed changes: %s", sha)
	}

	// 8. Push to remote
	s.logVerbose("Pushing to origin/%s (force=%v)", s.opts.Branch, s.opts.ForcePush)
	if err := Push(shadowDir, "origin", s.opts.Branch, s.opts.ForcePush); err != nil {
		return result, fmt.Errorf("failed to push: %w", err)
	}

	// 9. Generate compare URL
	result.CompareURL = CompareURL(s.opts.ShadowRepo, s.opts.BaseBranch, s.opts.Branch)

	// 10. Cleanup merged PR branches if requested
	if s.opts.CleanupMerged && s.opts.SourceRepo != "" {
		s.logVerbose("Running cleanup for merged PR branches...")
		cleanupResult, err := CleanupStaleBranches(shadowDir, s.opts.SourceRepo, false, s.opts.Verbose)
		if err != nil {
			// Log but don't fail the sync for cleanup errors
			s.logVerbose("Warning: cleanup failed: %v", err)
		} else {
			result.Cleanup = &cleanupResult
			if len(cleanupResult.DeletedBranches) > 0 {
				s.logVerbose("Deleted %d stale branches", len(cleanupResult.DeletedBranches))
			}
		}
	}

	return result, nil
}

// discoverDirectories finds kustomization directories to render
func (s *Syncer) discoverDirectories() ([]string, error) {
	return DiscoverKustomizationsForSync(s.opts.RepoPath, s.opts.Clusters)
}

// buildCommitMessage creates the commit message with source metadata
func (s *Syncer) buildCommitMessage() string {
	msg := "shadow sync"

	if s.opts.SourceRepo != "" && s.opts.SourceCommit != "" {
		shortSha := s.opts.SourceCommit
		if len(shortSha) > 7 {
			shortSha = shortSha[:7]
		}
		msg += fmt.Sprintf(": %s@%s", s.opts.SourceRepo, shortSha)
	}

	if s.opts.PRNumber != "" {
		msg += fmt.Sprintf(" PR #%s", s.opts.PRNumber)
	}

	return msg
}

func (s *Syncer) logVerbose(format string, args ...interface{}) {
	if s.opts.Verbose {
		fmt.Fprintf(os.Stderr, "[sync] "+format+"\n", args...)
	}
}

// renderHelmSource renders a Helm chart source from an ArgoCD Application
func (s *Syncer) renderHelmSource(app *argocd.Application, source *argocd.Source) helm.TemplateResult {
	// Resolve value files from $values/ references
	var valueFiles []string
	if source.Helm != nil && len(source.Helm.ValueFiles) > 0 {
		resolved, err := argocd.ResolveValueFiles(source.Helm.ValueFiles, s.opts.RepoPath)
		if err != nil {
			return helm.TemplateResult{
				Passed: false,
				Error:  fmt.Errorf("failed to resolve value files: %w", err),
			}
		}
		valueFiles = resolved
	}

	// Get inline values if present
	var inlineValues string
	if source.Helm != nil && source.Helm.Values != "" {
		inlineValues = source.Helm.Values
	}

	// Get release name
	releaseName := app.Name
	if source.Helm != nil && source.Helm.ReleaseName != "" {
		releaseName = source.Helm.ReleaseName
	}

	// Normalize repo URL for helm template --repo flag
	// Some URLs may need adjustment (e.g., OCI registries)
	repoURL := source.RepoURL

	// Check if this is an OCI registry URL (explicit or implicit)
	if IsOCIRegistry(repoURL) {
		// Normalize to oci:// format for helm template
		ociURL := NormalizeOCIURL(repoURL)
		// For OCI registries, we need to use the full chart reference
		// helm template RELEASE oci://registry/chart --version VERSION
		return helm.Template(helm.TemplateOptions{
			ReleaseName:  releaseName,
			Namespace:    app.Namespace,
			RepoURL:      "", // OCI doesn't use --repo
			Chart:        ociURL + "/" + source.Chart,
			Version:      source.TargetRevision,
			ValueFiles:   valueFiles,
			InlineValues: inlineValues,
			Verbose:      s.opts.Verbose,
		})
	}

	return helm.Template(helm.TemplateOptions{
		ReleaseName:  releaseName,
		Namespace:    app.Namespace,
		RepoURL:      repoURL,
		Chart:        source.Chart,
		Version:      source.TargetRevision,
		ValueFiles:   valueFiles,
		InlineValues: inlineValues,
		Verbose:      s.opts.Verbose,
	})
}

// ociRegistryPrefixes lists common OCI registry hostnames that ArgoCD may use
// without the oci:// prefix. These need to be detected and normalized.
var ociRegistryPrefixes = []string{
	"docker.io/",
	"ghcr.io/",
	"quay.io/",
	"registry.k8s.io/",
	"gcr.io/",
	"public.ecr.aws/",
	"mcr.microsoft.com/",
}

// IsOCIRegistry checks if the URL refers to an OCI registry
// This handles both explicit oci:// URLs and implicit registry hostnames
func IsOCIRegistry(url string) bool {
	// Explicit OCI protocol
	if strings.HasPrefix(url, "oci://") {
		return true
	}

	// Check for known OCI registry hostnames
	for _, prefix := range ociRegistryPrefixes {
		if strings.HasPrefix(url, prefix) {
			return true
		}
	}

	return false
}

// NormalizeOCIURL converts an OCI registry URL to the oci:// format expected by helm
// Examples:
//   - "oci://docker.io/envoyproxy" -> "oci://docker.io/envoyproxy" (unchanged)
//   - "docker.io/envoyproxy" -> "oci://docker.io/envoyproxy"
func NormalizeOCIURL(url string) string {
	// Already has oci:// prefix
	if strings.HasPrefix(url, "oci://") {
		return url
	}

	// Add oci:// prefix for known registries
	return "oci://" + url
}

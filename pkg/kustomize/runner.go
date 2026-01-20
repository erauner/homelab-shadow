// Package kustomize provides kustomize build and kubeconform validation
package kustomize

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// BuildResult represents the result of building a single kustomization directory
// This is the output of kustomize build without schema validation
type BuildResult struct {
	Directory  string
	Output     string
	Passed     bool
	Error      error
	Skipped    bool
	SkipReason string
}

// ValidationResult represents the result of validating a single kustomization directory
type ValidationResult struct {
	Directory    string
	BuildPassed  bool
	BuildOutput  string
	BuildError   error
	SchemaPassed bool
	SchemaOutput string
	SchemaError  error
	Skipped      bool
	SkipReason   string
}

// Passed returns true if both build and schema validation passed
func (r *ValidationResult) Passed() bool {
	return r.Skipped || (r.BuildPassed && r.SchemaPassed)
}

// Runner runs kustomize build and kubeconform validation
type Runner struct {
	RepoPath          string
	KubernetesVersion string
	Verbose           bool
}

// NewRunner creates a new kustomize validation runner
func NewRunner(repoPath string, kubernetesVersion string, verbose bool) *Runner {
	if kubernetesVersion == "" {
		kubernetesVersion = "1.31.0" // Default version
	}
	return &Runner{
		RepoPath:          repoPath,
		KubernetesVersion: kubernetesVersion,
		Verbose:           verbose,
	}
}

// DiscoverDirectories finds all kustomization directories to validate
// Patterns match the Jenkinsfile discovery logic
// Note: After #1256 migration, overlays/stacks are now 2 levels deep:
// apps/*/stack/erauner-home/production, apps/*/overlays/erauner-home/production
func (r *Runner) DiscoverDirectories() ([]string, error) {
	patterns := []string{
		// App base directories
		"apps/*/base",
		// App overlays - both old (apps/*/overlays/*) and new cluster-aware patterns
		"apps/*/overlays/*",
		"apps/*/overlays/*/*",
		// App stack directories - cluster-aware (apps/*/stack/erauner-home/production)
		"apps/*/stack/*",
		"apps/*/stack/*/*",
		// App database directories
		"apps/*/db/base",
		"apps/*/db/overlays/*",
		"apps/*/db/overlays/*/*",
		// Infrastructure
		"infrastructure/base/*",
		"infrastructure/*/base",
		"infrastructure/*/overlays/*",
		"infrastructure/*/overlays/*/*",
		// Operators
		"operators/*/base",
		"operators/*/overlays/*",
		"operators/*/overlays/*/*",
		// Security
		"security/*/base",
		"security/*/overlays/*",
		"security/*/overlays/*/*",
	}

	dirSet := make(map[string]bool)

	for _, pattern := range patterns {
		fullPattern := filepath.Join(r.RepoPath, pattern, "kustomization.yaml")
		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			return nil, fmt.Errorf("glob error for pattern %s: %w", pattern, err)
		}

		for _, match := range matches {
			dir := filepath.Dir(match)
			// Convert to relative path for cleaner output
			relDir, err := filepath.Rel(r.RepoPath, dir)
			if err != nil {
				relDir = dir
			}
			dirSet[relDir] = true
		}
	}

	// Convert to sorted slice
	dirs := make([]string, 0, len(dirSet))
	for dir := range dirSet {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	return dirs, nil
}

// BuildDirectory builds a single kustomization directory without schema validation
// This is useful for rendering manifests for preview diffs
func (r *Runner) BuildDirectory(dir string) BuildResult {
	result := BuildResult{
		Directory: dir,
	}

	absDir := filepath.Join(r.RepoPath, dir)

	// Check if directory exists
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		result.Skipped = true
		result.SkipReason = "directory not found"
		return result
	}

	// Check if kustomization.yaml exists
	kustomizationFile := filepath.Join(absDir, "kustomization.yaml")
	if _, err := os.Stat(kustomizationFile); os.IsNotExist(err) {
		result.Skipped = true
		result.SkipReason = "no kustomization.yaml"
		return result
	}

	// Run kustomize build
	// Flags match ArgoCD's kustomize.buildOptions
	buildCmd := exec.Command("kustomize", "build",
		"--load-restrictor=LoadRestrictionsNone",
		"--enable-helm",
		"--enable-alpha-plugins",
		"--enable-exec",
		absDir)

	buildOutput, err := buildCmd.CombinedOutput()
	result.Output = string(buildOutput)

	if err != nil {
		result.Passed = false
		result.Error = fmt.Errorf("kustomize build failed: %w", err)
		return result
	}
	result.Passed = true

	return result
}

// ValidateDirectory validates a single kustomization directory
// This builds the kustomization and validates it with kubeconform
func (r *Runner) ValidateDirectory(dir string) ValidationResult {
	result := ValidationResult{
		Directory: dir,
	}

	// First, build the kustomization
	buildResult := r.BuildDirectory(dir)

	// Copy build results
	result.BuildOutput = buildResult.Output
	result.BuildPassed = buildResult.Passed
	result.BuildError = buildResult.Error
	result.Skipped = buildResult.Skipped
	result.SkipReason = buildResult.SkipReason

	// If build was skipped or failed, return early
	if buildResult.Skipped || !buildResult.Passed {
		return result
	}

	// Write manifests to temp file for kubeconform
	tmpFile, err := os.CreateTemp("", "manifests-*.yaml")
	if err != nil {
		result.SchemaPassed = false
		result.SchemaError = fmt.Errorf("failed to create temp file: %w", err)
		return result
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(buildResult.Output); err != nil {
		result.SchemaPassed = false
		result.SchemaError = fmt.Errorf("failed to write temp file: %w", err)
		return result
	}
	tmpFile.Close()

	// Run kubeconform validation
	validateCmd := exec.Command("kubeconform",
		"-strict",
		"-ignore-missing-schemas",
		"-kubernetes-version", r.KubernetesVersion,
		"-summary",
		tmpFile.Name())

	validateOutput, err := validateCmd.CombinedOutput()
	result.SchemaOutput = string(validateOutput)

	if err != nil {
		result.SchemaPassed = false
		result.SchemaError = fmt.Errorf("kubeconform validation failed: %w", err)
		return result
	}
	result.SchemaPassed = true

	return result
}

// ValidateAll validates all discovered kustomization directories
func (r *Runner) ValidateAll() ([]ValidationResult, error) {
	dirs, err := r.DiscoverDirectories()
	if err != nil {
		return nil, err
	}

	results := make([]ValidationResult, 0, len(dirs))
	for _, dir := range dirs {
		result := r.ValidateDirectory(dir)
		results = append(results, result)
	}

	return results, nil
}

// Summary returns a summary of validation results
type Summary struct {
	Total       int
	Passed      int
	BuildFailed int
	SchemaFailed int
	Skipped     int
}

// Summarize creates a summary from validation results
func Summarize(results []ValidationResult) Summary {
	s := Summary{Total: len(results)}

	for _, r := range results {
		if r.Skipped {
			s.Skipped++
		} else if !r.BuildPassed {
			s.BuildFailed++
		} else if !r.SchemaPassed {
			s.SchemaFailed++
		} else {
			s.Passed++
		}
	}

	return s
}

// FailedResults returns only the failed validation results
func FailedResults(results []ValidationResult) []ValidationResult {
	var failed []ValidationResult
	for _, r := range results {
		if !r.Passed() {
			failed = append(failed, r)
		}
	}
	return failed
}

// IsKustomizeInstalled checks if kustomize CLI is available
func IsKustomizeInstalled() bool {
	_, err := exec.LookPath("kustomize")
	return err == nil
}

// IsKubeconformInstalled checks if kubeconform CLI is available
func IsKubeconformInstalled() bool {
	_, err := exec.LookPath("kubeconform")
	return err == nil
}

// IsHelmInstalled checks if helm CLI is available
// Required for kustomize --enable-helm flag
func IsHelmInstalled() bool {
	_, err := exec.LookPath("helm")
	return err == nil
}

// KustomizeVersion returns the installed kustomize version
func KustomizeVersion() (string, error) {
	cmd := exec.Command("kustomize", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get kustomize version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// KubeconformVersion returns the installed kubeconform version
func KubeconformVersion() (string, error) {
	cmd := exec.Command("kubeconform", "-v")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get kubeconform version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

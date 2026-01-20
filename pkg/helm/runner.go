// Package helm provides Helm template rendering for shadow sync
package helm

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// TemplateOptions configures a helm template operation
type TemplateOptions struct {
	// ReleaseName is the Helm release name (defaults to chart name)
	ReleaseName string

	// Namespace is the target namespace
	Namespace string

	// RepoURL is the Helm repository URL
	RepoURL string

	// Chart is the chart name
	Chart string

	// Version is the chart version
	Version string

	// ValueFiles are paths to value files
	ValueFiles []string

	// InlineValues is inline YAML values
	InlineValues string

	// Verbose enables verbose output
	Verbose bool
}

// TemplateResult contains the result of helm template
type TemplateResult struct {
	Output  string
	Passed  bool
	Error   error
	Command string // The command that was run (for debugging)
}

// Template runs helm template with the given options
func Template(opts TemplateOptions) TemplateResult {
	result := TemplateResult{}

	// Build command arguments
	args := []string{"template"}

	// Release name (required)
	releaseName := opts.ReleaseName
	if releaseName == "" {
		releaseName = opts.Chart
	}
	args = append(args, releaseName)

	// Chart reference (chart name when using --repo)
	args = append(args, opts.Chart)

	// Repository URL
	if opts.RepoURL != "" {
		args = append(args, "--repo", opts.RepoURL)
	}

	// Version
	if opts.Version != "" {
		args = append(args, "--version", opts.Version)
	}

	// Namespace
	if opts.Namespace != "" {
		args = append(args, "--namespace", opts.Namespace)
	}

	// Value files
	for _, vf := range opts.ValueFiles {
		args = append(args, "--values", vf)
	}

	// Inline values (write to temp file)
	if opts.InlineValues != "" {
		tmpFile, err := os.CreateTemp("", "helm-values-*.yaml")
		if err != nil {
			result.Error = fmt.Errorf("failed to create temp file for inline values: %w", err)
			return result
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(opts.InlineValues); err != nil {
			result.Error = fmt.Errorf("failed to write inline values: %w", err)
			return result
		}
		tmpFile.Close()

		args = append(args, "--values", tmpFile.Name())
	}

	// Include CRDs in output
	args = append(args, "--include-crds")

	// Build command string for debugging
	result.Command = "helm " + strings.Join(args, " ")

	// Execute helm template
	cmd := exec.Command("helm", args...)
	output, err := cmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		result.Passed = false
		result.Error = fmt.Errorf("helm template failed: %w\nOutput: %s", err, string(output))
		return result
	}

	result.Passed = true
	return result
}

// IsHelmInstalled checks if helm CLI is available
func IsHelmInstalled() bool {
	_, err := exec.LookPath("helm")
	return err == nil
}

// HelmVersion returns the installed helm version
func HelmVersion() (string, error) {
	cmd := exec.Command("helm", "version", "--short")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get helm version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// UpdateRepo ensures a helm repo is added and updated
// This is needed for charts from custom repositories
func UpdateRepo(name, url string) error {
	// Add repo (will update if already exists)
	addCmd := exec.Command("helm", "repo", "add", name, url, "--force-update")
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add helm repo %s: %w\nOutput: %s", name, err, string(output))
	}

	// Update repo
	updateCmd := exec.Command("helm", "repo", "update", name)
	if output, err := updateCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to update helm repo %s: %w\nOutput: %s", name, err, string(output))
	}

	return nil
}

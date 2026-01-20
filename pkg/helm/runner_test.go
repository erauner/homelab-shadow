package helm

import (
	"os"
	"strings"
	"testing"
)

func TestIsHelmInstalled(t *testing.T) {
	// This test just verifies the function runs without error
	// The result depends on whether helm is installed on the system
	result := IsHelmInstalled()
	t.Logf("Helm installed: %v", result)
}

func TestHelmVersion(t *testing.T) {
	if !IsHelmInstalled() {
		t.Skip("Helm not installed, skipping version test")
	}

	version, err := HelmVersion()
	if err != nil {
		t.Fatalf("HelmVersion failed: %v", err)
	}

	// Should start with "v" (e.g., "v3.14.0")
	if !strings.HasPrefix(version, "v") {
		t.Errorf("expected version to start with 'v', got %q", version)
	}
	t.Logf("Helm version: %s", version)
}

func TestTemplate_BasicChart(t *testing.T) {
	if !IsHelmInstalled() {
		t.Skip("Helm not installed, skipping template test")
	}

	// Create a temp values file
	tmpFile, err := os.CreateTemp("", "helm-test-values-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write minimal values for app-template
	values := `
controllers:
  main:
    containers:
      main:
        image:
          repository: nginx
          tag: latest
`
	if _, err := tmpFile.WriteString(values); err != nil {
		t.Fatalf("failed to write values: %v", err)
	}
	tmpFile.Close()

	result := Template(TemplateOptions{
		ReleaseName: "test-release",
		Namespace:   "default",
		RepoURL:     "https://bjw-s-labs.github.io/helm-charts",
		Chart:       "app-template",
		Version:     "4.5.0",
		ValueFiles:  []string{tmpFile.Name()},
	})

	if !result.Passed {
		t.Fatalf("Template failed: %v\nCommand: %s\nOutput: %s", result.Error, result.Command, result.Output)
	}

	// Output should contain Kubernetes manifests
	if !strings.Contains(result.Output, "apiVersion:") {
		t.Error("expected output to contain 'apiVersion:'")
	}
	if !strings.Contains(result.Output, "kind:") {
		t.Error("expected output to contain 'kind:'")
	}

	t.Logf("Generated %d bytes of manifests", len(result.Output))
}

func TestTemplate_InlineValues(t *testing.T) {
	if !IsHelmInstalled() {
		t.Skip("Helm not installed, skipping template test")
	}

	inlineValues := `
controllers:
  main:
    containers:
      main:
        image:
          repository: nginx
          tag: "1.25"
`

	result := Template(TemplateOptions{
		ReleaseName:  "inline-test",
		Namespace:    "test-ns",
		RepoURL:      "https://bjw-s-labs.github.io/helm-charts",
		Chart:        "app-template",
		Version:      "4.5.0",
		InlineValues: inlineValues,
	})

	if !result.Passed {
		t.Fatalf("Template with inline values failed: %v\nOutput: %s", result.Error, result.Output)
	}

	// Check namespace is set correctly
	if !strings.Contains(result.Output, "namespace: test-ns") {
		t.Error("expected output to contain 'namespace: test-ns'")
	}
}

func TestTemplate_InvalidChart(t *testing.T) {
	if !IsHelmInstalled() {
		t.Skip("Helm not installed, skipping template test")
	}

	result := Template(TemplateOptions{
		ReleaseName: "test",
		Namespace:   "default",
		RepoURL:     "https://example.com/nonexistent",
		Chart:       "nonexistent-chart",
		Version:     "1.0.0",
	})

	if result.Passed {
		t.Error("expected Template to fail for non-existent chart")
	}

	if result.Error == nil {
		t.Error("expected error to be set")
	}
}

func TestTemplateOptions_Defaults(t *testing.T) {
	// Verify that Template handles missing ReleaseName by using Chart
	if !IsHelmInstalled() {
		t.Skip("Helm not installed, skipping template test")
	}

	// This should work even with empty ReleaseName (defaults to chart name)
	result := Template(TemplateOptions{
		Namespace:    "default",
		RepoURL:      "https://bjw-s-labs.github.io/helm-charts",
		Chart:        "app-template",
		Version:      "4.5.0",
		InlineValues: "controllers: {}",
	})

	// The command should include the chart name as release name
	if !strings.Contains(result.Command, "app-template") {
		t.Errorf("expected command to include chart name, got: %s", result.Command)
	}
}

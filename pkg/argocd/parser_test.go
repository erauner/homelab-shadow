package argocd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseApplicationYAML_MultiSourceWithHelm(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: krr
  namespace: argocd
spec:
  destination:
    namespace: krr
  sources:
    - repoURL: https://bjw-s-labs.github.io/helm-charts
      chart: app-template
      targetRevision: "4.5.0"
      helm:
        releaseName: krr
        valueFiles:
          - $values/apps/krr/base/values.yaml
    - repoURL: git@github.com:erauner/homelab-k8s.git
      targetRevision: master
      ref: values
    - repoURL: git@github.com:erauner/homelab-k8s.git
      targetRevision: master
      path: apps/krr/overlays/erauner-home/production
`

	app, err := ParseApplicationYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseApplicationYAML failed: %v", err)
	}

	if app.Name != "krr" {
		t.Errorf("expected name 'krr', got %q", app.Name)
	}

	if app.Namespace != "krr" {
		t.Errorf("expected namespace 'krr', got %q", app.Namespace)
	}

	if len(app.Sources) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(app.Sources))
	}

	// Check Helm source
	helmSources := app.GetHelmSources()
	if len(helmSources) != 1 {
		t.Fatalf("expected 1 Helm source, got %d", len(helmSources))
	}

	helmSource := helmSources[0]
	if helmSource.Chart != "app-template" {
		t.Errorf("expected chart 'app-template', got %q", helmSource.Chart)
	}
	if helmSource.TargetRevision != "4.5.0" {
		t.Errorf("expected version '4.5.0', got %q", helmSource.TargetRevision)
	}
	if helmSource.Helm == nil {
		t.Fatal("expected helm config to be set")
	}
	if helmSource.Helm.ReleaseName != "krr" {
		t.Errorf("expected release name 'krr', got %q", helmSource.Helm.ReleaseName)
	}
	if len(helmSource.Helm.ValueFiles) != 1 {
		t.Fatalf("expected 1 value file, got %d", len(helmSource.Helm.ValueFiles))
	}
	if helmSource.Helm.ValueFiles[0] != "$values/apps/krr/base/values.yaml" {
		t.Errorf("expected value file '$values/apps/krr/base/values.yaml', got %q", helmSource.Helm.ValueFiles[0])
	}

	// Check Kustomize source
	kustomizeSources := app.GetKustomizeSources()
	if len(kustomizeSources) != 1 {
		t.Fatalf("expected 1 Kustomize source, got %d", len(kustomizeSources))
	}

	kustomizeSource := kustomizeSources[0]
	if kustomizeSource.Path != "apps/krr/overlays/erauner-home/production" {
		t.Errorf("expected path 'apps/krr/overlays/erauner-home/production', got %q", kustomizeSource.Path)
	}
}

func TestParseApplicationYAML_InlineHelmValues(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: homepage
  namespace: argocd
spec:
  destination:
    namespace: homepage
  sources:
    - repoURL: https://bjw-s-labs.github.io/helm-charts
      targetRevision: "4.5.0"
      chart: app-template
      helm:
        values: |
          controllers:
            main:
              replicas: 1
    - repoURL: git@github.com:erauner/homelab-k8s.git
      targetRevision: HEAD
      path: apps/homepage/overlays/erauner-home/production
`

	app, err := ParseApplicationYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseApplicationYAML failed: %v", err)
	}

	helmSources := app.GetHelmSources()
	if len(helmSources) != 1 {
		t.Fatalf("expected 1 Helm source, got %d", len(helmSources))
	}

	helmSource := helmSources[0]
	if helmSource.Helm == nil {
		t.Fatal("expected helm config to be set")
	}
	if helmSource.Helm.Values == "" {
		t.Error("expected inline values to be set")
	}
	if len(helmSource.Helm.ValueFiles) != 0 {
		t.Errorf("expected no value files, got %d", len(helmSource.Helm.ValueFiles))
	}
}

func TestParseApplicationYAML_SingleSource(t *testing.T) {
	yaml := `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: coder
  namespace: argocd
spec:
  destination:
    namespace: coder
  source:
    repoURL: git@github.com:erauner/homelab-k8s.git
    targetRevision: master
    path: apps/coder/stack/erauner-home/production
`

	app, err := ParseApplicationYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseApplicationYAML failed: %v", err)
	}

	if app.Name != "coder" {
		t.Errorf("expected name 'coder', got %q", app.Name)
	}

	// Single source apps should have 0 helm sources
	helmSources := app.GetHelmSources()
	if len(helmSources) != 0 {
		t.Errorf("expected 0 Helm sources, got %d", len(helmSources))
	}

	// Should have 1 kustomize source (from Source, not Sources)
	kustomizeSources := app.GetKustomizeSources()
	if len(kustomizeSources) != 1 {
		t.Fatalf("expected 1 Kustomize source, got %d", len(kustomizeSources))
	}
}

func TestParseApplicationYAML_NotApplication(t *testing.T) {
	yaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test
`

	_, err := ParseApplicationYAML([]byte(yaml))
	if err == nil {
		t.Error("expected error for non-Application kind")
	}
}

func TestResolveValueFiles(t *testing.T) {
	// Create temp directory with test files
	tmpDir, err := os.MkdirTemp("", "argocd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test value file
	valuesDir := filepath.Join(tmpDir, "apps", "krr", "base")
	if err := os.MkdirAll(valuesDir, 0755); err != nil {
		t.Fatalf("failed to create values dir: %v", err)
	}
	valuesFile := filepath.Join(valuesDir, "values.yaml")
	if err := os.WriteFile(valuesFile, []byte("test: value"), 0644); err != nil {
		t.Fatalf("failed to write values file: %v", err)
	}

	// Test resolving $values/ path
	valueFiles := []string{"$values/apps/krr/base/values.yaml"}
	resolved, err := ResolveValueFiles(valueFiles, tmpDir)
	if err != nil {
		t.Fatalf("ResolveValueFiles failed: %v", err)
	}

	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved file, got %d", len(resolved))
	}

	expectedPath := filepath.Join(tmpDir, "apps", "krr", "base", "values.yaml")
	if resolved[0] != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, resolved[0])
	}
}

func TestResolveValueFiles_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "argocd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	valueFiles := []string{"$values/nonexistent/values.yaml"}
	_, err = ResolveValueFiles(valueFiles, tmpDir)
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestSourceHelpers(t *testing.T) {
	tests := []struct {
		name          string
		source        Source
		isHelm        bool
		isKustomize   bool
		isRef         bool
	}{
		{
			name: "helm source",
			source: Source{
				RepoURL: "https://charts.example.com",
				Chart:   "app-template",
			},
			isHelm:      true,
			isKustomize: false,
			isRef:       false,
		},
		{
			name: "kustomize source",
			source: Source{
				RepoURL: "git@github.com:example/repo.git",
				Path:    "apps/test/overlays/production",
			},
			isHelm:      false,
			isKustomize: true,
			isRef:       false,
		},
		{
			name: "ref source",
			source: Source{
				RepoURL: "git@github.com:example/repo.git",
				Ref:     "values",
			},
			isHelm:      false,
			isKustomize: false,
			isRef:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.IsHelmSource(); got != tt.isHelm {
				t.Errorf("IsHelmSource() = %v, want %v", got, tt.isHelm)
			}
			if got := tt.source.IsKustomizeSource(); got != tt.isKustomize {
				t.Errorf("IsKustomizeSource() = %v, want %v", got, tt.isKustomize)
			}
			if got := tt.source.IsRefSource(); got != tt.isRef {
				t.Errorf("IsRefSource() = %v, want %v", got, tt.isRef)
			}
		})
	}
}

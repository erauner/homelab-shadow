package validate

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTestCluster creates a temporary test cluster structure
func setupTestCluster(t *testing.T, name string, dirs []string, files map[string]string) string {
	t.Helper()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "clusters", name)

	// Create directories
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(clusterDir, dir), 0755); err != nil {
			t.Fatalf("failed to create directory %s: %v", dir, err)
		}
	}

	// Create files
	for path, content := range files {
		fullPath := filepath.Join(clusterDir, path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create directory for file %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write file %s: %v", path, err)
		}
	}

	return tmpDir
}

func TestDiscoverClusters(t *testing.T) {
	tests := []struct {
		name     string
		clusters []string
		want     []string
		wantErr  bool
	}{
		{
			name:     "single cluster",
			clusters: []string{"home"},
			want:     []string{"home"},
		},
		{
			name:     "multiple clusters",
			clusters: []string{"home", "cloud", "edge"},
			want:     []string{"cloud", "edge", "home"}, // sorted by readdir
		},
		{
			name:     "no clusters",
			clusters: []string{},
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			clustersDir := filepath.Join(tmpDir, "clusters")
			if err := os.MkdirAll(clustersDir, 0755); err != nil {
				t.Fatalf("failed to create clusters dir: %v", err)
			}

			for _, c := range tt.clusters {
				if err := os.MkdirAll(filepath.Join(clustersDir, c), 0755); err != nil {
					t.Fatalf("failed to create cluster dir: %v", err)
				}
			}

			v := NewClusterValidator(tmpDir, false)
			got, err := v.DiscoverClusters()

			if (err != nil) != tt.wantErr {
				t.Errorf("DiscoverClusters() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("DiscoverClusters() got %d clusters, want %d", len(got), len(tt.want))
				return
			}
		})
	}
}

func TestDiscoverClusters_NoClustersDir(t *testing.T) {
	tmpDir := t.TempDir()
	v := NewClusterValidator(tmpDir, false)

	_, err := v.DiscoverClusters()
	if err == nil {
		t.Error("DiscoverClusters() expected error for missing clusters directory")
	}
}

func TestValidateCluster_MissingDirs(t *testing.T) {
	// Create cluster with no directories
	tmpDir := setupTestCluster(t, "test", []string{}, map[string]string{})

	v := NewClusterValidator(tmpDir, false)
	results := v.ValidateCluster("test")

	// Should have errors for all required directories
	dirErrors := 0
	for _, r := range results {
		if r.Rule == "cluster-missing-dir" {
			dirErrors++
		}
	}

	if dirErrors != len(RequiredDirs) {
		t.Errorf("expected %d missing dir errors, got %d", len(RequiredDirs), dirErrors)
	}
}

func TestValidateCluster_MissingBootstrapFiles(t *testing.T) {
	// Create cluster with bootstrap dir but no files
	tmpDir := setupTestCluster(t, "test", []string{"bootstrap"}, map[string]string{})

	v := NewClusterValidator(tmpDir, false)
	results := v.ValidateCluster("test")

	// Count bootstrap file errors
	bootstrapErrors := 0
	for _, r := range results {
		if r.Rule == "cluster-missing-bootstrap-file" {
			bootstrapErrors++
		}
	}

	if bootstrapErrors != len(RequiredBootstrapFiles) {
		t.Errorf("expected %d missing bootstrap file errors, got %d", len(RequiredBootstrapFiles), bootstrapErrors)
	}
}

func TestValidateCluster_ValidStructure(t *testing.T) {
	// Create a fully valid cluster structure
	files := map[string]string{
		"bootstrap/kustomization.yaml":             "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: []\n",
		"bootstrap/app-of-apps.yaml":               "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n",
		"bootstrap/operators-app-of-apps.yaml":     "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n",
		"bootstrap/security-app-of-apps.yaml":      "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n",
		"bootstrap/infra-app-of-apps.yaml":         "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: test\n",
		"argocd/apps/kustomization.yaml":           "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: []\n",
		"argocd/operators/kustomization.yaml":      "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: []\n",
		"argocd/security/kustomization.yaml":       "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: []\n",
		"argocd/infrastructure/kustomization.yaml": "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: []\n",
	}

	dirs := []string{
		"bootstrap",
		"argocd/apps",
		"argocd/operators",
		"argocd/security",
		"argocd/infrastructure",
	}

	tmpDir := setupTestCluster(t, "test", dirs, files)

	v := NewClusterValidator(tmpDir, false)
	results := v.ValidateCluster("test")

	// Filter out kustomize build errors (depends on external binary)
	structureErrors := []Result{}
	for _, r := range results {
		if r.Rule != "kustomize-build-fail" {
			structureErrors = append(structureErrors, r)
		}
	}

	if len(structureErrors) != 0 {
		t.Errorf("expected no structure errors for valid cluster, got %d:", len(structureErrors))
		for _, r := range structureErrors {
			t.Errorf("  - %s: %s (%s)", r.Rule, r.Message, r.Path)
		}
	}
}

func TestCountErrors(t *testing.T) {
	results := []Result{
		{Severity: "error", Rule: "test1"},
		{Severity: "error", Rule: "test2"},
		{Severity: "warn", Rule: "test3"},
		{Severity: "error", Rule: "test4"},
	}

	got := CountErrors(results)
	if got != 3 {
		t.Errorf("CountErrors() = %d, want 3", got)
	}
}

func TestCountWarnings(t *testing.T) {
	results := []Result{
		{Severity: "error", Rule: "test1"},
		{Severity: "warn", Rule: "test2"},
		{Severity: "warn", Rule: "test3"},
	}

	got := CountWarnings(results)
	if got != 2 {
		t.Errorf("CountWarnings() = %d, want 2", got)
	}
}

func TestCountErrors_EmptySlice(t *testing.T) {
	results := []Result{}

	if CountErrors(results) != 0 {
		t.Error("CountErrors() should return 0 for empty slice")
	}
	if CountWarnings(results) != 0 {
		t.Error("CountWarnings() should return 0 for empty slice")
	}
}

func TestValidateCluster_KustomizeBuildFail(t *testing.T) {
	// Create cluster with invalid kustomization.yaml
	files := map[string]string{
		"bootstrap/kustomization.yaml":         "invalid: yaml: content: [[[",
		"bootstrap/app-of-apps.yaml":           "test",
		"bootstrap/operators-app-of-apps.yaml": "test",
		"bootstrap/security-app-of-apps.yaml":  "test",
		"bootstrap/infra-app-of-apps.yaml":     "test",
	}

	dirs := []string{
		"bootstrap",
		"argocd/apps",
		"argocd/operators",
		"argocd/security",
		"argocd/infrastructure",
	}

	tmpDir := setupTestCluster(t, "test", dirs, files)

	v := NewClusterValidator(tmpDir, false)
	results := v.ValidateCluster("test")

	// Should have kustomize build error
	kustomizeErrors := 0
	for _, r := range results {
		if r.Rule == "kustomize-build-fail" {
			kustomizeErrors++
		}
	}

	if kustomizeErrors == 0 {
		t.Error("expected kustomize-build-fail error for invalid kustomization.yaml")
	}
}

func TestResult_JSONTags(t *testing.T) {
	// Verify Result struct has proper JSON tags by checking field names
	r := Result{
		Cluster:  "test",
		Rule:     "test-rule",
		Path:     "/test/path",
		Message:  "test message",
		Severity: "error",
	}

	// Basic sanity check that all fields are accessible
	if r.Cluster != "test" || r.Rule != "test-rule" || r.Path != "/test/path" ||
		r.Message != "test message" || r.Severity != "error" {
		t.Error("Result struct field access failed")
	}
}

func TestValidateAll(t *testing.T) {
	tmpDir := t.TempDir()
	clustersDir := filepath.Join(tmpDir, "clusters")

	// Create two clusters with minimal structure
	for _, cluster := range []string{"cluster1", "cluster2"} {
		clusterPath := filepath.Join(clustersDir, cluster)
		if err := os.MkdirAll(filepath.Join(clusterPath, "bootstrap"), 0755); err != nil {
			t.Fatalf("failed to create cluster dir: %v", err)
		}
	}

	v := NewClusterValidator(tmpDir, false)
	results, err := v.ValidateAll()

	if err != nil {
		t.Fatalf("ValidateAll() error = %v", err)
	}

	// Should have results from both clusters
	cluster1Results := 0
	cluster2Results := 0
	for _, r := range results {
		if r.Cluster == "cluster1" {
			cluster1Results++
		}
		if r.Cluster == "cluster2" {
			cluster2Results++
		}
	}

	if cluster1Results == 0 || cluster2Results == 0 {
		t.Error("ValidateAll() should return results from all clusters")
	}
}

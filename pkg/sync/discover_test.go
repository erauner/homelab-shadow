package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverKustomizationsForSync(t *testing.T) {
	// Create a temporary directory structure for testing
	tempDir, err := os.MkdirTemp("", "discover-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test directory structure
	dirs := []string{
		// Apps - legacy flat structure (should still be discovered for backward compatibility)
		"apps/giraffe/overlays/production",
		"apps/coder/overlays/production",
		"apps/coder/overlays/staging",
		// Apps base - should NOT be discovered (we only want overlays)
		"apps/giraffe/base",
		// Infrastructure - should be discovered
		"infrastructure/argocd/overlays/erauner-home",
		"infrastructure/envoy-gateway/overlays/erauner-home",
		// Infrastructure base - should NOT be discovered
		"infrastructure/argocd/base",
		// Operators - should be discovered
		"operators/cert-manager/overlays/erauner-home",
		// Security - should be discovered
		"security/namespaces/overlays/erauner-home",
		// Other dirs - should NOT be discovered (not in patterns)
		"clusters/erauner-home/bootstrap",
		"terraform/modules",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(tempDir, dir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			t.Fatalf("Failed to create dir %s: %v", dir, err)
		}
		// Create kustomization.yaml in each
		kustomizationPath := filepath.Join(fullPath, "kustomization.yaml")
		if err := os.WriteFile(kustomizationPath, []byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"), 0644); err != nil {
			t.Fatalf("Failed to create kustomization.yaml: %v", err)
		}
	}

	// Run discovery
	discovered, err := DiscoverKustomizationsForSync(tempDir, nil)
	if err != nil {
		t.Fatalf("DiscoverKustomizationsForSync() error = %v", err)
	}

	// Check expected directories are found
	expected := map[string]bool{
		"apps/giraffe/overlays/production":                  true,
		"apps/coder/overlays/production":                    true,
		"apps/coder/overlays/staging":                       true,
		"infrastructure/argocd/overlays/erauner-home":       true,
		"infrastructure/envoy-gateway/overlays/erauner-home": true,
		"operators/cert-manager/overlays/erauner-home":      true,
		"security/namespaces/overlays/erauner-home":         true,
	}

	// Check unexpected directories are NOT found
	unexpected := []string{
		"apps/giraffe/base",
		"infrastructure/argocd/base",
		"clusters/erauner-home/bootstrap",
		"terraform/modules",
	}

	// Convert discovered to map for easier checking
	discoveredMap := make(map[string]bool)
	for _, d := range discovered {
		discoveredMap[d] = true
	}

	// Verify expected dirs are found
	for dir := range expected {
		if !discoveredMap[dir] {
			t.Errorf("Expected to discover %q but it was not found", dir)
		}
	}

	// Verify unexpected dirs are NOT found
	for _, dir := range unexpected {
		if discoveredMap[dir] {
			t.Errorf("Did not expect to discover %q but it was found", dir)
		}
	}
}

func TestDiscoverKustomizationsForSync_ClusterAwareApps(t *testing.T) {
	// Test the new cluster-aware app overlay structure (issue #1256)
	tempDir, err := os.MkdirTemp("", "discover-cluster-aware-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dirs := []string{
		// New cluster-aware structure: apps/<app>/overlays/<cluster>/<env>
		"apps/coder/overlays/erauner-home/production",
		"apps/coder/overlays/erauner-home/staging",
		"apps/coder/overlays/erauner-cloud/production",
		"apps/giraffe/overlays/erauner-home/production",
		// Stack pattern with cluster layer
		"apps/media-stack/stack/erauner-home/production",
		// DB overlays with cluster layer
		"apps/coder/db/overlays/erauner-home/production",
		// Base directories - should NOT be discovered
		"apps/coder/base",
		"apps/coder/overlays/erauner-home", // Cluster dir itself, not a kustomization
		// Infrastructure remains the same
		"infrastructure/argocd/overlays/erauner-home",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(tempDir, dir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			t.Fatalf("Failed to create dir %s: %v", dir, err)
		}
		// Only create kustomization.yaml in leaf directories (not cluster directories)
		if dir != "apps/coder/overlays/erauner-home" {
			kustomizationPath := filepath.Join(fullPath, "kustomization.yaml")
			if err := os.WriteFile(kustomizationPath, []byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"), 0644); err != nil {
				t.Fatalf("Failed to create kustomization.yaml: %v", err)
			}
		}
	}

	// Test without cluster filter - should find all
	discovered, err := DiscoverKustomizationsForSync(tempDir, nil)
	if err != nil {
		t.Fatalf("DiscoverKustomizationsForSync() error = %v", err)
	}

	expected := map[string]bool{
		"apps/coder/overlays/erauner-home/production":    true,
		"apps/coder/overlays/erauner-home/staging":       true,
		"apps/coder/overlays/erauner-cloud/production":   true,
		"apps/giraffe/overlays/erauner-home/production":  true,
		"apps/media-stack/stack/erauner-home/production": true,
		"apps/coder/db/overlays/erauner-home/production": true,
		"infrastructure/argocd/overlays/erauner-home":    true,
	}

	unexpected := []string{
		"apps/coder/base",
		"apps/coder/overlays/erauner-home", // Cluster dir, not a kustomization target
	}

	discoveredMap := make(map[string]bool)
	for _, d := range discovered {
		discoveredMap[d] = true
	}

	for dir := range expected {
		if !discoveredMap[dir] {
			t.Errorf("Expected to discover %q but it was not found. Discovered: %v", dir, discovered)
		}
	}

	for _, dir := range unexpected {
		if discoveredMap[dir] {
			t.Errorf("Did not expect to discover %q but it was found", dir)
		}
	}
}

func TestDiscoverKustomizationsForSync_ClusterFilterWithClusterAwareApps(t *testing.T) {
	// Test cluster filtering with new cluster-aware app structure
	tempDir, err := os.MkdirTemp("", "discover-cluster-filter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dirs := []string{
		// Cluster-aware apps
		"apps/coder/overlays/erauner-home/production",
		"apps/coder/overlays/erauner-cloud/production",
		"apps/giraffe/overlays/erauner-home/production",
		"apps/giraffe/overlays/erauner-cloud/staging",
		// Infrastructure
		"infrastructure/argocd/overlays/erauner-home",
		"infrastructure/argocd/overlays/erauner-cloud",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(tempDir, dir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			t.Fatalf("Failed to create dir %s: %v", dir, err)
		}
		kustomizationPath := filepath.Join(fullPath, "kustomization.yaml")
		if err := os.WriteFile(kustomizationPath, []byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"), 0644); err != nil {
			t.Fatalf("Failed to create kustomization.yaml: %v", err)
		}
	}

	// Filter to "erauner-home" cluster only
	discovered, err := DiscoverKustomizationsForSync(tempDir, []string{"erauner-home"})
	if err != nil {
		t.Fatalf("DiscoverKustomizationsForSync() error = %v", err)
	}

	// Should find erauner-home cluster overlays only
	shouldFind := []string{
		"apps/coder/overlays/erauner-home/production",
		"apps/giraffe/overlays/erauner-home/production",
		"infrastructure/argocd/overlays/erauner-home",
	}

	shouldNotFind := []string{
		"apps/coder/overlays/erauner-cloud/production",
		"apps/giraffe/overlays/erauner-cloud/staging",
		"infrastructure/argocd/overlays/erauner-cloud",
	}

	discoveredMap := make(map[string]bool)
	for _, d := range discovered {
		discoveredMap[d] = true
	}

	for _, dir := range shouldFind {
		if !discoveredMap[dir] {
			t.Errorf("With erauner-home cluster filter, expected to find %q but it was not found", dir)
		}
	}

	for _, dir := range shouldNotFind {
		if discoveredMap[dir] {
			t.Errorf("With erauner-home cluster filter, did not expect to find %q but it was found", dir)
		}
	}
}

func TestDiscoverKustomizationsForSync_MixedLegacyAndClusterAware(t *testing.T) {
	// Test discovery with mix of legacy and cluster-aware structures (migration period)
	tempDir, err := os.MkdirTemp("", "discover-mixed-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dirs := []string{
		// Some apps migrated to cluster-aware
		"apps/coder/overlays/erauner-home/production",
		"apps/coder/overlays/erauner-home/staging",
		// Some apps still using legacy flat structure
		"apps/legacy-app/overlays/production",
		"apps/legacy-app/overlays/staging",
		// Infrastructure already cluster-aware
		"infrastructure/argocd/overlays/erauner-home",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(tempDir, dir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			t.Fatalf("Failed to create dir %s: %v", dir, err)
		}
		kustomizationPath := filepath.Join(fullPath, "kustomization.yaml")
		if err := os.WriteFile(kustomizationPath, []byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"), 0644); err != nil {
			t.Fatalf("Failed to create kustomization.yaml: %v", err)
		}
	}

	discovered, err := DiscoverKustomizationsForSync(tempDir, nil)
	if err != nil {
		t.Fatalf("DiscoverKustomizationsForSync() error = %v", err)
	}

	// Should find both cluster-aware and legacy
	expected := map[string]bool{
		"apps/coder/overlays/erauner-home/production":  true,
		"apps/coder/overlays/erauner-home/staging":     true,
		"apps/legacy-app/overlays/production":          true,
		"apps/legacy-app/overlays/staging":             true,
		"infrastructure/argocd/overlays/erauner-home":  true,
	}

	discoveredMap := make(map[string]bool)
	for _, d := range discovered {
		discoveredMap[d] = true
	}

	for dir := range expected {
		if !discoveredMap[dir] {
			t.Errorf("Expected to discover %q but it was not found. Discovered: %v", dir, discovered)
		}
	}
}

func TestExtractClusterFromAppPath(t *testing.T) {
	tests := []struct {
		path          string
		wantCluster   string
		wantOK        bool
	}{
		// New cluster-aware patterns
		{"apps/coder/overlays/erauner-home/production", "erauner-home", true},
		{"apps/coder/overlays/erauner-cloud/staging", "erauner-cloud", true},
		{"apps/media-stack/stack/erauner-home/production", "erauner-home", true},
		{"apps/coder/db/overlays/erauner-home/production", "erauner-home", true},
		// Legacy patterns - no cluster extraction
		{"apps/coder/overlays/production", "", false},
		{"apps/coder/stack/production", "", false},
		// Infrastructure patterns - not app paths
		{"infrastructure/argocd/overlays/erauner-home", "", false},
		// Invalid paths
		{"something/else", "", false},
		{"apps", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			cluster, ok := extractClusterFromAppPath(tt.path)
			if ok != tt.wantOK {
				t.Errorf("extractClusterFromAppPath(%q) ok = %v, want %v", tt.path, ok, tt.wantOK)
			}
			if cluster != tt.wantCluster {
				t.Errorf("extractClusterFromAppPath(%q) cluster = %q, want %q", tt.path, cluster, tt.wantCluster)
			}
		})
	}
}

func TestDiscoverKustomizationsForSync_WithClusterFilter(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "discover-filter-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create structure with multiple clusters
	dirs := []string{
		"infrastructure/argocd/overlays/erauner-home",
		"infrastructure/argocd/overlays/erauner-cloud",
		"apps/myapp/overlays/production",
		"apps/myapp/overlays/staging",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(tempDir, dir)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			t.Fatalf("Failed to create dir %s: %v", dir, err)
		}
		kustomizationPath := filepath.Join(fullPath, "kustomization.yaml")
		if err := os.WriteFile(kustomizationPath, []byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"), 0644); err != nil {
			t.Fatalf("Failed to create kustomization.yaml: %v", err)
		}
	}

	// Test with cluster filter
	discovered, err := DiscoverKustomizationsForSync(tempDir, []string{"erauner-home"})
	if err != nil {
		t.Fatalf("DiscoverKustomizationsForSync() error = %v", err)
	}

	// Should find erauner-home overlay but not erauner-cloud
	foundHome := false
	foundCloud := false
	for _, d := range discovered {
		if d == "infrastructure/argocd/overlays/erauner-home" {
			foundHome = true
		}
		if d == "infrastructure/argocd/overlays/erauner-cloud" {
			foundCloud = true
		}
	}

	if !foundHome {
		t.Error("Expected to find erauner-home overlay with cluster filter")
	}
	if foundCloud {
		t.Error("Did not expect to find erauner-cloud overlay with erauner-home cluster filter")
	}
}

func TestDiscoverKustomizationsForSync_EmptyRepo(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "discover-empty-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Empty repo - no kustomizations
	discovered, err := DiscoverKustomizationsForSync(tempDir, nil)
	if err != nil {
		t.Fatalf("DiscoverKustomizationsForSync() error = %v", err)
	}

	if len(discovered) != 0 {
		t.Errorf("Expected 0 discoveries in empty repo, got %d", len(discovered))
	}
}

func TestDiscoverKustomizationsForSync_NoKustomizationYaml(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "discover-no-kustomization-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create directory structure but without kustomization.yaml
	dir := filepath.Join(tempDir, "apps/myapp/overlays/production")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	discovered, err := DiscoverKustomizationsForSync(tempDir, nil)
	if err != nil {
		t.Fatalf("DiscoverKustomizationsForSync() error = %v", err)
	}

	// Should not discover directory without kustomization.yaml
	if len(discovered) != 0 {
		t.Errorf("Expected 0 discoveries without kustomization.yaml, got %d", len(discovered))
	}
}

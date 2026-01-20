// Package validate provides GitOps structure validation
package validate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// InfraIgnoreDirs are directories under infrastructure/ that are NOT components
// These are either legacy or special-purpose directories
var InfraIgnoreDirs = map[string]bool{
	"base":       true, // Legacy flat base (to be removed after migration)
	"core":       true, // Special directory for core services
	"crds":       true, // CRD definitions (separate from components)
	"namespaces": true, // May be special structure
	"secrets":    true, // Secrets directory
	"jobs":       true, // Empty/legacy directory
}

// OperatorsIgnoreDirs are directories under operators/ that are NOT components
// Currently empty but kept for symmetry and future additions
var OperatorsIgnoreDirs = map[string]bool{}

// SecurityIgnoreDirs are directories under security/ that are NOT components
// Currently empty but kept for symmetry and future additions
var SecurityIgnoreDirs = map[string]bool{}

// ComponentRoot describes a top-level component root directory
type ComponentRoot struct {
	Name       string          // "infrastructure" | "operators"
	RelPath    string          // directory at repo root
	IgnoreDirs map[string]bool // root-specific ignores
}

// Result represents a validation finding
type Result struct {
	Cluster  string `json:"cluster"`
	Rule     string `json:"rule"`
	Path     string `json:"path"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // "error" or "warn"
}

// ClusterValidator validates the multi-cluster directory structure
type ClusterValidator struct {
	RepoPath string
	Verbose  bool
}

// RequiredDirs defines the required directories for each cluster
var RequiredDirs = []string{
	"bootstrap",
	"argocd/apps",
	"argocd/operators",
	"argocd/security",
	"argocd/infrastructure",
}

// RequiredBootstrapFiles defines required files in bootstrap/
var RequiredBootstrapFiles = []string{
	"kustomization.yaml",
	"app-of-apps.yaml",
	"operators-app-of-apps.yaml",
	"security-app-of-apps.yaml",
	"infra-app-of-apps.yaml",
}

// KustomizePaths defines paths that must build successfully
var KustomizePaths = []string{
	"bootstrap",
	"argocd/apps",
	"argocd/operators",
	"argocd/security",
	"argocd/infrastructure",
}

// NewClusterValidator creates a new cluster validator
func NewClusterValidator(repoPath string, verbose bool) *ClusterValidator {
	return &ClusterValidator{
		RepoPath: repoPath,
		Verbose:  verbose,
	}
}

// DiscoverClusters finds all cluster directories
func (v *ClusterValidator) DiscoverClusters() ([]string, error) {
	clustersDir := filepath.Join(v.RepoPath, "clusters")

	entries, err := os.ReadDir(clustersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("clusters directory not found at %s", clustersDir)
		}
		return nil, fmt.Errorf("failed to read clusters directory: %w", err)
	}

	var clusters []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			clusters = append(clusters, entry.Name())
		}
	}

	return clusters, nil
}

// ValidateAll validates all discovered clusters
func (v *ClusterValidator) ValidateAll() ([]Result, error) {
	clusters, err := v.DiscoverClusters()
	if err != nil {
		return nil, err
	}

	results := []Result{} // Initialize to empty slice, not nil
	for _, cluster := range clusters {
		clusterResults := v.ValidateCluster(cluster)
		results = append(results, clusterResults...)
	}

	return results, nil
}

// ValidateCluster validates a single cluster's structure
func (v *ClusterValidator) ValidateCluster(cluster string) []Result {
	results := []Result{} // Initialize to empty slice for consistent JSON output
	clusterPath := filepath.Join(v.RepoPath, "clusters", cluster)

	// Check required directories
	for _, dir := range RequiredDirs {
		dirPath := filepath.Join(clusterPath, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			results = append(results, Result{
				Cluster:  cluster,
				Rule:     "cluster-missing-dir",
				Path:     dir,
				Message:  fmt.Sprintf("Missing required directory: %s", dir),
				Severity: "error",
			})
		}
	}

	// Check required bootstrap files
	for _, file := range RequiredBootstrapFiles {
		filePath := filepath.Join(clusterPath, "bootstrap", file)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			results = append(results, Result{
				Cluster:  cluster,
				Rule:     "cluster-missing-bootstrap-file",
				Path:     filepath.Join("bootstrap", file),
				Message:  fmt.Sprintf("Missing required bootstrap file: %s", file),
				Severity: "error",
			})
		}
	}

	// Validate kustomize builds
	for _, kpath := range KustomizePaths {
		fullPath := filepath.Join(clusterPath, kpath)
		if _, err := os.Stat(filepath.Join(fullPath, "kustomization.yaml")); err == nil {
			if err := v.validateKustomizeBuild(fullPath); err != nil {
				results = append(results, Result{
					Cluster:  cluster,
					Rule:     "kustomize-build-fail",
					Path:     kpath,
					Message:  fmt.Sprintf("Kustomize build failed: %v", err),
					Severity: "error",
				})
			} else if v.Verbose {
				fmt.Fprintf(os.Stderr, "[shadow] %s/%s: kustomize build OK\n", cluster, kpath)
			}
		}
	}

	return results
}

// validateKustomizeBuild runs kustomize build and checks for errors
func (v *ClusterValidator) validateKustomizeBuild(path string) error {
	cmd := exec.Command("kustomize", "build", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Extract first line of error for cleaner message
		lines := strings.Split(string(output), "\n")
		if len(lines) > 0 && lines[0] != "" {
			return fmt.Errorf("%s", strings.TrimSpace(lines[0]))
		}
		return err
	}
	return nil
}

// CountErrors returns the number of error-severity results
func CountErrors(results []Result) int {
	count := 0
	for _, r := range results {
		if r.Severity == "error" {
			count++
		}
	}
	return count
}

// CountWarnings returns the number of warn-severity results
func CountWarnings(results []Result) int {
	count := 0
	for _, r := range results {
		if r.Severity == "warn" {
			count++
		}
	}
	return count
}

// ============================================================================
// Infrastructure validation (new pattern: infrastructure/<component>/base + overlays/<cluster>)
// ============================================================================

// KustomizationFile represents a parsed kustomization.yaml
type KustomizationFile struct {
	Resources  []string `yaml:"resources"`
	Bases      []string `yaml:"bases"`      // deprecated but still used
	Components []string `yaml:"components"`
	Generators []string `yaml:"generators"` // Secret/ConfigMap generators
	HelmCharts []struct {
		Name string `yaml:"name"`
	} `yaml:"helmCharts"` // If overlay defines helmCharts, it replaces base config
}

// ArgoCDApplication represents an ArgoCD Application manifest (partial)
type ArgoCDApplication struct {
	Kind string `yaml:"kind"`
	Spec struct {
		Source struct {
			Path string `yaml:"path"`
		} `yaml:"source"`
		Sources []struct {
			Path string `yaml:"path"`
		} `yaml:"sources"`
	} `yaml:"spec"`
}

// DiscoverComponents finds all component directories under a given root
// Components are directories that follow the base/overlays pattern
func (v *ClusterValidator) DiscoverComponents(root ComponentRoot) ([]string, error) {
	rootDir := filepath.Join(v.RepoPath, root.RelPath)

	entries, err := os.ReadDir(rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No directory is OK
		}
		return nil, fmt.Errorf("failed to read %s directory: %w", root.Name, err)
	}

	var components []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip ignored directories and hidden directories
		if root.IgnoreDirs[name] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		components = append(components, name)
	}

	return components, nil
}

// DiscoverInfrastructureComponents finds all component directories under infrastructure/
// Components are directories that follow the base/overlays pattern
// Deprecated: Use DiscoverComponents with ComponentRoot instead
func (v *ClusterValidator) DiscoverInfrastructureComponents() ([]string, error) {
	return v.DiscoverComponents(ComponentRoot{
		Name:       "infrastructure",
		RelPath:    "infrastructure",
		IgnoreDirs: InfraIgnoreDirs,
	})
}

// ValidateInfrastructure validates the infrastructure/ directory structure
func (v *ClusterValidator) ValidateInfrastructure(clusters []string) []Result {
	return v.ValidateComponentRoots(clusters)
}

// ValidateComponentRoots validates both infrastructure/ and operators/ directory structures
func (v *ClusterValidator) ValidateComponentRoots(clusters []string) []Result {
	results := []Result{}

	// Define component roots to validate
	roots := []ComponentRoot{
		{Name: "infrastructure", RelPath: "infrastructure", IgnoreDirs: InfraIgnoreDirs},
		{Name: "operators", RelPath: "operators", IgnoreDirs: OperatorsIgnoreDirs},
		{Name: "security", RelPath: "security", IgnoreDirs: SecurityIgnoreDirs},
	}

	for _, root := range roots {
		// Get components for this root
		components, err := v.DiscoverComponents(root)
		if err != nil {
			results = append(results, Result{
				Cluster:  "global",
				Rule:     fmt.Sprintf("%s-discovery-error", root.Name),
				Path:     root.RelPath + "/",
				Message:  fmt.Sprintf("Failed to discover components: %v", err),
				Severity: "error",
			})
			continue
		}

		// Skip if no components found (e.g., operators/ may not exist yet)
		if len(components) == 0 {
			continue
		}

		// Validate each component has base/ and overlays/ structure
		results = append(results, v.validateComponentStructure(root, components)...)

		// Validate overlay base references
		results = append(results, v.validateOverlayBaseRefs(root, components, clusters)...)
	}

	// Validate ArgoCD apps don't use legacy paths
	results = append(results, v.validateArgoCDInfraApps()...)

	return results
}

// validateComponentStructure checks that each component has base/ and overlays/
func (v *ClusterValidator) validateComponentStructure(root ComponentRoot, components []string) []Result {
	results := []Result{}

	for _, component := range components {
		componentPath := filepath.Join(v.RepoPath, root.RelPath, component)

		// Check for base/ directory
		basePath := filepath.Join(componentPath, "base")
		if info, err := os.Stat(basePath); os.IsNotExist(err) || !info.IsDir() {
			results = append(results, Result{
				Cluster:  "global",
				Rule:     fmt.Sprintf("%s-component-structure", root.Name),
				Path:     fmt.Sprintf("%s/%s", root.RelPath, component),
				Message:  "Component missing base/ directory",
				Severity: "error",
			})
		}

		// Check for overlays/ directory
		overlaysPath := filepath.Join(componentPath, "overlays")
		if info, err := os.Stat(overlaysPath); os.IsNotExist(err) || !info.IsDir() {
			results = append(results, Result{
				Cluster:  "global",
				Rule:     fmt.Sprintf("%s-component-structure", root.Name),
				Path:     fmt.Sprintf("%s/%s", root.RelPath, component),
				Message:  "Component missing overlays/ directory",
				Severity: "error",
			})
		}

		if v.Verbose {
			fmt.Fprintf(os.Stderr, "[shadow] %s/%s: structure OK\n", root.RelPath, component)
		}
	}

	return results
}

// validateInfrastructureComponentStructure checks that each component has base/ and overlays/
// Deprecated: Use validateComponentStructure with ComponentRoot instead
func (v *ClusterValidator) validateInfrastructureComponentStructure(components []string) []Result {
	return v.validateComponentStructure(ComponentRoot{
		Name:       "infrastructure",
		RelPath:    "infrastructure",
		IgnoreDirs: InfraIgnoreDirs,
	}, components)
}

// validateOverlayBaseRefs checks that overlays reference ../../base
func (v *ClusterValidator) validateOverlayBaseRefs(root ComponentRoot, components []string, clusters []string) []Result {
	results := []Result{}

	for _, component := range components {
		overlaysPath := filepath.Join(v.RepoPath, root.RelPath, component, "overlays")

		// Check each cluster overlay
		for _, cluster := range clusters {
			clusterOverlayPath := filepath.Join(overlaysPath, cluster)
			kustomizationPath := filepath.Join(clusterOverlayPath, "kustomization.yaml")

			// Skip if overlay doesn't exist for this cluster
			if _, err := os.Stat(kustomizationPath); os.IsNotExist(err) {
				continue
			}

			// Parse kustomization.yaml
			data, err := os.ReadFile(kustomizationPath)
			if err != nil {
				results = append(results, Result{
					Cluster:  cluster,
					Rule:     fmt.Sprintf("%s-overlay-base-ref", root.Name),
					Path:     fmt.Sprintf("%s/%s/overlays/%s", root.RelPath, component, cluster),
					Message:  fmt.Sprintf("Failed to read kustomization.yaml: %v", err),
					Severity: "error",
				})
				continue
			}

			var kustomization KustomizationFile
			if err := yaml.Unmarshal(data, &kustomization); err != nil {
				results = append(results, Result{
					Cluster:  cluster,
					Rule:     fmt.Sprintf("%s-overlay-base-ref", root.Name),
					Path:     fmt.Sprintf("%s/%s/overlays/%s", root.RelPath, component, cluster),
					Message:  fmt.Sprintf("Failed to parse kustomization.yaml: %v", err),
					Severity: "error",
				})
				continue
			}

			// Check if ../../base is referenced
			allRefs := append(kustomization.Resources, kustomization.Bases...)
			hasBaseRef := false
			for _, ref := range allRefs {
				normalized := strings.TrimSuffix(strings.TrimSpace(ref), "/")
				if normalized == "../../base" {
					hasBaseRef = true
					break
				}
			}

			// If overlay has its own helmCharts, it's intentionally replacing base config
			// (e.g., oauth2-proxy overlay with Dex-specific values)
			hasOwnHelmCharts := len(kustomization.HelmCharts) > 0

			if !hasBaseRef && !hasOwnHelmCharts {
				results = append(results, Result{
					Cluster:  cluster,
					Rule:     fmt.Sprintf("%s-overlay-base-ref", root.Name),
					Path:     fmt.Sprintf("%s/%s/overlays/%s/kustomization.yaml", root.RelPath, component, cluster),
					Message:  "Overlay must include ../../base in resources (or define its own helmCharts)",
					Severity: "error",
				})
			} else if v.Verbose {
				if hasOwnHelmCharts && !hasBaseRef {
					fmt.Fprintf(os.Stderr, "[shadow] %s/%s/overlays/%s: has own helmCharts (base ref skipped)\n", root.RelPath, component, cluster)
				} else {
					fmt.Fprintf(os.Stderr, "[shadow] %s/%s/overlays/%s: base ref OK\n", root.RelPath, component, cluster)
				}
			}
		}
	}

	return results
}

// validateInfrastructureOverlayBaseRefs checks that overlays reference ../../base
// Deprecated: Use validateOverlayBaseRefs with ComponentRoot instead
func (v *ClusterValidator) validateInfrastructureOverlayBaseRefs(components []string, clusters []string) []Result {
	return v.validateOverlayBaseRefs(ComponentRoot{
		Name:       "infrastructure",
		RelPath:    "infrastructure",
		IgnoreDirs: InfraIgnoreDirs,
	}, components, clusters)
}

// validateArgoCDInfraApps checks that ArgoCD apps don't use legacy infrastructure paths
func (v *ClusterValidator) validateArgoCDInfraApps() []Result {
	results := []Result{}

	argoAppsDir := filepath.Join(v.RepoPath, "argocd-apps", "infrastructure")
	if _, err := os.Stat(argoAppsDir); os.IsNotExist(err) {
		return results // No ArgoCD apps directory
	}

	// Walk all YAML files in argocd-apps/infrastructure/
	err := filepath.WalkDir(argoAppsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}

		// Parse the file for ArgoCD Applications
		appResults := v.validateArgoCDAppFile(path)
		results = append(results, appResults...)
		return nil
	})

	if err != nil {
		results = append(results, Result{
			Cluster:  "global",
			Rule:     "argocd-app-validation-error",
			Path:     "argocd-apps/infrastructure/",
			Message:  fmt.Sprintf("Failed to walk directory: %v", err),
			Severity: "error",
		})
	}

	return results
}

// validateArgoCDAppFile validates a single ArgoCD Application file
func (v *ClusterValidator) validateArgoCDAppFile(filePath string) []Result {
	results := []Result{}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return results // Skip files that can't be read
	}

	// Handle multi-document YAML files
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var doc ArgoCDApplication
		if err := decoder.Decode(&doc); err != nil {
			break // End of documents or parse error
		}

		if doc.Kind != "Application" {
			continue
		}

		relPath, _ := filepath.Rel(v.RepoPath, filePath)

		// Check spec.source.path
		if doc.Spec.Source.Path != "" {
			results = append(results, v.validateArgoCDPath(doc.Spec.Source.Path, relPath)...)
		}

		// Check spec.sources[].path (multi-source)
		for _, source := range doc.Spec.Sources {
			if source.Path != "" {
				results = append(results, v.validateArgoCDPath(source.Path, relPath)...)
			}
		}
	}

	return results
}

// validateArgoCDPath checks a single ArgoCD source path for legacy patterns
func (v *ClusterValidator) validateArgoCDPath(sourcePath, filePath string) []Result {
	results := []Result{}

	// Normalize path (remove leading ./)
	normalized := strings.TrimPrefix(sourcePath, "./")

	// Rule: argocd-app-no-flat-infra-base
	// Block: infrastructure/base/<component>
	if strings.HasPrefix(normalized, "infrastructure/base/") {
		results = append(results, Result{
			Cluster:  "global",
			Rule:     "argocd-app-no-flat-infra-base",
			Path:     filePath,
			Message:  fmt.Sprintf("ArgoCD app uses legacy path %q - should use infrastructure/<component>/overlays/<cluster> or operators/<component>/overlays/<cluster>", sourcePath),
			Severity: "error",
		})
	}

	// Rule: argocd-app-no-clusters-infra
	// Block: clusters/<cluster>/infrastructure/<component>
	if strings.HasPrefix(normalized, "clusters/") && strings.Contains(normalized, "/infrastructure/") {
		results = append(results, Result{
			Cluster:  "global",
			Rule:     "argocd-app-no-clusters-infra",
			Path:     filePath,
			Message:  fmt.Sprintf("ArgoCD app uses legacy path %q - should use infrastructure/<component>/overlays/<cluster> or operators/<component>/overlays/<cluster>", sourcePath),
			Severity: "error",
		})
	}

	// Rule: argocd-app-no-clusters-operators
	// Block: clusters/<cluster>/operators/<component>
	if strings.HasPrefix(normalized, "clusters/") && strings.Contains(normalized, "/operators/") {
		results = append(results, Result{
			Cluster:  "global",
			Rule:     "argocd-app-no-clusters-operators",
			Path:     filePath,
			Message:  fmt.Sprintf("ArgoCD app uses legacy path %q - should use operators/<component>/overlays/<cluster>", sourcePath),
			Severity: "error",
		})
	}

	return results
}

// ============================================================================
// Namespace location validation (issue #950: centralize namespaces in security/)
// ============================================================================

// AllowedNamespaceDirs defines directories where namespace definitions are allowed
// All namespace definitions should be centralized in security/namespaces/
// This follows platform-managed namespace best practice where:
// - Namespaces are infrastructure objects, not app-owned
// - Namespace lifecycle is decoupled from application lifecycle
// - Prevents accidental data loss when apps are deleted
var AllowedNamespaceDirs = []string{
	"security/namespaces/",
}

// LegacyNamespaceDirs are directories that currently contain namespaces but should be migrated
// These generate warnings but are tracked separately for migration progress
var LegacyNamespaceDirs = []string{
	"infrastructure/namespaces/", // To be migrated to security/namespaces/
}

// ExcludedNamespaceDirs are directories that should be skipped during namespace validation
// These contain templates, samples, or vendor code - not actual deployed namespaces
var ExcludedNamespaceDirs = []string{
	"kustomize/components/",       // Namespace templates, not actual namespaces
	"kustomize/bases/",            // Operator base manifests (vendor code)
	"istio-",                      // Istio samples/documentation
	"tools/",                      // Tooling directory
	"terraform/",                  // Terraform modules
	".github/",                    // GitHub workflows
}

// NamespaceManifest represents a discovered namespace manifest
type NamespaceManifest struct {
	Path      string
	Namespace string // metadata.name
}

// ValidateNamespaceLocations checks that namespace definitions are in approved directories
// Returns warnings for namespaces in legacy locations (infrastructure/namespaces/)
// and errors for namespaces in wrong locations (apps/, operators/, etc.)
func (v *ClusterValidator) ValidateNamespaceLocations() []Result {
	results := []Result{}

	// Find all namespace.yaml files
	namespaces, err := v.discoverNamespaceManifests()
	if err != nil {
		results = append(results, Result{
			Cluster:  "global",
			Rule:     "namespace-discovery-error",
			Path:     "",
			Message:  fmt.Sprintf("Failed to discover namespace manifests: %v", err),
			Severity: "error",
		})
		return results
	}

	// Check each namespace location
	for _, ns := range namespaces {
		location := v.classifyNamespaceLocation(ns.Path)
		switch location {
		case "allowed":
			// In security/namespaces/ - correct location
			continue
		case "excluded":
			// In template/sample directories - skip validation
			continue
		case "legacy":
			// In infrastructure/namespaces/ - needs migration
			results = append(results, Result{
				Cluster:  "global",
				Rule:     "namespace-legacy-location",
				Path:     ns.Path,
				Message:  fmt.Sprintf("Namespace %q in legacy location - migrate to security/namespaces/", ns.Namespace),
				Severity: "warn",
			})
		case "wrong":
			// In apps/, operators/, etc. - should not exist
			results = append(results, Result{
				Cluster:  "global",
				Rule:     "namespace-wrong-location",
				Path:     ns.Path,
				Message:  fmt.Sprintf("Namespace %q defined in app/operator directory - namespaces should be platform-managed in security/namespaces/, not owned by applications", ns.Namespace),
				Severity: "warn", // Warn for now, will be error after migration
			})
		}
	}

	// Check for duplicates (same namespace name in multiple files)
	duplicates := v.findDuplicateNamespaces(namespaces)
	for nsName, paths := range duplicates {
		if len(paths) > 1 {
			results = append(results, Result{
				Cluster:  "global",
				Rule:     "namespace-duplicate",
				Path:     strings.Join(paths, ", "),
				Message:  fmt.Sprintf("Namespace %q is defined in multiple locations - consolidate to security/namespaces/", nsName),
				Severity: "warn",
			})
		}
	}

	return results
}

// discoverNamespaceManifests finds all files that define Namespace resources
func (v *ClusterValidator) discoverNamespaceManifests() ([]NamespaceManifest, error) {
	var namespaces []NamespaceManifest

	// Walk the repo looking for namespace definitions
	err := filepath.WalkDir(v.RepoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden and vendor directories
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only check YAML files
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}

		// Skip test files
		if strings.Contains(path, "/tests/") || strings.Contains(path, "_test.") {
			return nil
		}

		// Check if file contains a Namespace definition
		ns, found := v.extractNamespaceFromFile(path)
		if found {
			relPath, _ := filepath.Rel(v.RepoPath, path)
			namespaces = append(namespaces, NamespaceManifest{
				Path:      relPath,
				Namespace: ns,
			})
		}

		return nil
	})

	return namespaces, err
}

// extractNamespaceFromFile checks if a file defines a Namespace and returns its name
func (v *ClusterValidator) extractNamespaceFromFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	// Quick check before parsing
	if !strings.Contains(string(data), "kind: Namespace") && !strings.Contains(string(data), "kind:Namespace") {
		return "", false
	}

	// Parse YAML to extract namespace name
	type NamespaceDoc struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var doc NamespaceDoc
		if err := decoder.Decode(&doc); err != nil {
			break
		}
		if doc.Kind == "Namespace" && doc.Metadata.Name != "" {
			return doc.Metadata.Name, true
		}
	}

	return "", false
}

// isAllowedNamespaceLocation checks if a path is in an approved namespace directory
func (v *ClusterValidator) isAllowedNamespaceLocation(path string) bool {
	// First check if it's in an excluded directory (templates, samples, vendor)
	for _, excluded := range ExcludedNamespaceDirs {
		if strings.HasPrefix(path, excluded) || strings.Contains(path, "/"+excluded) {
			return true // Excluded = don't warn about it
		}
	}

	// Check if it's in an allowed directory
	for _, allowed := range AllowedNamespaceDirs {
		if strings.HasPrefix(path, allowed) {
			return true
		}
	}
	return false
}

// classifyNamespaceLocation returns the classification of a namespace path:
// - "allowed": in security/namespaces/ (correct location)
// - "legacy": in infrastructure/namespaces/ (needs migration)
// - "excluded": in template/sample directories (skip validation)
// - "wrong": in apps/, operators/, or other locations (should not exist)
func (v *ClusterValidator) classifyNamespaceLocation(path string) string {
	// First check if it's in an excluded directory (templates, samples, vendor)
	for _, excluded := range ExcludedNamespaceDirs {
		if strings.HasPrefix(path, excluded) || strings.Contains(path, "/"+excluded) {
			return "excluded"
		}
	}

	// Check if it's in the allowed directory (security/namespaces/)
	for _, allowed := range AllowedNamespaceDirs {
		if strings.HasPrefix(path, allowed) {
			return "allowed"
		}
	}

	// Check if it's in a legacy directory (infrastructure/namespaces/)
	for _, legacy := range LegacyNamespaceDirs {
		if strings.HasPrefix(path, legacy) {
			return "legacy"
		}
	}

	// Everything else is wrong (apps/, operators/, clusters/, etc.)
	return "wrong"
}

// findDuplicateNamespaces groups namespace manifests by namespace name
// Excludes namespaces in excluded directories (templates, samples, vendor)
func (v *ClusterValidator) findDuplicateNamespaces(namespaces []NamespaceManifest) map[string][]string {
	byName := make(map[string][]string)
	for _, ns := range namespaces {
		// Skip excluded directories for duplicate detection
		excluded := false
		for _, excludedDir := range ExcludedNamespaceDirs {
			if strings.HasPrefix(ns.Path, excludedDir) || strings.Contains(ns.Path, "/"+excludedDir) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		byName[ns.Namespace] = append(byName[ns.Namespace], ns.Path)
	}
	return byName
}

// ============================================================================
// CreateNamespace validation (issue #950: apps should not create namespaces)
// ============================================================================

// ArgoCDAppWithSyncOptions represents an ArgoCD Application with syncOptions
type ArgoCDAppWithSyncOptions struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		SyncPolicy struct {
			SyncOptions []string `yaml:"syncOptions"`
		} `yaml:"syncPolicy"`
	} `yaml:"spec"`
}

// ValidateCreateNamespace checks that ArgoCD Applications don't use CreateNamespace=true
// Applications should never create namespaces - namespaces are platform-managed in security/namespaces/
func (v *ClusterValidator) ValidateCreateNamespace() []Result {
	results := []Result{}

	// Walk argocd-apps/applications/ looking for CreateNamespace=true
	appsDir := filepath.Join(v.RepoPath, "argocd-apps", "applications")
	if _, err := os.Stat(appsDir); os.IsNotExist(err) {
		return results // No applications directory
	}

	err := filepath.WalkDir(appsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}

		appResults := v.checkAppCreateNamespace(path)
		results = append(results, appResults...)
		return nil
	})

	if err != nil {
		results = append(results, Result{
			Cluster:  "global",
			Rule:     "create-namespace-validation-error",
			Path:     "argocd-apps/applications/",
			Message:  fmt.Sprintf("Failed to walk directory: %v", err),
			Severity: "error",
		})
	}

	return results
}

// CreateNamespaceExemptApps are applications allowed to use CreateNamespace=true
// These are typically test applications or special-purpose apps
var CreateNamespaceExemptApps = map[string]bool{
	"homelab-testapp": true, // Test application for CI/CD verification
}

// checkAppCreateNamespace checks a single ArgoCD Application for CreateNamespace=true
func (v *ClusterValidator) checkAppCreateNamespace(filePath string) []Result {
	results := []Result{}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return results
	}

	// Quick check before parsing
	if !strings.Contains(string(data), "CreateNamespace=true") {
		return results
	}

	// Parse YAML to get app name
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var app ArgoCDAppWithSyncOptions
		if err := decoder.Decode(&app); err != nil {
			break
		}

		if app.Kind != "Application" {
			continue
		}

		// Check syncOptions for CreateNamespace=true
		for _, opt := range app.Spec.SyncPolicy.SyncOptions {
			if opt == "CreateNamespace=true" {
				// Skip exempt applications
				if CreateNamespaceExemptApps[app.Metadata.Name] {
					continue
				}
				relPath, _ := filepath.Rel(v.RepoPath, filePath)
				results = append(results, Result{
					Cluster:  "global",
					Rule:     "app-create-namespace",
					Path:     relPath,
					Message:  fmt.Sprintf("Application %q uses CreateNamespace=true - namespaces should be platform-managed in security/namespaces/, not created by applications", app.Metadata.Name),
					Severity: "warn", // Warn for now, will be error after migration
				})
			}
		}
	}

	return results
}

// ============================================================================
// App overlay structure validation (issue #1256: cluster dimension for apps)
// ============================================================================

// AppsIgnoreDirs are directories under apps/ that are NOT applications
var AppsIgnoreDirs = map[string]bool{
	"_template": true, // Template directory
}

// ValidateAppOverlayStructure checks that apps use cluster-layered overlay structure
// New structure: apps/<app>/overlays/<cluster>/<env>/kustomization.yaml
// Legacy structure: apps/<app>/overlays/<env>/kustomization.yaml (emits warning)
//
// This validates:
// 1. Apps should have cluster layer in overlays (e.g., overlays/home/production/)
// 2. Cluster-layered overlays should reference ../../../base
// 3. Legacy flat overlays emit warnings to encourage migration
func (v *ClusterValidator) ValidateAppOverlayStructure(clusters []string) []Result {
	results := []Result{}

	appsDir := filepath.Join(v.RepoPath, "apps")
	if _, err := os.Stat(appsDir); os.IsNotExist(err) {
		return results // No apps directory
	}

	// Discover all apps
	apps, err := v.discoverApps()
	if err != nil {
		results = append(results, Result{
			Cluster:  "global",
			Rule:     "app-discovery-error",
			Path:     "apps/",
			Message:  fmt.Sprintf("Failed to discover apps: %v", err),
			Severity: "error",
		})
		return results
	}

	for _, app := range apps {
		appResults := v.validateSingleAppOverlayStructure(app, clusters)
		results = append(results, appResults...)
	}

	return results
}

// discoverApps finds all application directories under apps/
func (v *ClusterValidator) discoverApps() ([]string, error) {
	appsDir := filepath.Join(v.RepoPath, "apps")

	entries, err := os.ReadDir(appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read apps directory: %w", err)
	}

	var apps []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip ignored directories and hidden directories
		if AppsIgnoreDirs[name] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		apps = append(apps, name)
	}

	return apps, nil
}

// validateSingleAppOverlayStructure checks overlay structure for a single app
func (v *ClusterValidator) validateSingleAppOverlayStructure(app string, clusters []string) []Result {
	results := []Result{}

	// Check various overlay/stack locations
	// Note: "stack" directories are aggregators that combine multiple sources (app + db),
	// they intentionally don't reference base directly
	overlayRoots := []string{"overlays", "stack", "db/overlays"}

	for _, overlayRoot := range overlayRoots {
		overlaysPath := filepath.Join(v.RepoPath, "apps", app, overlayRoot)
		if _, err := os.Stat(overlaysPath); os.IsNotExist(err) {
			continue // This overlay type doesn't exist for this app
		}

		// Read immediate children of overlays directory
		entries, err := os.ReadDir(overlaysPath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}

			childName := entry.Name()
			childPath := filepath.Join(overlaysPath, childName)

			// Check if this child is a cluster directory (contains environment subdirs)
			// or a legacy environment directory (contains kustomization.yaml directly)
			kustomizationPath := filepath.Join(childPath, "kustomization.yaml")
			if _, err := os.Stat(kustomizationPath); err == nil {
				// This is a legacy flat structure: apps/<app>/overlays/<env>/
				// Check if it's NOT actually a cluster directory with environments below
				subEntries, _ := os.ReadDir(childPath)
				hasEnvSubdirs := false
				for _, sub := range subEntries {
					if sub.IsDir() && !strings.HasPrefix(sub.Name(), ".") {
						subKustomization := filepath.Join(childPath, sub.Name(), "kustomization.yaml")
						if _, err := os.Stat(subKustomization); err == nil {
							hasEnvSubdirs = true
							break
						}
					}
				}

				if !hasEnvSubdirs {
					// This is a legacy flat overlay - emit warning
					relPath := fmt.Sprintf("apps/%s/%s/%s", app, overlayRoot, childName)
					results = append(results, Result{
						Cluster:  "global",
						Rule:     "app-overlay-legacy-flat",
						Path:     relPath,
						Message:  fmt.Sprintf("App %q uses legacy flat overlay structure - migrate to apps/%s/%s/<cluster>/%s/ (issue #1256)", app, app, overlayRoot, childName),
						Severity: "warn",
					})

					// Skip base ref validation for stack directories - they aggregate multiple sources
					if overlayRoot != "stack" {
						// Validate base reference for legacy overlays
						baseRefResults := v.validateAppOverlayBaseRef(app, overlayRoot, childName, "", kustomizationPath)
						results = append(results, baseRefResults...)
					}
				}
			}

			// Check if this is a cluster directory
			isClusterDir := false
			for _, cluster := range clusters {
				if childName == cluster {
					isClusterDir = true
					break
				}
			}

			if isClusterDir || v.looksLikeClusterDir(childPath) {
				// This is a cluster directory - validate its environment subdirs
				envEntries, _ := os.ReadDir(childPath)
				for _, envEntry := range envEntries {
					if !envEntry.IsDir() || strings.HasPrefix(envEntry.Name(), ".") {
						continue
					}
					envName := envEntry.Name()
					envKustomization := filepath.Join(childPath, envName, "kustomization.yaml")
					if _, err := os.Stat(envKustomization); err == nil {
						// Skip base ref validation for stack directories - they aggregate multiple sources
						// (app overlay + db overlay) and intentionally don't reference base directly
						if overlayRoot != "stack" {
							// Validate base reference for cluster-layered overlays
							baseRefResults := v.validateAppOverlayBaseRef(app, overlayRoot, childName, envName, envKustomization)
							results = append(results, baseRefResults...)
						}

						if v.Verbose {
							fmt.Fprintf(os.Stderr, "[shadow] apps/%s/%s/%s/%s: cluster-layered structure OK\n", app, overlayRoot, childName, envName)
						}
					}
				}
			}
		}
	}

	return results
}

// looksLikeClusterDir checks if a directory looks like a cluster directory
// (contains subdirectories with kustomization.yaml files)
func (v *ClusterValidator) looksLikeClusterDir(dirPath string) bool {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		kustomization := filepath.Join(dirPath, entry.Name(), "kustomization.yaml")
		if _, err := os.Stat(kustomization); err == nil {
			return true
		}
	}
	return false
}

// AppOverlayExemptions are directory patterns that don't require base refs
// These are special-purpose directories for organization or ArgoCD-specific configs
var AppOverlayExemptions = map[string]bool{
	"httproutes": true, // HTTPRoute definitions only
	"routes":     true, // Route definitions only
	"secrets":    true, // Secret definitions only
	"patches":    true, // Patches-only overlay
}

// AppOverlayExemptSuffixes are directory name suffixes that indicate special-purpose overlays
var AppOverlayExemptSuffixes = []string{
	"-argocd",   // ArgoCD-specific configs
	"-external", // External-only configs
	"-internal", // Internal-only configs
}

// validateAppOverlayBaseRef checks that an app overlay references the correct base path
// - Cluster-layered (apps/<app>/overlays/<cluster>/<env>/): should reference ../../../base
// - Legacy flat (apps/<app>/overlays/<env>/): should reference ../../base
func (v *ClusterValidator) validateAppOverlayBaseRef(app, overlayRoot, clusterOrEnv, env, kustomizationPath string) []Result {
	results := []Result{}

	// Check for exempt directory names
	dirName := clusterOrEnv
	if env != "" {
		dirName = env
	}
	if AppOverlayExemptions[dirName] {
		return results // Exempt from base ref requirement
	}
	for _, suffix := range AppOverlayExemptSuffixes {
		if strings.HasSuffix(dirName, suffix) {
			return results // Exempt from base ref requirement
		}
	}

	data, err := os.ReadFile(kustomizationPath)
	if err != nil {
		return results
	}

	var kustomization KustomizationFile
	if err := yaml.Unmarshal(data, &kustomization); err != nil {
		return results
	}

	// Exempt overlays with only local resources (no external references)
	// These are typically HTTPRoute collections or overlay-specific resources
	hasLocalResourcesOnly := true
	for _, res := range kustomization.Resources {
		// If any resource references outside the current directory, it's not local-only
		if strings.Contains(res, "..") || strings.HasPrefix(res, "/") {
			hasLocalResourcesOnly = false
			break
		}
	}
	if hasLocalResourcesOnly && len(kustomization.Resources) > 0 {
		return results // Local resources only don't need base ref
	}

	// Exempt overlays with only generators (e.g., secret-generator.yaml)
	// These add secrets/configmaps without needing base resources
	if len(kustomization.Generators) > 0 && len(kustomization.Resources) == 0 && len(kustomization.Bases) == 0 {
		return results // Generators-only overlay doesn't need base ref
	}

	// Determine expected base reference
	var expectedBaseRef string
	var relPath string
	if env != "" {
		// Cluster-layered: apps/<app>/overlays/<cluster>/<env>/
		expectedBaseRef = "../../../base"
		relPath = fmt.Sprintf("apps/%s/%s/%s/%s", app, overlayRoot, clusterOrEnv, env)
	} else {
		// Legacy flat: apps/<app>/overlays/<env>/
		expectedBaseRef = "../../base"
		relPath = fmt.Sprintf("apps/%s/%s/%s", app, overlayRoot, clusterOrEnv)
	}

	// Handle db/overlays pattern
	// Structure: apps/<app>/db/overlays/<cluster>/<env>/ -> apps/<app>/db/base/
	// So from <env>/, we go: ../../../base (up env, up cluster, up overlays, into base)
	if strings.HasPrefix(overlayRoot, "db/") {
		if env != "" {
			// Cluster-layered: apps/<app>/db/overlays/<cluster>/<env>/ -> ../../../base
			expectedBaseRef = "../../../base"
		} else {
			// Legacy flat: apps/<app>/db/overlays/<env>/ -> ../../base
			expectedBaseRef = "../../base"
		}
	}

	// Check if base is referenced
	allRefs := append(kustomization.Resources, kustomization.Bases...)
	hasBaseRef := false
	hasCorrectBaseRef := false
	for _, ref := range allRefs {
		normalized := strings.TrimSuffix(strings.TrimSpace(ref), "/")
		if strings.HasSuffix(normalized, "/base") || normalized == expectedBaseRef || strings.Contains(normalized, "base") {
			hasBaseRef = true
			// Check if reference is exactly the base path or a file within base
			// e.g., "../../../base" or "../../../base/secret.yaml"
			if normalized == expectedBaseRef || strings.HasPrefix(normalized, expectedBaseRef+"/") {
				hasCorrectBaseRef = true
			}
		}
		// Also check for ../common pattern (some apps use common instead of base directly)
		if strings.HasSuffix(normalized, "/common") || normalized == "../common" || normalized == "../../common" || normalized == "../../../common" {
			hasBaseRef = true
			hasCorrectBaseRef = true // common overlay references base internally
		}
	}

	// If overlay has its own helmCharts, it's intentionally replacing base config
	hasOwnHelmCharts := len(kustomization.HelmCharts) > 0

	if !hasBaseRef && !hasOwnHelmCharts {
		results = append(results, Result{
			Cluster:  "global",
			Rule:     "app-overlay-missing-base",
			Path:     relPath + "/kustomization.yaml",
			Message:  fmt.Sprintf("App overlay should include %s in resources (or define its own helmCharts)", expectedBaseRef),
			Severity: "warn",
		})
	} else if hasBaseRef && !hasCorrectBaseRef && !hasOwnHelmCharts {
		results = append(results, Result{
			Cluster:  "global",
			Rule:     "app-overlay-wrong-base-ref",
			Path:     relPath + "/kustomization.yaml",
			Message:  fmt.Sprintf("App overlay base reference may be incorrect - expected %s for cluster-layered structure", expectedBaseRef),
			Severity: "warn",
		})
	}

	return results
}

// ValidateArgoCDAppPaths checks that ArgoCD Applications use correct overlay paths
// This validates that app paths match the new cluster-layered structure
func (v *ClusterValidator) ValidateArgoCDAppPaths(clusters []string) []Result {
	results := []Result{}

	// Check argocd-apps/applications/ for app path validation
	appsDir := filepath.Join(v.RepoPath, "argocd-apps", "applications")
	if _, err := os.Stat(appsDir); os.IsNotExist(err) {
		return results
	}

	err := filepath.WalkDir(appsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}

		appResults := v.validateArgoCDAppPathsInFile(path, clusters)
		results = append(results, appResults...)
		return nil
	})

	if err != nil {
		results = append(results, Result{
			Cluster:  "global",
			Rule:     "argocd-app-path-validation-error",
			Path:     "argocd-apps/applications/",
			Message:  fmt.Sprintf("Failed to walk directory: %v", err),
			Severity: "error",
		})
	}

	return results
}

// validateArgoCDAppPathsInFile validates ArgoCD Application paths in a single file
func (v *ClusterValidator) validateArgoCDAppPathsInFile(filePath string, clusters []string) []Result {
	results := []Result{}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return results
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var doc ArgoCDApplication
		if err := decoder.Decode(&doc); err != nil {
			break
		}

		if doc.Kind != "Application" {
			continue
		}

		relPath, _ := filepath.Rel(v.RepoPath, filePath)

		// Check spec.source.path
		if doc.Spec.Source.Path != "" {
			results = append(results, v.validateAppSourcePath(doc.Spec.Source.Path, relPath, clusters)...)
		}

		// Check spec.sources[].path (multi-source)
		for _, source := range doc.Spec.Sources {
			if source.Path != "" {
				results = append(results, v.validateAppSourcePath(source.Path, relPath, clusters)...)
			}
		}
	}

	return results
}

// validateAppSourcePath checks if an ArgoCD app source path uses the correct structure
func (v *ClusterValidator) validateAppSourcePath(sourcePath, filePath string, clusters []string) []Result {
	results := []Result{}

	// Normalize path
	normalized := strings.TrimPrefix(sourcePath, "./")

	// Only validate apps/ paths
	if !strings.HasPrefix(normalized, "apps/") {
		return results
	}

	// Parse the path
	parts := strings.Split(normalized, "/")

	// Expected new structure: apps/<app>/overlays/<cluster>/<env>
	// or: apps/<app>/stack/<cluster>/<env>
	// or: apps/<app>/db/overlays/<cluster>/<env>
	//
	// Legacy structure: apps/<app>/overlays/<env>

	if len(parts) >= 4 && (parts[2] == "overlays" || parts[2] == "stack") {
		// Could be apps/<app>/overlays/<env> (legacy) or apps/<app>/overlays/<cluster>/<env> (new)
		if len(parts) == 4 {
			// This is legacy: apps/<app>/overlays/<env>
			envOrCluster := parts[3]
			// Check if this looks like a cluster name
			isCluster := false
			for _, c := range clusters {
				if envOrCluster == c {
					isCluster = true
					break
				}
			}
			if !isCluster {
				// This is a legacy path pointing to an environment directly
				results = append(results, Result{
					Cluster:  "global",
					Rule:     "argocd-app-legacy-path",
					Path:     filePath,
					Message:  fmt.Sprintf("ArgoCD app uses legacy path %q - should use apps/<app>/%s/<cluster>/<env> structure (issue #1256)", sourcePath, parts[2]),
					Severity: "warn",
				})
			}
		}
		// len(parts) >= 5 is the new structure - OK
	}

	// Handle db/overlays pattern
	if len(parts) >= 5 && parts[2] == "db" && parts[3] == "overlays" {
		if len(parts) == 5 {
			// Legacy: apps/<app>/db/overlays/<env>
			results = append(results, Result{
				Cluster:  "global",
				Rule:     "argocd-app-legacy-path",
				Path:     filePath,
				Message:  fmt.Sprintf("ArgoCD app uses legacy path %q - should use apps/<app>/db/overlays/<cluster>/<env> structure (issue #1256)", sourcePath),
				Severity: "warn",
			})
		}
	}

	return results
}

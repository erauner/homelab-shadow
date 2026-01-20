package argocd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// applicationYAML represents the raw YAML structure of an ArgoCD Application
type applicationYAML struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Destination struct {
			Namespace string `yaml:"namespace"`
		} `yaml:"destination"`
		Source  *Source  `yaml:"source,omitempty"`
		Sources []Source `yaml:"sources,omitempty"`
	} `yaml:"spec"`
}

// ParseApplicationFile reads and parses an ArgoCD Application YAML file
func ParseApplicationFile(path string) (*Application, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return ParseApplicationYAML(data)
}

// ParseApplicationYAML parses ArgoCD Application YAML data
func ParseApplicationYAML(data []byte) (*Application, error) {
	var appYAML applicationYAML
	if err := yaml.Unmarshal(data, &appYAML); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Verify this is an Application
	if appYAML.Kind != "Application" {
		return nil, fmt.Errorf("not an Application resource (kind=%s)", appYAML.Kind)
	}

	app := &Application{
		Name:      appYAML.Metadata.Name,
		Namespace: appYAML.Spec.Destination.Namespace,
		Sources:   appYAML.Spec.Sources,
		Source:    appYAML.Spec.Source,
	}

	return app, nil
}

// DiscoverApplications finds all ArgoCD Application files in a directory tree
func DiscoverApplications(rootPath string) ([]string, error) {
	var apps []string

	// Look in standard ArgoCD app locations
	searchPaths := []string{
		filepath.Join(rootPath, "argocd-apps"),
	}

	for _, searchPath := range searchPaths {
		if _, err := os.Stat(searchPath); os.IsNotExist(err) {
			continue
		}

		err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
				return nil
			}
			// Skip kustomization.yaml files
			if info.Name() == "kustomization.yaml" || info.Name() == "kustomization.yml" {
				return nil
			}
			apps = append(apps, path)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to walk %s: %w", searchPath, err)
		}
	}

	return apps, nil
}

// DiscoverHelmApplications finds all Applications that use Helm charts
func DiscoverHelmApplications(rootPath string) ([]*Application, error) {
	appFiles, err := DiscoverApplications(rootPath)
	if err != nil {
		return nil, err
	}

	var helmApps []*Application
	for _, path := range appFiles {
		app, err := ParseApplicationFile(path)
		if err != nil {
			// Skip files that aren't valid Applications
			continue
		}

		// Check if this app has Helm sources
		if len(app.GetHelmSources()) > 0 {
			helmApps = append(helmApps, app)
		}
	}

	return helmApps, nil
}

// ResolveValueFiles resolves $values/ references in valueFiles to local paths
// Example: $values/apps/krr/base/values.yaml -> apps/krr/base/values.yaml
func ResolveValueFiles(valueFiles []string, repoPath string) ([]string, error) {
	var resolved []string

	for _, vf := range valueFiles {
		// Handle $values/ prefix
		if strings.HasPrefix(vf, "$values/") {
			localPath := strings.TrimPrefix(vf, "$values/")
			fullPath := filepath.Join(repoPath, localPath)

			// Verify file exists
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				return nil, fmt.Errorf("value file not found: %s (resolved from %s)", fullPath, vf)
			}

			resolved = append(resolved, fullPath)
		} else {
			// Non-$values paths might be relative or absolute
			resolved = append(resolved, vf)
		}
	}

	return resolved, nil
}

// GetKustomizePathsFromApp extracts kustomize paths from an Application
// Returns paths relative to repo root
func GetKustomizePathsFromApp(app *Application) []string {
	var paths []string
	for _, source := range app.GetKustomizeSources() {
		if source.Path != "" {
			paths = append(paths, source.Path)
		}
	}
	return paths
}

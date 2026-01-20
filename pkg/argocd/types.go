// Package argocd provides ArgoCD Application parsing for shadow sync
package argocd

// Application represents an ArgoCD Application with its source configuration
type Application struct {
	Name      string    `yaml:"-"` // Extracted from metadata.name
	Namespace string    // Destination namespace
	Sources   []Source  // Multi-source configuration
	Source    *Source   // Single-source configuration (legacy)
}

// Source represents a single source in an ArgoCD Application
type Source struct {
	// Common fields
	RepoURL        string `yaml:"repoURL"`
	TargetRevision string `yaml:"targetRevision"`

	// For Kustomize sources
	Path string `yaml:"path"`

	// For Helm sources
	Chart string      `yaml:"chart"`
	Helm  *HelmConfig `yaml:"helm,omitempty"`

	// For Git ref sources (provides $values reference)
	Ref string `yaml:"ref"`
}

// HelmConfig contains Helm-specific configuration
type HelmConfig struct {
	ReleaseName string   `yaml:"releaseName"`
	ValueFiles  []string `yaml:"valueFiles"`  // e.g., [$values/apps/krr/base/values.yaml]
	Values      string   `yaml:"values"`      // Inline values YAML
}

// IsHelmSource returns true if this source is a Helm chart
func (s *Source) IsHelmSource() bool {
	return s.Chart != ""
}

// IsKustomizeSource returns true if this source is a Kustomize path
func (s *Source) IsKustomizeSource() bool {
	return s.Path != "" && s.Chart == ""
}

// IsRefSource returns true if this source is a Git ref (for $values)
func (s *Source) IsRefSource() bool {
	return s.Ref != ""
}

// HasMultipleSources returns true if the Application uses multi-source
func (a *Application) HasMultipleSources() bool {
	return len(a.Sources) > 0
}

// GetHelmSources returns all Helm chart sources
func (a *Application) GetHelmSources() []Source {
	var helmSources []Source
	for _, s := range a.Sources {
		if s.IsHelmSource() {
			helmSources = append(helmSources, s)
		}
	}
	// Check single source
	if a.Source != nil && a.Source.IsHelmSource() {
		helmSources = append(helmSources, *a.Source)
	}
	return helmSources
}

// GetKustomizeSources returns all Kustomize path sources
func (a *Application) GetKustomizeSources() []Source {
	var kustomizeSources []Source
	for _, s := range a.Sources {
		if s.IsKustomizeSource() {
			kustomizeSources = append(kustomizeSources, s)
		}
	}
	// Check single source
	if a.Source != nil && a.Source.IsKustomizeSource() {
		kustomizeSources = append(kustomizeSources, *a.Source)
	}
	return kustomizeSources
}

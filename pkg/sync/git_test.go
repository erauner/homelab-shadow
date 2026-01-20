package sync

import (
	"testing"
)

func TestGitURLFromSlug(t *testing.T) {
	tests := []struct {
		name     string
		slug     string
		expected string
	}{
		{
			name:     "simple slug",
			slug:     "owner/repo",
			expected: "https://github.com/owner/repo.git",
		},
		{
			name:     "slug with hyphens",
			slug:     "my-org/my-repo-name",
			expected: "https://github.com/my-org/my-repo-name.git",
		},
		{
			name:     "already https URL",
			slug:     "https://github.com/owner/repo.git",
			expected: "https://github.com/owner/repo.git",
		},
		{
			name:     "https URL without .git",
			slug:     "https://github.com/owner/repo",
			expected: "https://github.com/owner/repo",
		},
		{
			name:     "ssh URL",
			slug:     "git@github.com:owner/repo.git",
			expected: "git@github.com:owner/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GitURLFromSlug(tt.slug)
			if result != tt.expected {
				t.Errorf("GitURLFromSlug(%q) = %q, want %q", tt.slug, result, tt.expected)
			}
		})
	}
}

func TestParseRepoSlug(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "https URL with .git",
			input: "https://github.com/owner/repo.git",
			want:  "owner/repo",
		},
		{
			name:  "https URL without .git",
			input: "https://github.com/owner/repo",
			want:  "owner/repo",
		},
		{
			name:  "ssh URL",
			input: "git@github.com:owner/repo.git",
			want:  "owner/repo",
		},
		{
			name:  "ssh URL without .git",
			input: "git@github.com:owner/repo",
			want:  "owner/repo",
		},
		{
			name:  "already a slug",
			input: "owner/repo",
			want:  "owner/repo",
		},
		{
			name:  "slug with hyphens and numbers",
			input: "my-org-123/my-repo-456",
			want:  "my-org-123/my-repo-456",
		},
		{
			name:    "invalid format - no slash",
			input:   "justrepo",
			wantErr: true,
		},
		{
			name:    "invalid format - empty",
			input:   "",
			wantErr: true,
		},
		{
			name:  "https with trailing slash",
			input: "https://github.com/owner/repo/",
			want:  "owner/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRepoSlug(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRepoSlug(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseRepoSlug(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInjectAuthToken(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		token    string
		expected string
	}{
		{
			name:     "no token set",
			url:      "https://github.com/owner/repo.git",
			token:    "",
			expected: "https://github.com/owner/repo.git",
		},
		{
			name:     "inject token into github https",
			url:      "https://github.com/owner/repo.git",
			token:    "ghp_test123",
			expected: "https://x-access-token:ghp_test123@github.com/owner/repo.git",
		},
		{
			name:     "non-github url unchanged",
			url:      "https://gitlab.com/owner/repo.git",
			token:    "ghp_test123",
			expected: "https://gitlab.com/owner/repo.git",
		},
		{
			name:     "ssh url unchanged",
			url:      "git@github.com:owner/repo.git",
			token:    "ghp_test123",
			expected: "git@github.com:owner/repo.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set/unset GH_TOKEN for test
			if tt.token != "" {
				t.Setenv("GH_TOKEN", tt.token)
			}

			result := injectAuthToken(tt.url)
			if result != tt.expected {
				t.Errorf("injectAuthToken(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}

func TestCompareURL(t *testing.T) {
	tests := []struct {
		name       string
		repo       string
		baseBranch string
		branch     string
		expected   string
	}{
		{
			name:       "simple compare",
			repo:       "owner/repo",
			baseBranch: "main",
			branch:     "pr-123",
			expected:   "https://github.com/owner/repo/compare/main...pr-123",
		},
		{
			name:       "master base branch",
			repo:       "org/project",
			baseBranch: "master",
			branch:     "feature-branch",
			expected:   "https://github.com/org/project/compare/master...feature-branch",
		},
		{
			name:       "same branch",
			repo:       "owner/repo",
			baseBranch: "main",
			branch:     "main",
			expected:   "https://github.com/owner/repo/compare/main...main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareURL(tt.repo, tt.baseBranch, tt.branch)
			if result != tt.expected {
				t.Errorf("CompareURL(%q, %q, %q) = %q, want %q",
					tt.repo, tt.baseBranch, tt.branch, result, tt.expected)
			}
		})
	}
}

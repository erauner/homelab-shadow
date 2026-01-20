package sync

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantRedacted bool   // should contain REDACTED
		wantPreserve []string // strings that should be preserved
		wantRemove   []string // strings that should be removed
	}{
		{
			name: "basic secret with data",
			input: `apiVersion: v1
kind: Secret
metadata:
  name: my-secret
  namespace: default
type: Opaque
data:
  password: cGFzc3dvcmQxMjM=
  username: YWRtaW4=
`,
			wantRedacted: true,
			wantPreserve: []string{"my-secret", "default", "Opaque"},
			wantRemove:   []string{"cGFzc3dvcmQxMjM=", "YWRtaW4="},
		},
		{
			name: "secret with stringData",
			input: `apiVersion: v1
kind: Secret
metadata:
  name: plaintext-secret
type: Opaque
stringData:
  api-key: super-secret-key-12345
  token: another-secret-token
`,
			wantRedacted: true,
			wantPreserve: []string{"plaintext-secret", "Opaque"},
			wantRemove:   []string{"super-secret-key-12345", "another-secret-token"},
		},
		{
			name: "non-secret resource unchanged",
			input: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: app
        image: nginx:latest
`,
			wantRedacted: false,
			wantPreserve: []string{"my-app", "replicas: 3", "nginx:latest"},
		},
		{
			name: "configmap unchanged",
			input: `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  config.yaml: |
    key: value
    setting: enabled
`,
			wantRedacted: false,
			wantPreserve: []string{"my-config", "config.yaml", "key: value", "setting: enabled"},
		},
		{
			name: "multi-document with mixed resources",
			input: `apiVersion: v1
kind: ConfigMap
metadata:
  name: config
data:
  key: value
---
apiVersion: v1
kind: Secret
metadata:
  name: secret
data:
  password: c2VjcmV0
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
`,
			wantRedacted: true,
			wantPreserve: []string{"name: config", "key: value", "name: app"},
			wantRemove:   []string{"c2VjcmV0"},
		},
		{
			name: "secret with binaryData",
			input: `apiVersion: v1
kind: Secret
metadata:
  name: binary-secret
type: Opaque
binaryData:
  cert.pem: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t
`,
			wantRedacted: true,
			wantPreserve: []string{"binary-secret"},
			wantRemove:   []string{"LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t"},
		},
		{
			name: "empty input",
			input: "",
			wantRedacted: false,
		},
		{
			name: "secret with no data fields",
			input: `apiVersion: v1
kind: Secret
metadata:
  name: empty-secret
type: Opaque
`,
			wantRedacted: false,
			wantPreserve: []string{"empty-secret", "Opaque"},
		},
		{
			name: "tls secret type",
			input: `apiVersion: v1
kind: Secret
metadata:
  name: tls-secret
type: kubernetes.io/tls
data:
  tls.crt: LS0tLS1CRUdJTi...
  tls.key: LS0tLS1CRUdJTi...
`,
			wantRedacted: true,
			wantPreserve: []string{"tls-secret", "kubernetes.io/tls"},
			wantRemove:   []string{"LS0tLS1CRUdJTi..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactSecrets(tt.input)

			// Check if REDACTED appears when expected
			hasRedacted := strings.Contains(result, "REDACTED")
			if hasRedacted != tt.wantRedacted {
				t.Errorf("RedactSecrets() REDACTED presence = %v, want %v", hasRedacted, tt.wantRedacted)
			}

			// Check preserved strings
			for _, s := range tt.wantPreserve {
				if !strings.Contains(result, s) {
					t.Errorf("RedactSecrets() should preserve %q, but it's missing", s)
				}
			}

			// Check removed strings
			for _, s := range tt.wantRemove {
				if strings.Contains(result, s) {
					t.Errorf("RedactSecrets() should remove %q, but it's still present", s)
				}
			}
		})
	}
}

func TestRedactSecrets_PreservesYAMLStructure(t *testing.T) {
	// Ensure the output is still valid YAML structure
	input := `apiVersion: v1
kind: Secret
metadata:
  name: test
  labels:
    app: myapp
data:
  key: dmFsdWU=
`
	result := RedactSecrets(input)

	// Should still have proper YAML structure
	if !strings.Contains(result, "apiVersion: v1") {
		t.Error("Should preserve apiVersion")
	}
	if !strings.Contains(result, "kind: Secret") {
		t.Error("Should preserve kind")
	}
	if !strings.Contains(result, "name: test") {
		t.Error("Should preserve metadata")
	}
	if !strings.Contains(result, "app: myapp") {
		t.Error("Should preserve labels")
	}
}

func TestRedactSecrets_DocumentSeparatorOnOwnLine(t *testing.T) {
	// Regression test for issue #1073: YAML document separator should be on its own line
	input := `apiVersion: v1
kind: ConfigMap
metadata:
  name: config
  namespace: management
data:
  key: value
---
apiVersion: v1
kind: Secret
metadata:
  name: secret
data:
  password: c2VjcmV0
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app`

	result := RedactSecrets(input)

	// The bug was: "namespace: management---" instead of "namespace: management\n---"
	if strings.Contains(result, "management---") {
		t.Error("Document separator should be on its own line, not appended to previous value")
	}
	if strings.Contains(result, "value---") {
		t.Error("Document separator should be on its own line, not appended to previous value")
	}

	// Verify separators are properly formatted (on their own lines)
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			continue // Good - separator on its own line
		}
		if strings.Contains(line, "---") && trimmed != "---" && !strings.HasPrefix(trimmed, "#") {
			t.Errorf("Line %d has embedded separator: %q", i+1, line)
		}
	}
}

func TestRedactSecrets_HandlesEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "only whitespace",
			input: "   \n\n   ",
		},
		{
			name:  "yaml with no kind",
			input: "foo: bar\nbaz: qux",
		},
		{
			name:  "malformed yaml",
			input: "this: is: not: valid: yaml:",
		},
		{
			name: "secret-like but not a secret",
			input: `apiVersion: v1
kind: ConfigMap
metadata:
  name: looks-like-secret
data:
  # This data field should NOT be redacted
  password: not-actually-a-secret
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			result := RedactSecrets(tt.input)

			// For ConfigMap, data should be preserved
			if strings.Contains(tt.input, "kind: ConfigMap") {
				if !strings.Contains(result, "not-actually-a-secret") {
					t.Error("ConfigMap data should not be redacted")
				}
			}
		})
	}
}

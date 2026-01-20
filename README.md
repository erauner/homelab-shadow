# Shadow

GitOps manifest sync tool for preview diffs. Shadow renders Kubernetes manifests from kustomize overlays and Helm charts, then pushes them to a shadow repository for comparison.

## Installation

### Via Go Install (Recommended)

```bash
# Install latest stable version via Athens proxy
GOPROXY=https://athens.erauner.dev,direct \
GONOSUMDB=github.com/erauner/* \
go install github.com/erauner/homelab-shadow/cmd/shadow@latest

# Or install a specific version
go install github.com/erauner/homelab-shadow/cmd/shadow@v0.1.0
```

### Build from Source

```bash
git clone https://github.com/erauner/homelab-shadow.git
cd homelab-shadow
go build -o shadow ./cmd/shadow
```

## Usage

### Validate GitOps Structure

```bash
# Validate cluster configuration
shadow validate --repo /path/to/homelab-k8s

# Validate specific clusters
shadow validate --repo /path/to/homelab-k8s --cluster erauner-home
```

### Sync to Shadow Repository

```bash
# Sync PR changes (creates pr-<id> branch in shadow repo)
shadow sync --shadow-repo erauner/homelab-k8s-shadow --pr 950

# Sync main branch
shadow sync --shadow-repo erauner/homelab-k8s-shadow --branch main
```

### List Discovered Resources

```bash
# List discovered ArgoCD Applications
shadow list --repo /path/to/homelab-k8s

# JSON output
shadow list --repo /path/to/homelab-k8s --output json
```

### Helm Chart Debugging

```bash
# List all Helm applications
shadow helm list

# Test Helm chart rendering
shadow helm test jenkins
shadow helm test --retries 3
```

## Features

- **Kustomize Rendering**: Renders all kustomize overlays with SOPS decryption support
- **Helm Chart Rendering**: Renders Helm charts from ArgoCD Applications (including multi-source)
- **Secret Redaction**: Automatically redacts sensitive data in rendered manifests
- **Multi-Cluster Support**: Discovers and renders manifests for multiple clusters
- **OCI Registry Support**: Handles both traditional and OCI Helm registries
- **ArgoCD Integration**: Parses ArgoCD Application manifests for Helm configurations
- **Stale Branch Cleanup**: Automatically cleans up merged PR branches from shadow repo

## Environment Variables

| Variable | Description |
|----------|-------------|
| `SOPS_AGE_KEY` | Age key for SOPS secret decryption |
| `GH_TOKEN` | GitHub token for API access (cleanup, PR operations) |
| `HELM_CACHE_HOME` | Helm cache directory |

## Development

```bash
# Run tests
go test -v ./...

# Build
go build -o shadow ./cmd/shadow

# Lint
go vet ./...
```

## CI/CD

This repo uses Jenkins for CI:
- **On push to main**: Auto-creates pre-release versions (vX.Y.Z-rc.N)
- **Promote job**: Promotes pre-releases to stable versions

Releases are available via Athens proxy at `https://athens.erauner.dev`.

## License

MIT

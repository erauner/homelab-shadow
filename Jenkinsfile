#!/usr/bin/env groovy
/**
 * Jenkinsfile - homelab-shadow CI
 *
 * Tests the shadow GitOps sync tool on every push.
 * On main branch: auto-creates pre-release versions (vX.Y.Z-rc.N)
 * Pre-releases can be promoted to stable via Jenkinsfile.promote
 */

pipeline {
    agent {
        kubernetes {
            yaml '''
apiVersion: v1
kind: Pod
metadata:
  labels:
    workload-type: ci-builds
spec:
  containers:
  - name: jnlp
    image: jenkins/inbound-agent:3355.v388858a_47b_33-3-jdk21
    resources:
      requests:
        cpu: 100m
        memory: 256Mi
      limits:
        cpu: 500m
        memory: 512Mi
  - name: golang
    image: golang:1.25-alpine
    command: ['sleep', '3600']
    resources:
      requests:
        cpu: 200m
        memory: 512Mi
      limits:
        cpu: 1000m
        memory: 1Gi
    env:
    - name: GOPROXY
      value: "https://athens.erauner.dev,direct"
    - name: GONOSUMDB
      value: "github.com/erauner/*"
    - name: GOFLAGS
      value: "-buildvcs=false"
  - name: release
    image: alpine/git:latest
    command: ['sleep', '3600']
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: 200m
        memory: 256Mi
'''
        }
    }

    environment {
        ATHENS_URL = 'https://athens.erauner.dev'
        MODULE_PATH = 'github.com/erauner/homelab-shadow'
    }

    stages {
        stage('Checkout') {
            steps {
                checkout scm
            }
        }

        stage('Test') {
            steps {
                container('golang') {
                    sh 'go version'
                    sh 'go mod download'
                    sh 'go vet ./...'
                    sh 'go test -v ./...'
                }
            }
        }

        stage('Build Check') {
            steps {
                container('golang') {
                    sh 'go build -o shadow ./cmd/shadow'
                    sh './shadow version || echo "Version command not implemented yet"'
                    echo "Binary builds successfully"
                }
            }
        }

        stage('Create Pre-release') {
            when {
                branch 'main'
            }
            steps {
                container('release') {
                    withCredentials([usernamePassword(
                        credentialsId: 'github-app',
                        usernameVariable: 'GIT_USER',
                        passwordVariable: 'GIT_TOKEN'
                    )]) {
                        script {
                            // Install tools
                            sh 'apk add --no-cache curl jq'

                            // Fix git safe directory issue
                            sh 'git config --global --add safe.directory "*"'

                            // Set remote URL with credentials for fetching
                            sh "git remote set-url origin https://\${GIT_USER}:\${GIT_TOKEN}@github.com/erauner/homelab-shadow.git"

                            // Fetch all tags
                            sh 'git fetch --tags'

                            // Get latest stable version
                            def latestStable = sh(
                                script: "git tag -l 'v*' --sort=-v:refname | grep -v 'rc' | head -1",
                                returnStdout: true
                            ).trim()

                            if (!latestStable) {
                                latestStable = 'v0.0.0'
                            }

                            // Calculate next minor version for pre-releases
                            def stableVersion = latestStable.replaceFirst('v', '')
                            def parts = stableVersion.tokenize('.')
                            def major = parts[0] as int
                            def minor = (parts[1] as int) + 1
                            def nextVersion = "v${major}.${minor}.0"

                            // Count existing rc tags for this version
                            def rcCount = sh(
                                script: "git tag -l '${nextVersion}-rc.*' | wc -l",
                                returnStdout: true
                            ).trim() as int

                            def rcNumber = rcCount + 1
                            env.PRERELEASE_VERSION = "${nextVersion}-rc.${rcNumber}"

                            echo "Creating pre-release: ${env.PRERELEASE_VERSION}"

                            // Configure git
                            sh """
                                git config user.email "jenkins@erauner.dev"
                                git config user.name "Jenkins CI"
                            """

                            // Create and push tag
                            sh """
                                git tag -a ${env.PRERELEASE_VERSION} -m "Pre-release ${env.PRERELEASE_VERSION}"
                                git remote set-url origin https://\${GIT_USER}:\${GIT_TOKEN}@github.com/erauner/homelab-shadow.git
                                git push origin ${env.PRERELEASE_VERSION}
                            """

                            // Create GitHub pre-release
                            def commitSha = sh(script: 'git rev-parse HEAD', returnStdout: true).trim()
                            def shortSha = commitSha.take(7)

                            def releaseBody = "## Pre-release ${env.PRERELEASE_VERSION}\\n\\n"
                            releaseBody += "**Commit:** ${shortSha}\\n\\n"
                            releaseBody += "### Installation\\n\\n"
                            releaseBody += "\\`\\`\\`bash\\ngo install github.com/erauner/homelab-shadow/cmd/shadow@${env.PRERELEASE_VERSION}\\n\\`\\`\\`\\n\\n"
                            releaseBody += "To promote to stable, use the **Promote Release** job in Jenkins."

                            def releasePayload = """{
                                "tag_name": "${env.PRERELEASE_VERSION}",
                                "name": "${env.PRERELEASE_VERSION}",
                                "body": "${releaseBody}",
                                "draft": false,
                                "prerelease": true
                            }"""

                            writeFile file: 'release-payload.json', text: releasePayload

                            // Create GitHub release
                            def releaseResponse = sh(
                                script: """
                                    curl -s -X POST \\
                                        -H "Authorization: token \${GIT_TOKEN}" \\
                                        -H "Accept: application/vnd.github.v3+json" \\
                                        -d @release-payload.json \\
                                        "https://api.github.com/repos/erauner/homelab-shadow/releases"
                                """,
                                returnStdout: true
                            ).trim()
                            echo "GitHub API Response: ${releaseResponse}"

                            echo "Pre-release created: https://github.com/erauner/homelab-shadow/releases/tag/${env.PRERELEASE_VERSION}"
                        }
                    }
                }
            }
        }

        stage('Warm Athens Cache') {
            when {
                branch 'main'
            }
            steps {
                container('release') {
                    script {
                        def athensUrl = "${env.ATHENS_URL}/${env.MODULE_PATH}/@v/${env.PRERELEASE_VERSION}.info"
                        echo "Warming Athens cache: ${athensUrl}"

                        sh """
                            curl -sf '${athensUrl}' || sleep 5 && curl -sf '${athensUrl}' || true
                        """
                    }
                }
            }
        }
    }

    post {
        success {
            script {
                if (env.PRERELEASE_VERSION) {
                    echo """
                    Pre-release ${env.PRERELEASE_VERSION} created!

                    To install: go install ${env.MODULE_PATH}/cmd/shadow@${env.PRERELEASE_VERSION}
                    To promote: Use 'Promote Release' job in Jenkins

                    GitHub: https://github.com/erauner/homelab-shadow/releases
                    Athens: ${env.ATHENS_URL}/${env.MODULE_PATH}/@v/list
                    """
                } else {
                    echo 'All tests passed!'
                }
            }
        }
        failure {
            echo 'Build failed - check the logs'
        }
    }
}

#!/usr/bin/env groovy
/**
 * Jenkinsfile - homelab-shadow CI
 *
 * Tests the shadow GitOps sync tool on every push.
 * On main branch: auto-creates pre-release versions (vX.Y.Z-rc.N)
 * Pre-releases can be promoted to stable via Jenkinsfile.promote
 */

@Library('homelab') _

pipeline {
    agent {
        kubernetes {
            yaml homelab.podTemplate('golang')
        }
    }

    options {
        buildDiscarder(logRotator(numToKeepStr: '10'))
        timeout(time: 15, unit: 'MINUTES')
        disableConcurrentBuilds()
    }

    environment {
        ATHENS_URL = 'https://athens.erauner.dev'
        MODULE_PATH = 'github.com/erauner/homelab-shadow'
    }

    stages {
        stage('Test') {
            steps {
                container('golang') {
                    sh '''
                        go version
                        go mod download
                        go vet ./...
                        go test -v ./...
                    '''
                }
            }
        }

        stage('Build Check') {
            steps {
                container('golang') {
                    sh '''
                        go build -o shadow ./cmd/shadow
                        ./shadow version || echo "Version command not implemented yet"
                    '''
                    echo "Binary builds successfully"
                }
            }
        }

        stage('Create Pre-release') {
            when {
                expression { env.BRANCH_NAME == 'main' }
            }
            steps {
                container('golang') {
                    withCredentials([usernamePassword(
                        credentialsId: 'github-app',
                        usernameVariable: 'GIT_USER',
                        passwordVariable: 'GIT_TOKEN'
                    )]) {
                        script {
                            // Install release tools (alpine-based Go image)
                            sh 'apk add --no-cache git curl jq'

                            // Use shared library for release creation
                            def result = homelab.createPreRelease([
                                repo: 'erauner/homelab-shadow'
                            ])
                            env.PRERELEASE_VERSION = result.version
                        }
                    }
                }
            }
        }

        stage('Warm Athens Cache') {
            when {
                expression { env.BRANCH_NAME == 'main' }
            }
            steps {
                container('golang') {
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

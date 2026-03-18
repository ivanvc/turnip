# Implementation Tasks: Multi-IaC Automation Platform

## Phase 1: Core Infrastructure Setup

### 1.1 Project Structure and Dependencies
- [ ] Initialize Go module with required dependencies (gRPC, Redis client, Kubernetes client, GitHub API client)
- [ ] Set up project directory structure (cmd/server, cmd/runner, internal/plugin, internal/lock, internal/github, internal/config)
- [ ] Configure build system and Dockerfiles for server and runner images
- [ ] Set up CI/CD pipeline configuration

### 1.2 Configuration Parsing
- [ ] Implement turnip.yaml schema definition
- [ ] Implement YAML parser for turnip.yaml
- [ ] Implement project configuration validation (tool types, required fields)
- [ ] Add support for tool-specific config fields (workspace, stack, environment)
- [ ] Write unit tests for configuration parsing
- [ ] Write property test for configuration round-trip (Property 1)
- [ ] Write property test for tool validation (Property 2)

### 1.3 Project Matching
- [ ] Implement glob pattern matching for whenModified rules
- [ ] Implement project matcher that evaluates patterns against file lists
- [ ] Handle edge case: no projects match modified files
- [ ] Write unit tests for pattern matching edge cases
- [ ] Write property test for whenModified pattern matching (Property 3)

## Phase 2: Lock Management

### 2.1 Redis Lock Manager
- [ ] Implement LockManager interface
- [ ] Implement Redis-based lock acquisition using SET NX (no TTL)
- [ ] Implement lock acquisition with PR number and URL tracking
- [ ] Implement plan data storage in locks
- [ ] Implement plan data retrieval from locks with PR verification
- [ ] Implement lock release
- [ ] Implement lock status checking (which PR holds the lock)
- [ ] Write unit tests for lock operations
- [ ] Write property test for lock acquisition preventing concurrent operations from different PRs (Property 9)
- [ ] Write property test for lock persistence across plan-apply (Property 11)

### 2.2 Lock Lifecycle Management
- [ ] Implement PR merge/close webhook handler
- [ ] Implement automatic lock release on PR merge
- [ ] Implement automatic lock release on PR close
- [ ] Implement manual unlock via comment ("/turnip unlock")
- [ ] Implement authorization check for manual unlock
- [ ] Add logging for all lock lifecycle events
- [ ] Write unit tests for lock lifecycle

### 2.3 Lock UI and Monitoring
- [ ] Implement web UI to view all active locks
- [ ] Implement lock detail view showing PR, project, and plan summary
- [ ] Implement admin API to force-unlock stale locks
- [ ] Add metrics for lock duration and contention
- [ ] Write unit tests for lock UI endpoints

## Phase 3: GitHub Integration

### 3.1 GitHub Client
- [ ] Implement GitHubClient interface
- [ ] Implement GitHub App authentication with private key
- [ ] Implement installation token generation
- [ ] Implement file fetching from repository
- [ ] Implement modified files retrieval for PRs
- [ ] Implement check run creation and updates
- [ ] Implement PR comment posting and updating
- [ ] Implement collaborator permission checking
- [ ] Write unit tests with mocked GitHub API
- [ ] Write property test for token generation per webhook (Property 26)

### 3.2 Check Run Management
- [ ] Implement check run creation for each project operation
- [ ] Implement check run status updates (in_progress, completed)
- [ ] Implement change summary formatting in check run output
- [ ] Handle check run creation failures gracefully
- [ ] Write unit tests for check run lifecycle
- [ ] Write property test for check run creation per project (Property 12)
- [ ] Write property test for check run status reflection (Property 13)
- [ ] Write property test for check run change summary (Property 14)

### 3.3 Comment Management
- [ ] Implement consolidated comment generation from multiple project results
- [ ] Implement comment formatting with markdown tables and collapsible sections
- [ ] Implement comment update logic to avoid duplication
- [ ] Implement syntax highlighting for tool output
- [ ] Handle GitHub comment length limits (split into multiple comments if needed)
- [ ] Write unit tests for comment formatting
- [ ] Write property test for consolidated comment per PR (Property 15)
- [ ] Write property test for comment update not duplication (Property 16)
- [ ] Write property test for comment containing all project results (Property 17)

### 3.4 Comment Parser
- [ ] Implement CommentParser interface
- [ ] Implement trigger pattern recognition (/{tool} {operation})
- [ ] Implement project name extraction from comments
- [ ] Implement extra arguments parsing (after -- delimiter)
- [ ] Write unit tests for various comment formats
- [ ] Write property test for comment trigger pattern recognition (Property 7)
- [ ] Write property test for selective project triggering (Property 8)

### 3.5 Authorization
- [ ] Implement collaborator verification for trigger comments
- [ ] Implement permission level checking (write required for apply/destroy)
- [ ] Implement authorization error responses
- [ ] Add logging for authorization checks
- [ ] Write unit tests for authorization logic
- [ ] Write property test for comment author authorization (Property 28)
- [ ] Write property test for write permission requirement (Property 29)

## Phase 4: Plugin System

### 4.1 Plugin Interface
- [ ] Define unified Plugin interface (GetOperations, Execute, Name, GetPlanOperation, GetApplyOperation methods)
- [ ] Define ExecuteOptions struct
- [ ] Define ExecuteResult struct
- [ ] Define ChangeSummary struct
- [ ] Write property test for plugin result structure completeness (Property 4)

### 4.2 Terraform Plugin
- [ ] Implement Terraform plugin with Plugin interface
- [ ] Implement GetOperations to return ["plan", "apply"]
- [ ] Implement GetPlanOperation to return "plan"
- [ ] Implement GetApplyOperation to return "apply"
- [ ] Implement Execute method for "plan" operation (terraform init + terraform plan -out=<plan file>)
- [ ] Implement Execute method for "apply" operation (terraform init + terraform apply <plan file from lock>)
- [ ] Implement support for -destroy flag in plan operations via extra arguments
- [ ] Implement terraform output parsing for change counts
- [ ] Implement workspace configuration support
- [ ] Write unit tests for Terraform plugin
- [ ] Write property test for Terraform command execution (Property 18)
- [ ] Write property test for Terraform change count parsing (Property 19)

### 4.3 Pulumi Plugin
- [ ] Implement Pulumi plugin with Plugin interface
- [ ] Implement GetOperations to return ["preview", "up"]
- [ ] Implement GetPlanOperation to return "preview"
- [ ] Implement GetApplyOperation to return "up"
- [ ] Implement Execute method for "preview" operation (pulumi preview)
- [ ] Implement Execute method for "up" operation (pulumi up --yes)
- [ ] Implement support for destroy via "up" operation with --destroy flag in extra arguments
- [ ] Implement pulumi output parsing for change counts
- [ ] Implement stack configuration support
- [ ] Write unit tests for Pulumi plugin
- [ ] Write property test for Pulumi command execution (Property 20)
- [ ] Write property test for Pulumi change count parsing (Property 21)

### 4.4 Helmfile Plugin
- [ ] Implement Helmfile plugin with Plugin interface
- [ ] Implement GetOperations to return ["diff", "apply", "sync"]
- [ ] Implement GetPlanOperation to return "diff"
- [ ] Implement GetApplyOperation to return "apply"
- [ ] Implement Execute method for "diff" operation (helmfile diff)
- [ ] Implement Execute method for "apply" operation (helmfile apply)
- [ ] Implement Execute method for "sync" operation (helmfile sync)
- [ ] Implement support for destroy via helmfile destroy when explicitly requested
- [ ] Implement helmfile output parsing for changed releases
- [ ] Implement environment configuration support
- [ ] Write unit tests for Helmfile plugin
- [ ] Write property test for Helmfile command execution (Property 22)

## Phase 5: gRPC Communication

### 5.1 Protocol Definition
- [ ] Define gRPC service in protobuf (OperationService)
- [ ] Define OperationRequest message
- [ ] Define OperationResponse message with streaming
- [ ] Define LogLine and OperationResult messages
- [ ] Generate Go code from protobuf definitions

### 5.2 Server-Side gRPC
- [ ] Implement gRPC server in server process
- [ ] Implement ExecuteOperation RPC handler
- [ ] Implement log streaming from runner to server
- [ ] Implement operation result handling
- [ ] Handle gRPC connection failures
- [ ] Write unit tests for gRPC server

### 5.3 Runner-Side gRPC
- [ ] Implement gRPC client in runner process
- [ ] Implement connection to server on runner startup
- [ ] Implement operation execution via gRPC call
- [ ] Implement log streaming to server
- [ ] Handle connection timeouts (2 minute limit)
- [ ] Write unit tests for gRPC client

## Phase 6: Server Implementation

### 6.1 Webhook Handler
- [ ] Implement HTTP server for GitHub webhooks
- [ ] Implement webhook signature verification
- [ ] Implement webhook event parsing (pull_request, issue_comment)
- [ ] Implement webhook routing to appropriate handlers
- [ ] Write unit tests for webhook handling

### 6.2 PR Event Handler
- [ ] Implement handler for PR opened events
- [ ] Implement handler for PR synchronized events
- [ ] Implement handler for PR closed events (release locks)
- [ ] Implement handler for PR merged events (release locks)
- [ ] Implement turnip.yaml fetching and parsing
- [ ] Implement modified files retrieval
- [ ] Implement project matching
- [ ] Implement operation orchestration for matched projects
- [ ] Implement lock acquisition with PR tracking
- [ ] Write unit tests for PR event handling
- [ ] Write property test for PR event triggering plan operations (Property 5)

### 6.3 Comment Event Handler
- [ ] Implement handler for issue_comment created events
- [ ] Implement comment parsing for triggers
- [ ] Implement authorization checking
- [ ] Implement project selection (all or specific)
- [ ] Implement operation orchestration for triggered projects
- [ ] Write unit tests for comment event handling

### 6.4 Operation Orchestration
- [ ] Implement parallel operation execution for multiple projects
- [ ] Implement lock acquisition before operations
- [ ] Implement runner job creation
- [ ] Implement operation result collection
- [ ] Implement lock release after operations
- [ ] Implement consolidated result generation
- [ ] Write unit tests for orchestration logic
- [ ] Write property test for runner creation per project (Property 6)
- [ ] Write property test for parallel project execution (Property 30)
- [ ] Write property test for synchronization before commenting (Property 31)
- [ ] Write property test for failure propagation (Property 32)

## Phase 7: Runner Implementation

### 7.1 Runner Initialization
- [ ] Implement runner startup and gRPC connection
- [ ] Implement environment variable parsing (repo URL, commit SHA, project dir, operation type)
- [ ] Implement repository cloning at specified commit
- [ ] Handle clone failures gracefully
- [ ] Write unit tests for runner initialization
- [ ] Write property test for runner cloning correct commit (Property 24)

### 7.2 Operation Execution
- [ ] Implement plugin selection based on tool type
- [ ] Implement plugin operation invocation via Execute method
- [ ] Implement log streaming to server
- [ ] Implement result reporting to server
- [ ] Handle plugin execution errors
- [ ] Write unit tests for operation execution

### 7.3 Runner Cleanup
- [ ] Implement graceful shutdown on operation completion
- [ ] Implement cleanup of cloned repository
- [ ] Implement error reporting on failures
- [ ] Write unit tests for cleanup logic

## Phase 8: Kubernetes Integration

### 8.1 Job Management
- [ ] Implement Kubernetes client initialization
- [ ] Implement job creation for runner pods
- [ ] Implement job specification with environment variables
- [ ] Implement job status monitoring
- [ ] Implement job deletion after completion
- [ ] Handle job creation failures
- [ ] Write unit tests with mocked Kubernetes API
- [ ] Write property test for runner job environment variables (Property 23)
- [ ] Write property test for job cleanup after completion (Property 25)

### 8.2 Job Cleanup
- [ ] Implement periodic cleanup task for orphaned jobs
- [ ] Implement job timeout detection (5 minute startup, 30 minute execution)
- [ ] Implement job deletion for completed/failed jobs older than 1 hour
- [ ] Write unit tests for cleanup logic

## Phase 9: Error Handling and Resilience

### 9.1 Configuration Error Handling
- [ ] Implement error comment posting for missing turnip.yaml
- [ ] Implement error comment posting for invalid YAML syntax
- [ ] Implement error comment posting for invalid project configuration
- [ ] Write unit tests for configuration error scenarios

### 9.2 GitHub API Error Handling
- [ ] Implement retry logic with exponential backoff for API calls
- [ ] Implement rate limit detection and handling
- [ ] Implement fallback to comment-only reporting if check runs fail
- [ ] Write unit tests for API error scenarios

### 9.3 Runner Error Handling
- [ ] Implement timeout detection for job startup
- [ ] Implement error reporting for gRPC connection failures
- [ ] Implement error reporting for repository clone failures
- [ ] Implement error reporting for plugin execution failures
- [ ] Write unit tests for runner error scenarios

### 9.4 Authorization Error Handling
- [ ] Implement error response for non-collaborator triggers
- [ ] Implement error response for insufficient permissions
- [ ] Implement logging for authorization failures
- [ ] Write unit tests for authorization error scenarios

## Phase 10: Tool-Specific Configuration

### 10.1 Config Propagation
- [ ] Implement config extraction from turnip.yaml projects
- [ ] Implement config passing to plugins via operation options
- [ ] Implement Terraform workspace configuration
- [ ] Implement Pulumi stack configuration
- [ ] Implement Helmfile environment configuration
- [ ] Write unit tests for config propagation
- [ ] Write property test for tool-specific config propagation (Property 33)

## Phase 11: Integration Testing

### 11.1 End-to-End Tests
- [ ] Set up test environment with kind (Kubernetes) and miniredis
- [ ] Write test for complete PR workflow (webhook → plan → comment → apply)
- [ ] Write test for multi-project parallel execution
- [ ] Write test for lock coordination under concurrent load
- [ ] Write test for comment-triggered operations
- [ ] Write test for authorization checks

### 11.2 GitHub Integration Tests
- [ ] Set up test GitHub repository
- [ ] Write test for real GitHub API interactions
- [ ] Write test for webhook delivery and processing
- [ ] Write test for check run creation and updates
- [ ] Write test for comment posting and updating

### 11.3 High Availability Tests
- [ ] Write test for multiple Server instances processing webhooks concurrently
- [ ] Write test for lock acquisition race conditions between Server instances
- [ ] Write test for Server instance failure during operation
- [ ] Write test for webhook processing by different Server instances for same project
- [ ] Verify no in-memory state dependencies between Server instances
- [ ] Write property test for stateless server operation (Property 34)
- [ ] Write property test for multi-instance webhook processing (Property 35)
- [ ] Write property test for lock acquisition across instances (Property 36)
- [ ] Write property test for server failure resilience (Property 37)

### 11.4 Performance Tests
- [ ] Write load test for 100 concurrent webhooks across multiple Server instances
- [ ] Measure lock contention and queue depth with multiple Servers
- [ ] Monitor runner pod resource usage
- [ ] Test with large repositories (>10k files)
- [ ] Verify no deadlocks or race conditions with multiple Server instances

## Phase 12: Documentation and Deployment

### 12.1 Documentation
- [ ] Write README with architecture overview
- [ ] Write deployment guide for Kubernetes
- [ ] Write configuration guide for turnip.yaml
- [ ] Write GitHub App setup guide
- [ ] Write plugin development guide
- [ ] Write troubleshooting guide

### 12.2 Deployment Artifacts
- [ ] Create Kubernetes manifests for server deployment (with multiple replicas for HA)
- [ ] Create Kubernetes manifests for runner job template
- [ ] Create Helm chart for platform deployment with HA configuration
- [ ] Create example turnip.yaml configurations
- [ ] Create GitHub App configuration template
- [ ] Document HA deployment best practices (Redis/Valkey setup, replica count, load balancing)

### 12.3 Observability
- [ ] Implement structured logging throughout platform
- [ ] Add metrics for operation counts, durations, and failures
- [ ] Add metrics for lock contention and queue depth
- [ ] Add metrics for GitHub API rate limit usage
- [ ] Create Grafana dashboard for platform monitoring

## Phase 13: Final Testing and Release

### 13.1 Final Validation
- [ ] Run full test suite (unit, property, integration)
- [ ] Verify all coverage targets met
- [ ] Run linting and static analysis
- [ ] Perform security scan of dependencies
- [ ] Test deployment on staging environment

### 13.2 Release Preparation
- [ ] Tag release version
- [ ] Generate release notes
- [ ] Build and publish Docker images
- [ ] Publish Helm chart
- [ ] Update documentation with release version

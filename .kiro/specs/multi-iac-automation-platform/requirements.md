# Requirements Document

## Introduction

This document specifies requirements for rewriting the multi-IaC automation platform (turnip). The rewrite preserves proven architectural decisions from the existing implementation while addressing architectural complexity, completing missing functionality, and adding Terraform support.

**Implementation Constraints:**
- **Language**: Go (for both Server and Runner components)
- **Version Control Platform**: GitHub (GitHub App integration, webhooks, API)
- **Note**: While initially tailored for GitHub, the architecture should allow future extension to other platforms (GitLab, Bitbucket) through abstraction of the VCS client interface

**Preserved from existing implementation:**
- Server/runner architecture with ephemeral Kubernetes pods
- Redis/Valkey-based locking to prevent concurrent operations
- gRPC communication between server and runners
- GitHub App integration
- Repository configuration via turnip.yaml
- Project-based configuration with whenModified rules for selective triggering
- Auto-plan/preview on PR open/sync

**Key improvements in rewrite:**
- Simplified plugin abstraction (eliminate overlapping Plugin/Executor/Adapter/Workflow interfaces)
- Tool-defined operation naming. For example, for terraform plan/apply, for Helmfile diff/apply/sync/etc, for Pulumi preview/up
- Complete webhook-to-operation flow with comment parsing
- GitHub status checks and PR comment posting
- Full Terraform and Helmfile support
- Comprehensive test coverage

## Glossary

- **Platform**: The complete multi-IaC automation system
- **Server**: HTTP service that receives GitHub webhooks and orchestrates runner execution via gRPC
- **Runner**: Ephemeral Kubernetes pod that executes IaC operations for a specific tool
- **Plugin**: Unified component that implements IaC tool-specific logic (plan, apply, destroy operations)
- **IaC_Tool**: Infrastructure-as-code tool (Terraform, Pulumi, or Helmfile)
- **Webhook_Event**: GitHub webhook payload containing repository and pull request information
- **Operation**: Standardized IaC action (plan, apply, or destroy) across all tools
- **Project**: Configuration unit in turnip.yaml defining a directory, IaC tool, and whenModified rules
- **WhenModified_Rule**: File path pattern that determines if a Project should be triggered
- **Lock**: Redis-based mechanism preventing concurrent operations on the same Project
- **Trigger_Comment**: PR comment matching a pattern that initiates an Operation

## Requirements

### Requirement 1: Parse turnip.yaml Configuration

**User Story:** As a developer, I want to define Projects in turnip.yaml, so that I can configure which directories contain IaC code and when they should be triggered.

#### Acceptance Criteria

1. WHEN the Server processes a Webhook_Event, THE Server SHALL read the repository's configuration from `.turnip/config.yaml`, falling back to `turnip.yaml` at the repository root; no other location and no other file extension is accepted
2. THE Server SHALL parse the projects list from turnip.yaml
3. FOR EACH Project, THE Server SHALL extract the directory, tool type, and WhenModified_Rule patterns
4. IF turnip.yaml is missing or invalid, THEN THE Server SHALL post an error comment to the PR
5. THE Server SHALL validate that each Project specifies a supported IaC_Tool (terraform, pulumi, or helmfile)

### Requirement 2: Selective Project Triggering with WhenModified Rules

**User Story:** As a developer, I want Projects to trigger only when relevant files change, so that I don't run unnecessary IaC operations.

#### Acceptance Criteria

1. WHEN a PR is opened or synchronized, THE Server SHALL retrieve the list of modified files from GitHub
2. FOR EACH Project, THE Server SHALL evaluate its WhenModified_Rule patterns against the modified files
3. IF any modified file matches a Project's WhenModified_Rule, THEN THE Server SHALL mark that Project for execution
4. THE Server SHALL support glob patterns in WhenModified_Rule definitions (e.g., "infrastructure/**/*.tf")
5. IF no Projects match the modified files, THEN THE Server SHALL skip operation execution

### Requirement 3: Unified Plugin Interface

**User Story:** As a platform developer, I want a single Plugin interface, so that the architecture is simple and consistent across all IaC tools.

#### Acceptance Criteria

1. THE Platform SHALL define one Plugin interface that all IaC tools implement
2. THE Plugin interface SHALL allow each tool to expose its native operations (e.g., Terraform: plan/apply, Helmfile: diff/apply/sync, Pulumi: preview/up)
3. THE Plugin SHALL provide methods to query supported operations and identify plan/apply operation names
4. THE Plugin SHALL return a standardized result structure containing output, exit code, and error information
5. THE Platform SHALL eliminate overlapping abstractions (no separate Executor, Adapter, or Workflow interfaces)

### Requirement 4: Automatic Plan on PR Events

**User Story:** As a developer, I want to see a plan automatically when I open or update a PR, so that I can review infrastructure changes without manual triggering.

#### Acceptance Criteria

1. WHEN a PR is opened, THE Server SHALL trigger plan Operations for all matching Projects
2. WHEN a PR is synchronized (new commits pushed), THE Server SHALL trigger plan Operations for all matching Projects (if there was a plan before for this given pull request already, could be matched by auto plan or a manual plan)
3. FOR EACH triggered Project, THE Server SHALL create a Runner pod and execute the Plugin's Plan method
4. WHEN all plan Operations complete, THE Server SHALL post a consolidated comment with results from all Projects
5. IF any plan Operation fails, THEN THE Server SHALL include the error output in the comment

### Requirement 5: Comment-Triggered Apply Operations

**User Story:** As a developer, I want to trigger apply operations via PR comments, so that I can deploy infrastructure changes after review.

#### Acceptance Criteria

1. WHEN a PR comment is created, THE Server SHALL parse the comment body for trigger patterns
2. THE Server SHALL recognize "/{tool name,turnip} apply" as the apply trigger pattern
3. WHEN "/turnip apply" or "/terraform apply" is detected with no Project named, THE Server SHALL trigger apply Operations for the Projects whose Lock this pull request holds with a plan recorded
4. WHERE a specific Project is named (e.g., "/turnip apply project-name"), THE Server SHALL trigger apply only for that Project
5. THE Server SHALL post a comment with apply results for each executed Project

### Requirement 6: Plan with Destroy Flag

**User Story:** As a developer, I want to plan destroy operations via comments, so that I can review what will be destroyed before applying.

#### Acceptance Criteria

1. THE user SHALL be able to plan with destroy flag by running "/turnip plan -- -destroy" or "/terraform plan -- -destroy"
2. THE Server SHALL pass the "-destroy" flag to the Plugin's Plan method via extra arguments
3. THE Plugin SHALL execute the tool-specific destroy plan command (e.g., "terraform plan -destroy")
4. THE user SHALL receive the detailed plan output showing resources to be destroyed
5. THE user SHALL apply the destroy plan by running "/turnip apply" or "/terraform apply" which uses the plan from the lock

### Requirement 7: Redis/Valkey-Based Locking

**User Story:** As a platform operator, I want Redis/Valkey-based locking, so that concurrent operations on the same Project are prevented and plan-apply consistency is guaranteed.

#### Acceptance Criteria

1. WHEN a plan Operation starts for a Project, THE Server SHALL attempt to acquire a Lock in Redis/Valkey using the Project identifier and PR number as the key
2. IF a Lock already exists for the Project held by a different PR, THEN THE Server SHALL reject the Operation and post a comment indicating another PR holds the lock with a link to that PR
3. WHEN a plan Operation completes successfully, THE Server SHALL store the plan result in the Lock and THE Lock SHALL remain held
4. THE Lock SHALL persist without TTL until the PR is merged, closed, manually unlocked, or a successful apply completes
5. WHEN an apply Operation is triggered, THE Server SHALL verify the Lock is held by the current PR and retrieve the plan data from the Lock
6. WHEN an apply Operation completes successfully, THE Server SHALL release the Lock
7. THE Server SHALL support manual unlock via comment ("/turnip unlock") which releases the Lock and requires re-planning before apply
8. THE Server SHALL use Redis SET NX for atomic lock acquisition to prevent race conditions

### Requirement 8: gRPC Communication Between Server and Runner

**User Story:** As a platform developer, I want gRPC communication between Server and Runner, so that operation execution is efficient and type-safe.

#### Acceptance Criteria

1. THE Server SHALL define a gRPC service with ExecuteOperation RPC method
2. WHEN the Server creates a Runner pod, THE Server SHALL pass the gRPC server address as an environment variable
3. THE Runner SHALL connect to the Server via gRPC on startup
4. THE Runner SHALL call ExecuteOperation with Project details, Operation type, and repository information
5. THE Server SHALL stream operation logs back to the Runner via gRPC response streaming

### Requirement 9: GitHub Status Checks

**User Story:** As a developer, I want to see operation status in GitHub checks, so that I know if infrastructure changes are safe to merge.

#### Acceptance Criteria

1. WHEN a plan Operation starts for a Project, THE Server SHALL create a GitHub check run with status "in_progress"
2. WHEN a plan Operation completes successfully, THE Server SHALL update the check run to "completed" with conclusion "success"
3. WHEN a plan Operation fails, THE Server SHALL update the check run to "completed" with conclusion "failure"
4. THE Server SHALL include a summary of changes (resources to add/change/destroy) in the check run output
5. THE Server SHALL create separate check runs for each Project in a multi-project repository

### Requirement 10: GitHub PR Comment Posting

**User Story:** As a developer, I want operation results posted as PR comments, so that I can see infrastructure changes without leaving GitHub.

#### Acceptance Criteria

1. WHEN plan Operations complete, THE Server SHALL post a single comment with results from all Projects
2. THE comment SHALL show each Project's name, Operation type, and status (success/failure) legibly without the reader expanding any collapsed section
3. THE comment SHALL include collapsible sections with detailed output for each Project
4. WHEN subsequent plan Operations run on the same PR, THE Server SHALL update the existing comment instead of creating a new one
5. THE Server SHALL format Terraform/Pulumi/Helmfile output with syntax highlighting using markdown code blocks
6. THE server should be aware of GitHub's comment maximum length, and generate multiple messages if it overflows the limit
### Requirement 11: Terraform Plugin Implementation

**User Story:** As a developer, I want to use Terraform, so that I can manage infrastructure with the most widely adopted IaC tool.

#### Acceptance Criteria

1. THE Platform SHALL provide a Terraform Plugin implementing the unified Plugin interface
2. THE Terraform_Plugin SHALL expose "plan" and "apply" as supported operations
3. WHEN "plan" operation is executed, THE Terraform_Plugin SHALL execute "terraform init" followed by "terraform plan -out=<plan file>" and store the plan file
4. WHEN "apply" operation is executed, THE Terraform_Plugin SHALL execute "terraform init" followed by "terraform apply <plan file from lock>"
5. THE Terraform_Plugin SHALL parse terraform plan output to extract resource change counts (add/change/destroy)
6. THE Terraform_Plugin SHALL support the -destroy flag in plan operations via extra arguments=<plan file>"
### Requirement 12: Pulumi Plugin Implementation

**User Story:** As a developer, I want to use Pulumi, so that I can manage infrastructure with a modern programming language-based IaC tool.

#### Acceptance Criteria

1. THE Platform SHALL provide a Pulumi Plugin implementing the unified Plugin interface
2. THE Pulumi_Plugin SHALL expose "preview" and "up" as supported operations
3. WHEN "preview" operation is executed, THE Pulumi_Plugin SHALL execute "pulumi preview"
4. WHEN "up" operation is executed, THE Pulumi_Plugin SHALL execute "pulumi up --yes"
5. THE Pulumi_Plugin SHALL parse pulumi preview output to extract resource change counts (create/update/delete)
6. THE Pulumi_Plugin SHALL support destroy via "up" operation with --destroy flag in extra arguments (e.g., "pulumi destroy --yes")
### Requirement 13: Helmfile Plugin Implementation

**User Story:** As a developer, I want to use Helmfile, so that I can manage Kubernetes applications declaratively.

#### Acceptance Criteria

1. THE Platform SHALL provide a Helmfile Plugin implementing the unified Plugin interface
2. THE Helmfile_Plugin SHALL expose "diff", "apply", and "sync" as supported operations
3. WHEN "diff" operation is executed, THE Helmfile_Plugin SHALL execute "helmfile diff"
4. WHEN "apply" operation is executed, THE Helmfile_Plugin SHALL execute "helmfile apply"
5. WHEN "sync" operation is executed, THE Helmfile_Plugin SHALL execute "helmfile sync"
6. THE Helmfile_Plugin SHALL parse helmfile diff output to identify changed releases
7. THE Helmfile_Plugin SHALL NOT expose destroy as an Operation — `helmfile destroy` has no dry-run and uninstalls every release its selector matches regardless of `installed:`, so it does not converge to the state `helmfile diff` describes and no plan can say what it would remove
8. WHEN Apply is called, THE Helmfile_Plugin SHALL execute "helmfile apply"
9. WHERE a trigger names destroy for a Helmfile Project, THE Server SHALL reject it as an unrecognized Operation; a release is removed instead by marking it `installed: false`, which diff reports as a pending removal and apply performs
10. THE Helmfile_Plugin SHALL parse helmfile diff output to identify changed releases

### Requirement 14: Ephemeral Runner Pod Lifecycle

**User Story:** As a platform operator, I want Runner pods to be ephemeral, so that each operation has a clean environment and resources are released after completion.

#### Acceptance Criteria

1. WHEN an Operation is triggered, THE Server SHALL create a Kubernetes Job resource for the Runner
2. THE Runner Job SHALL include the repository URL, commit SHA, Project directory, and Operation type as environment variables
2a. THE Runner Job SHALL provision the Project's IaC_Tool binary via a dedicated initContainer using that tool's vendor-published image, tagged with the resolved tool version (Requirement 18.7-18.10), copying the binary to a volume shared with the Runner's main container
3. THE Runner SHALL clone the repository at the specified commit SHA on startup
4. WHEN the Operation completes, THE Kubernetes Job SHALL terminate and THE Server SHALL delete the Job resource
5. IF the Runner Job fails to start within 5 minutes, THEN THE Server SHALL report the failure via GitHub comment and check run

### Requirement 15: GitHub App Authentication

**User Story:** As a platform operator, I want GitHub App authentication, so that the Platform can access private repositories and post comments with proper identity.

#### Acceptance Criteria

1. THE Server SHALL authenticate as a GitHub App using a private key and app ID
2. WHEN processing a Webhook_Event, THE Server SHALL generate an installation access token for the repository
3. THE Server SHALL use the installation access token for all GitHub API calls (creating check runs, posting comments, fetching files)
4. THE installation access token SHALL be passed to the Runner for repository cloning
5. IF token generation fails, THEN THE Server SHALL log the error and reject the webhook with HTTP 500

### Requirement 16: Comment Author Authorization

**User Story:** As a platform operator, I want to verify comment authors are authorized, so that only repository collaborators can trigger operations.

#### Acceptance Criteria

1. WHEN a Trigger_Comment is detected, THE Server SHALL verify the comment author is a repository collaborator via GitHub API
2. IF the author is not a collaborator, THEN THE Server SHALL ignore the comment and post a reply indicating insufficient permissions
3. THE Server SHALL cache collaborator status for 5 minutes to reduce API calls
4. WHERE apply or destroy Operations are triggered, THE Server SHALL require write permission level or higher
5. THE Server SHALL log all authorization checks with author username and decision

### Requirement 17: Multi-Project Consolidated Output

**User Story:** As a developer, I want to see results from all Projects in one place, so that I can review changes across my entire infrastructure.

#### Acceptance Criteria

1. WHEN multiple Projects are triggered, THE Server SHALL execute Operations for all Projects in parallel
2. THE Server SHALL wait for all Project Operations to complete before posting the consolidated comment
3. THE consolidated comment SHALL show each Project's status and change counts, visible without expanding that Project's detail section
4. THE consolidated comment SHALL include detailed output sections for each Project in collapsible markdown details blocks
### Requirement 18: Tool Selection and Tool Configuration in turnip.yaml

**User Story:** As a developer, I want to name the IaC_Tool a Project uses and configure it, so that I can pin a tool version and set workspaces, stacks, and environments.

#### Acceptance Criteria

1. THE turnip.yaml parser SHALL support a "uses" field within each Project definition, naming the IaC_Tool as "<tool>" or "<tool>@<version>"
2. THE turnip.yaml parser SHALL support a "with" field within each Project definition, carrying configuration read by that Project's Plugin and by nothing else
3. WHERE the IaC_Tool is Terraform, THE with field SHALL support "workspace" to specify the Terraform workspace name, and "backendConfig" to specify backend configuration the Plugin passes to the tool's initialization step — a survey of real Atlantis deployments found a per-project backend key to be the single most common reason projects need per-project arguments at all
4. WHERE the IaC_Tool is Pulumi, THE with field SHALL support "stack" to specify the Pulumi stack name
5. WHERE the IaC_Tool is Helmfile, THE with field SHALL support "environment" to specify the Helmfile environment
6. THE Server SHALL pass the with field to the Plugin when executing Operations
7. THE version portion of "uses" SHALL specify the IaC_Tool version the Runner Job SHALL provision (Requirement 14.2a)
8. IF the version portion is absent, THEN THE Server SHALL use a documented default version for that IaC_Tool
9. IF the version portion is present but is not well-formed, THEN THE Server SHALL reject the configuration with an error comment on the PR, without creating a Runner Job — well-formedness is a semver shape, not membership of a list the Server maintains, so a version the vendor publishes is usable the day it ships
10. THE Server SHALL accept a version portion written with or without a leading "v", normalizing it before the vendor image tag is formed

### Requirement 19: High Availability Server Deployment

**User Story:** As a platform operator, I want to run multiple Server instances for high availability, so that the platform remains operational during server failures or deployments.

#### Acceptance Criteria

1. THE Server SHALL be stateless and store no operation state locally
2. WHEN multiple Server instances are running, ANY Server instance SHALL be able to process ANY webhook event
3. THE Server SHALL use Redis/Valkey as the single source of truth for all locks and plan data
4. THE Server SHALL NOT require leader election or coordination between instances
5. WHEN a Server instance fails, OTHER Server instances SHALL continue processing webhooks without interruption
6. THE Server SHALL use Redis atomic operations (SET NX) to prevent race conditions between multiple instances
7. THE Server SHALL NOT rely on in-memory state that would be lost on instance restart

### Requirement 20: Lock Lifecycle Management

**User Story:** As a developer, I want locks to be automatically released when my PR is merged or closed, so that I don't have to manually unlock projects.

#### Acceptance Criteria

1. WHEN a PR is merged, THE Server SHALL release all locks held by that PR
2. WHEN a PR is closed without merging, THE Server SHALL release all locks held by that PR
3. THE Server SHALL process PR closed/merged webhook events to trigger lock release
4. WHEN a lock is released, THE Server SHALL post a comment to the PR indicating which projects were unlocked
5. IF a PR is reopened after being closed, THE Server SHALL require new plan operations to re-acquire locks


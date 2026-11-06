## MODIFIED Requirements

### Requirement: Helmfile command line
The Runner SHALL invoke Helmfile in the Project's directory as `helmfile --no-color [--environment <environment>] [--file <file>] [--kube-context <context>] [--kubeconfig <kubeconfig>] <operation> [arguments...]`, where each flag's value is the Project's `with:` key of the same name (`environment`, `file`, `kube-context`, `kubeconfig`), passed verbatim for Helmfile to interpret (a relative `file` or `kubeconfig` is relative to the Project's directory, where Helmfile runs), and the arguments are the plan's extra arguments (replayed by mutating operations from the recorded plan; see the `locking` capability). `--no-color` SHALL always be passed, so Helmfile's colored banners put no color codes in the results comment or the logs. Each of the four flags SHALL be passed only when its key is set to a non-empty value, in the order above, for every operation, the plan and mutating ones alike. They are Helmfile's global flags, so they apply to every sub-helmfile a Project's helmfile loads. Any other key under `with:` SHALL be accepted by validation and SHALL NOT change the command line, but SHALL NOT be silent: the Runner SHALL write one annotation line per such key, in key order, right after the command line in the record of the run (see the `plugins` capability), reading `@@ turnip: with: ignored unexpected key <key> (supported: environment, file, kube-context, kubeconfig) @@`, and the operation SHALL still run.

#### Scenario: Environment set
- **WHEN** a Project sets `with: {environment: staging}` and is planned with no extra arguments
- **THEN** the Runner runs `helmfile --no-color --environment staging diff`

#### Scenario: Environment omitted
- **WHEN** a Project has no `with.environment`
- **THEN** the Runner runs `helmfile --no-color diff`, and Helmfile uses its own default environment

#### Scenario: Extra arguments follow the operation
- **WHEN** a collaborator comments `/helmfile diff web -l name=ingress` and `web` sets `with.environment: staging`
- **THEN** the Runner runs `helmfile --no-color --environment staging diff -l name=ingress`

#### Scenario: Unknown with key
- **WHEN** a Project sets `with: {environment: staging, color: blue}`
- **THEN** the configuration is valid, `color` has no effect on the command line, and the record shows `@@ turnip: with: ignored unexpected key color (supported: environment, file, kube-context, kubeconfig) @@` right after the command line

#### Scenario: Kube context set
- **WHEN** a Project sets `with: {environment: staging, kube-context: stg}` and is planned with no extra arguments
- **THEN** the Runner runs `helmfile --no-color --environment staging --kube-context stg diff`

#### Scenario: File and kubeconfig set
- **WHEN** a Project sets `with: {file: helmfile.d/web.yaml, kubeconfig: /turnip/repository-secrets/web-kubeconfig}` and runs `sync`
- **THEN** the Runner runs `helmfile --no-color --file helmfile.d/web.yaml --kubeconfig /turnip/repository-secrets/web-kubeconfig sync`

#### Scenario: A misspelled key
- **WHEN** a Project sets `with: {enviroment: staging}`
- **THEN** the Runner runs `helmfile --no-color diff`, and the record shows `@@ turnip: with: ignored unexpected key enviroment (supported: environment, file, kube-context, kubeconfig) @@`

#### Scenario: An empty value
- **WHEN** a Project sets `with: {kube-context: ""}`
- **THEN** no `--kube-context` is passed and no warning is written

#### Scenario: Release tables without color codes
- **WHEN** a `helmfile apply` succeeds and prints its `Updated Releases` table
- **THEN** the table's banner reaches the results comment as plain text, with no color code such as `[1;34m` around it

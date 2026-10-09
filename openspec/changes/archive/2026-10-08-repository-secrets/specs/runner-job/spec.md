## ADDED Requirements

### Requirement: Repository Secret keys as files
When a Project's effective `runner.secrets` lists keys (see the `configuration` capability), its Runner Pod SHALL have a volume `repository-secrets` of the repository's Secret (see the `runner-access` capability) that projects exactly those keys, each to a file named after its key, with mode `0444`. The volume SHALL NOT be optional, so a missing Secret or key keeps the Pod from starting (see the `operation-lifecycle` capability). It SHALL be mounted read-only at `/turnip/repository-secrets` in the `runner` container only; no init container SHALL mount it. A Project that lists no key SHALL get no such volume.

#### Scenario: A Project lists a kubeconfig
- **WHEN** Project `lab` sets `runner.secrets: [lab-kubeconfig]` and `runner.env: {KUBECONFIG: /turnip/repository-secrets/lab-kubeconfig}`
- **THEN** the tool reads the kubeconfig from that file, and the directory holds no other key of the Secret

#### Scenario: The clone never sees the keys
- **WHEN** a Runner Job is built for a Project that lists keys
- **THEN** only the `runner` container mounts `repository-secrets`; `clone`, `provision-<tool>` and `copy-runner` do not

#### Scenario: No keys, no volume
- **WHEN** a Project's effective `runner.secrets` is absent or empty
- **THEN** its Runner Pod has no `repository-secrets` volume, whether or not the repository's Secret exists

## MODIFIED Requirements

### Requirement: Fixed paths under /turnip
Everything turnip places in a Runner Pod SHALL live under `/turnip`, on these volumes:

| Path | Volume | Mounted in | Present |
|---|---|---|---|
| `/turnip/src` | emptyDir `workspace` | `clone`, `runner` | always |
| `/turnip/tools` | emptyDir `tools` | `provision-<tool>`, `runner` | copy-out only |
| `/turnip/bin` | emptyDir `bin` | `copy-runner`, `runner` | run-in-image only |
| `/turnip/run/secrets/token` | projected `turnip-clone-identity`, read-only | `clone` | always |
| `/turnip/run/secrets/token` | projected `turnip-report-identity`, read-only | `runner` | always |
| `/turnip/repository-secrets` | Secret `repository-secrets`, read-only | `runner` | when the Project lists keys |

`/turnip/src` SHALL be the repository root in every Runner, so a Project's `directory` is resolved relative to it and committed configuration may reference it by absolute path. Each container that talks to the Server SHALL find its own Runner identity at the same path, so `TURNIP_TOKEN_FILE` has one value for both; the contents of the two Runner identity volumes are defined by the `runner-security` capability.

#### Scenario: An absolute path in committed configuration
- **WHEN** a Project's `runner.env` sets `KUBECONFIG: /turnip/src/.turnip/kubeconfig`
- **THEN** that path names the same file in the repository for every Project and every run

#### Scenario: Where a Project's tool runs
- **WHEN** an Operation runs for a Project with `directory: infra/web`
- **THEN** the tool runs with working directory `/turnip/src/infra/web`

#### Scenario: Each container's Runner identity at one path
- **WHEN** a Runner Job is built
- **THEN** `/turnip/run/secrets/token` is the `turnip-clone-identity` volume in the `clone` container and the `turnip-report-identity` volume in the `runner` container, and no other container mounts either

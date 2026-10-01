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

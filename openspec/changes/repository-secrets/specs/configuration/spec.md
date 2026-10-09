## ADDED Requirements

### Requirement: Repository Secret keys
In either `runner:` block, `secrets` SHALL be a list of keys of the repository's Secret (see the `runner-access` capability). Each key SHALL be 1 to 253 characters of letters, digits, `-`, `_` and `.`, SHALL NOT be `.` and SHALL NOT start with `..`; a key that breaks this SHALL be a validation error reading `"<key>" is not a valid Secret key: use up to 253 letters, digits, "-", "_" or ".", other than "." and not starting with ".."`. A key listed twice in one list SHALL be a validation error on its second entry, reading `"<key>" is already listed`. Every problem SHALL be reported together, against `runner.secrets[<index>]`; a problem in the top-level block SHALL be reported once against `turnip.yaml`, not once per Project inheriting it.

#### Scenario: A key with a slash
- **WHEN** Project `web` sets `runner.secrets: [clusters/poc]`
- **THEN** validation SHALL report `config: web: runner.secrets[0]: "clusters/poc" is not a valid Secret key: use up to 253 letters, digits, "-", "_" or ".", other than "." and not starting with ".."`

#### Scenario: A key listed twice at the top level
- **WHEN** the top-level `runner.secrets` is `[poc-kubeconfig, poc-kubeconfig]` and three Projects inherit it
- **THEN** validation SHALL report exactly one problem, `config: turnip.yaml: runner.secrets[1]: "poc-kubeconfig" is already listed`

## MODIFIED Requirements

### Requirement: Runner settings at two levels
`runner:` SHALL be accepted both at the top level and on each Project, with the same fields (`serviceAccount`, `env`, `secrets`) and meaning. Each Project SHALL run with its own block merged over the top-level one: `serviceAccount` is replaced (the Project's value if set, else the top level's, else the Server's default; see the `runner-access` capability), `env` is merged per variable (every top-level variable, then every Project variable over it), and `secrets` is replaced (the Project's list if set, even an empty one, else the top level's, else none). A Project SHALL NOT be able to unset a shared variable; setting it to an empty value overrides it with the empty string. Projects inheriting the same top-level block SHALL NOT affect one another's settings.

#### Scenario: Env merged per variable
- **WHEN** the top level sets `env: {AWS_REGION: us-west-2}` and Project `web` sets `env: {AWS_PROFILE: web}`
- **THEN** `web`'s tool SHALL run with both `AWS_REGION=us-west-2` and `AWS_PROFILE=web`

#### Scenario: Project value wins for the same variable
- **WHEN** both levels set `AWS_REGION` and the Project's value is `eu-west-1`
- **THEN** the Project's tool SHALL see `AWS_REGION=eu-west-1`

#### Scenario: Empty value overrides rather than unsets
- **WHEN** a Project sets a shared variable to `""`
- **THEN** the variable SHALL be present in the tool's environment with an empty value

#### Scenario: serviceAccount replaced
- **WHEN** the top level sets `serviceAccount: deployer` and Project `api` sets `serviceAccount: api-deployer`
- **THEN** `api` SHALL use `api-deployer` and every other Project SHALL use `deployer`, subject to the override gate in the `runner-access` capability

#### Scenario: secrets replaced
- **WHEN** the top level sets `secrets: [shared-kubeconfig]` and Project `lab` sets `secrets: [lab-kubeconfig]`
- **THEN** `lab` SHALL mount only `lab-kubeconfig` and every other Project SHALL mount only `shared-kubeconfig`

#### Scenario: A Project opts out of shared keys
- **WHEN** the top level sets `secrets: [shared-kubeconfig]` and Project `web` sets `secrets: []`
- **THEN** `web` SHALL mount no key

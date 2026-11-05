## MODIFIED Requirements

### Requirement: with passthrough
A Project's `with` SHALL be a map of string values handed to the Project's Plugin unchanged; turnip SHALL accept any key in it and interpret none itself. Which keys have an effect, and how a key the tool does not read is reported, is defined per tool: the Helmfile tool warns about such a key in the record of the run (see the `plugin-helmfile` capability).

#### Scenario: Unknown key inside with
- **WHEN** a Project sets `with: {environment: staging, team: web}`
- **THEN** validation SHALL accept both keys
- **AND** both SHALL reach the Plugin, which acts only on the keys it reads and reports the rest

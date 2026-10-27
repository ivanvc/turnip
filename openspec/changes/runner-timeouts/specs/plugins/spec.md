## MODIFIED Requirements

### Requirement: Execution transcript and interleaved output
Every tool run SHALL be recorded as one sequence of lines, each marked with the stream it was written to, with the tool's standard output and standard error interleaved rather than one after the other. Each stream's lines SHALL be in the order they were written. Across the two streams the record SHALL be in write order when the tool writes to one stream and then, later, to the other, however much it wrote to the first; lines the tool writes to both streams at the same moment, or within the time turnip takes to read one of them, can land either way round, close to where they were written. turnip SHALL open the record with two annotation lines and close it with one, each written `@@ turnip: <text> @@`:

| Line | Text |
|---|---|
| first | `<directory>, <tool> <tag-spec>`, the directory repository-relative (`.` for the root) and the tag-spec as `uses:` wrote it |
| second | the command's argument vector, `<tool> <args...>`, including arguments turnip supplied |
| last | `exit <code> in <duration>`, written after both streams have ended; or, when turnip stopped the tool (see the `operation-lifecycle` capability), `stopped: no output for <idle timeout>, after <duration>` or `stopped: the Runner was told to stop, after <duration>` |

The Operation's live log stream and the Runner's own log SHALL carry the record as it happened (see the `operation-lifecycle` and `observability` capabilities). The Operation's output in the pull request comment SHALL be what the tool makes of the record (see the requirement "Output chosen by the tool"). The annotations SHALL record arguments only, never the environment. A command that could not be started SHALL produce no transcript.

#### Scenario: Diff with a configured environment
- **WHEN** a Helmfile Project with directory `infra/web`, `uses: helmfile@v1.7.4` and `with: {environment: staging}` runs `diff -l name=api`
- **THEN** its output SHALL open with `@@ turnip: infra/web, helmfile v1.7.4 @@` and `@@ turnip: helmfile --no-color --environment staging diff -l name=api @@`
- **AND** it SHALL end with a line such as `@@ turnip: exit 0 in 4.2s @@`

#### Scenario: Warning written mid-run
- **WHEN** a tool writes a line to standard output, a moment later a warning to standard error, and a moment later another line to standard output
- **THEN** the warning SHALL appear between those two lines in the record, not at its end

#### Scenario: Errors written after a long output
- **WHEN** a tool writes 20,000 lines to standard output and then 5 lines to standard error
- **THEN** every one of the 20,000 lines SHALL precede the first standard error line in the record

#### Scenario: Errors written after a burst
- **WHEN** a tool writes several megabytes to standard output in one write and then an error to standard error
- **THEN** the error SHALL follow every line of that write in the record

#### Scenario: Both streams written at once
- **WHEN** a tool writes to standard output and to standard error at the same time, or one right after the other with no pause
- **THEN** each stream's lines SHALL keep their own order in the record
- **AND** a line of one stream MAY be recorded before or after a line of the other written at that moment

#### Scenario: Tool binary missing
- **WHEN** the tool's binary cannot be found in the image
- **THEN** the Operation SHALL fail with no transcript lines in its output

#### Scenario: A tool stopped for silence
- **WHEN** the Runner stops a tool after 30 minutes with no output, 41 minutes into the run
- **THEN** the record SHALL end with `@@ turnip: stopped: no output for 30m0s, after 41m0s @@`

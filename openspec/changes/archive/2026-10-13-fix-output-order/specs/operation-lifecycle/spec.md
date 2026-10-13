## MODIFIED Requirements

### Requirement: Runner-to-Server stream contract
A Runner SHALL report over a single client-streaming gRPC call, `ExecuteOperation`, to the address in `TURNIP_SERVER_ADDR`, authenticated as defined by the `runner-security` capability. Each stream SHALL open with a start message describing the Operation (marked resumed when it is not the first stream for that Operation), followed by any number of log lines (timestamp, level `info` for stdout or `error` for stderr, message), and close with one result: success, the Operation's output, exit code, error message, add/change/destroy counts, plan data and a failure category.

The Server SHALL identify the Operation from the authenticated identity, never from the start message; a start message naming a different Operation SHALL be ignored with a logged warning. The first log line the Server receives on each stream SHALL mark its Operation started, and the Server SHALL do nothing for that stream's later lines. Log line content SHALL serve only as evidence of progress: the Server SHALL NOT store or display it, and the pull request sees only the result's output (see the `pr-comment` and `project-checks` capabilities). The Runner SHALL also write the tool's output, unstripped, to its own container's stdout and stderr.

The `runner` container SHALL connect before running the tool, send every line of the tool's record (see the `plugins` capability) in the record's order while the tool runs, and send the result when the tool finishes. While the stream stays open no line SHALL be left out, including lines the Operation's output omits. Sending SHALL NOT delay the tool, the recording of its output or the Runner's own log, however slowly the Server reads. The `clone` container SHALL connect only when the checkout fails, and send a result with no log lines (see the `checkout` capability).

#### Scenario: A plan that prints output
- **WHEN** a Runner's tool prints its first line
- **THEN** the line is streamed to the Server and the Operation is marked started

#### Scenario: A mismatched start message
- **WHEN** an authenticated Runner's start message names a different operation id
- **THEN** the Server logs a warning and applies the stream to the authenticated Operation

#### Scenario: A Server slower than the tool
- **WHEN** the Server reads log lines more slowly than the tool writes them
- **THEN** the tool is not slowed and the order of its record is unchanged
- **AND** every line of the record still reaches the Server, in the record's order, before the result

#### Scenario: A verbose plan
- **WHEN** a tool prints 20,000 lines over one stream
- **THEN** the Server marks the Operation started on the first of them and does nothing for the other 19,999

#### Scenario: Lines the output omits
- **WHEN** a failed Helmfile `diff`'s output omits its standard output (see the `plugin-helmfile` capability)
- **THEN** the stream still carried every line of its standard output

### Requirement: Log buffering
On a stream that never broke, the Runner SHALL send every log line once and replay none. When a stream broke, the Runner SHALL replay, on the stream that replaces it and before its result, the most recent 256 KiB of the record's log lines (counting timestamp, level and message), preceded by a line at level `warn` reading `<N> lines dropped` when the record holds N lines older than those replayed.

#### Scenario: A long-running, verbose Operation
- **WHEN** a tool prints more than 256 KiB of output and the stream never breaks
- **THEN** every line reaches the Server once, and no `lines dropped` line is sent

#### Scenario: A verbose Operation whose stream broke
- **WHEN** a tool prints more than 256 KiB of output and the stream breaks while it runs
- **THEN** the replay on the resumed stream begins with `<N> lines dropped` and carries only the most recent lines

## ADDED Requirements

### Requirement: Text sent to the Server is valid UTF-8
Before anything leaves the Runner for the Server, each run of bytes that is not valid UTF-8 SHALL be replaced with one Unicode replacement character (U+FFFD, `�`). This SHALL apply to what the workspace path is stripped from: streamed log lines, the result's output and error message, and checkout failure messages. The 1 MiB limit on a result's output SHALL apply to the text after replacement. The Runner's own log and the change counts SHALL use the bytes as the tool wrote them.

#### Scenario: Invalid bytes in a plan's output
- **WHEN** a tool prints the line `name: caf` followed by the single byte `0xE9` and exits 0
- **THEN** the Server receives that line as `name: caf�` and the result on the first attempt
- **AND** the Operation's check completes and the results comment shows the line with `�`

#### Scenario: A run of invalid bytes
- **WHEN** a tool prints three consecutive bytes that are not valid UTF-8 between `a` and `b`
- **THEN** the Server receives `a�b`

#### Scenario: Valid text is unchanged
- **WHEN** a tool prints `naïve 💡` in valid UTF-8
- **THEN** the Server receives exactly `naïve 💡`

#### Scenario: A log line with invalid bytes
- **WHEN** a tool prints a line with invalid bytes and then more lines
- **THEN** the log stream carries that line with `�` in their place and goes on to carry every later line on the same stream

#### Scenario: A failed checkout
- **WHEN** a checkout fails and git's message holds bytes that are not valid UTF-8
- **THEN** the Server receives the failure message with `�` in their place

#### Scenario: The Runner's own log
- **WHEN** a tool prints a line with invalid bytes
- **THEN** `kubectl logs` on the Runner Pod shows the line with the bytes the tool wrote

#### Scenario: Counts are read before replacement
- **WHEN** a Helmfile `diff` prints a release's diff whose only line holds invalid bytes
- **THEN** the release still counts as changed

#### Scenario: A long output with invalid bytes
- **WHEN** a plan's output is 3 MiB and holds invalid bytes throughout
- **THEN** its result carries at most 1 MiB of output after replacement, cut as the requirement "Output sent with a result" describes

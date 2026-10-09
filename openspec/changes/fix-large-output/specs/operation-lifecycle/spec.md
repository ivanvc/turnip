## ADDED Requirements

### Requirement: Output sent with a result
The Runner SHALL send at most 1 MiB (1,048,576 bytes) of an Operation's output with its result. A longer output SHALL be cut at a line boundary, keeping its end: the leading run of turnip's annotation lines (`@@ turnip: ... @@`) and the closing one SHALL be kept, the start of the lines between them removed, and the line `@@ turnip: <K> earlier lines omitted (<size>) @@` put right after the leading annotation lines, where `<size>` is the bytes removed, in KiB below 1 MiB and in MiB from 1 MiB, with one decimal. Change counts SHALL be read from the whole output before it is cut (see the `plugins` capability), and plan data SHALL never be cut. What the live log stream carries is not cut.

#### Scenario: A first install's plan
- **WHEN** a plan's output is 5,069,133 bytes
- **THEN** its result carries at most 1 MiB of output: the leading annotation lines, then `@@ turnip: <K> earlier lines omitted (3.8 MiB) @@`, then the end of the tool's output and the closing annotation line
- **AND** the plan's change counts are those of the whole output

#### Scenario: Output within the limit
- **WHEN** a plan's output is 200 KiB
- **THEN** its result carries the whole output, with no omission line

## MODIFIED Requirements

### Requirement: Retry budgets
The Runner SHALL retry opening its stream with exponential backoff (1 second base, capped at 30 seconds, full jitter) for up to 2 minutes; if it cannot connect within that budget, it SHALL exit with status 1 without running the tool. If the stream breaks while the tool runs, the Runner SHALL keep running the tool and buffer its output. The Runner SHALL retry delivering its result, reopening the stream as needed, with exponential backoff (1 second base, capped at 60 seconds, full jitter) for up to 15 minutes; if delivery fails within that budget, it SHALL exit with status 1. After delivering a result the Runner SHALL exit 0, whether the tool succeeded or failed. A result the Server refuses for its size SHALL NOT be retried: the Runner SHALL instead deliver a failed result with no output, no plan data and no category, whose error message reads `the result was too large to deliver (<size>; the Server accepts at most 4 MiB)`, under the same budget.

#### Scenario: The Server is briefly unreachable at start
- **WHEN** the Server becomes reachable 40 seconds after the Runner starts
- **THEN** the Runner connects and runs the tool

#### Scenario: The Server is unreachable for the whole connect budget
- **WHEN** the Runner cannot open a stream within 2 minutes
- **THEN** it exits 1 without running the tool, and the Operation is finalized by the start timeout

#### Scenario: A Server replica restarts mid-operation
- **WHEN** the stream breaks while the tool is running and a Server replica is reachable again within 15 minutes of the tool finishing
- **THEN** the Runner opens a resumed stream, replays its buffered log lines and delivers the result

#### Scenario: A failed tool
- **WHEN** the tool exits 1 and the result is delivered
- **THEN** the Runner exits 0

#### Scenario: A result too large to deliver
- **WHEN** the Server refuses a result because it exceeds 4 MiB
- **THEN** the Runner sends no second copy of it, and delivers instead a failed result whose error message says it was too large to deliver

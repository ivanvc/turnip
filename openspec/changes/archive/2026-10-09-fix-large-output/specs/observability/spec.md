## MODIFIED Requirements

### Requirement: Runner logs
The Runner SHALL mirror the tool's standard output and standard error to its own, line by line as they arrive, unredacted and with absolute paths intact, so `kubectl logs` on the Runner Pod shows exactly what the tool printed. Its own messages SHALL be plain-text lines prefixed `runner: ` on standard error. Once the Operation finishes, whether or not the Server acknowledged it, the Runner SHALL log one line summarizing its result, `runner: operation result: success=<true|false> exit_code=<code> category=<category, or none> add=<A> change=<C> destroy=<D> output_bytes=<size of the whole output> sent_bytes=<size of the output sent>`, which SHALL NOT repeat the output: the output is already in the log, line by line. When the result carries an error message, the Runner SHALL log it just before that line, as `runner: operation error: <message>`. A Runner settings error SHALL be logged as a JSON record `loading config` and the Runner SHALL exit non-zero. The Runner reads `TURNIP_LOG_LEVEL` the same way as the Server, but turnip does not set it on Runner Pods and `runner.env` cannot set it (names beginning `TURNIP_` are reserved; see the `configuration` capability), so it SHALL have no effect in practice.

#### Scenario: Reading a Runner's output
- **WHEN** an operator runs `kubectl logs` on a finished Runner Pod's tool container
- **THEN** it shows the tool's own output followed by a `runner: operation result:` line

#### Scenario: A large output in the Runner's log
- **WHEN** a plan prints 5,069,133 bytes
- **THEN** `kubectl logs` shows the tool's output once, followed by a `runner: operation result:` line reporting `output_bytes=5069133` and the smaller `sent_bytes`, without the output

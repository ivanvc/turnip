## MODIFIED Requirements

### Requirement: Size limits, splitting and dropping
No comment body SHALL exceed 65536 characters. A Project section too large for one body SHALL be split into several self-contained sections, each closing its own fence and `<details>`, with ` (output part N/M)` appended to the summary; the trailer SHALL appear only on the last part. Each part SHALL end at the end of the last whole line of output that fits it, and the next part SHALL start with the line after it; only a line longer than a whole part SHALL be split inside, and then between two characters, never inside one. A Project's output SHALL take at most as many parts as `TURNIP_MAX_COMMENTS_PER_PROJECT` allows (5 by default): when it would need more, the start of the output after its leading annotation lines SHALL be cut so that it fits, keeping the end, and the line `…(<K> earlier lines omitted)` put right after those annotation lines. Sections SHALL be packed greedily into as many bodies as needed; the first body opens with the notices and verdict line, every later body with `_(continued N/M)_`, and the footer appears only at the end of the last body. If a body still exceeds the limit, whole sections SHALL be dropped from its start, with `_(1 earlier section omitted because this comment reached GitHub's size limit)_` (or `_(K earlier sections omitted ...)_`) placed after the header; if a single remaining section still does not fit, the start of its output SHALL be cut and replaced with `…(earlier output omitted)`, keeping the end from the start of a whole line, or from a character boundary when the last line alone is too long.

#### Scenario: One very large plan
- **WHEN** one Project's plan output is several times the comment limit
- **THEN** turnip posts several comments, the first with the verdict line, the rest headed `_(continued 2/3)_` and so on, each carrying one `(output part N/M)` piece, and every fence closed within its comment

#### Scenario: A line where a part ends
- **WHEN** a Project's output is split into parts and the space left in a part ends in the middle of the line `port: 8888`
- **THEN** that part ends with the line before it, and the next part begins with `port: 8888` whole

#### Scenario: A multi-byte character where a part ends
- **WHEN** the space left in a part ends in the middle of a multi-byte character, such as `é` or `💡`
- **THEN** the character appears whole in exactly one part, and no part contains a broken character

#### Scenario: A line longer than a part
- **WHEN** a single line of output is longer than a whole part
- **THEN** that line alone is split across parts, each piece ending between two characters

#### Scenario: Every line kept
- **WHEN** a Project's output is split into parts and not cut by the cap
- **THEN** joining the parts' outputs gives the output exactly, with no line lost, repeated or joined to another

#### Scenario: Output beyond the cap
- **WHEN** one Project's output would take 80 parts and `TURNIP_MAX_COMMENTS_PER_PROJECT` is unset
- **THEN** its section takes 5 parts, the first showing its leading annotation lines, then `…(<K> earlier lines omitted)`, then the end of the output

#### Scenario: A raised cap
- **WHEN** `TURNIP_MAX_COMMENTS_PER_PROJECT=8` and one Project's output would take 80 parts
- **THEN** its section takes 8 parts

#### Scenario: A section cut to fit a body
- **WHEN** a single section is cut to fit one body, and the cut would fall in the middle of a line
- **THEN** the kept output starts at the beginning of the next whole line, after `…(earlier output omitted)`

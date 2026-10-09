## ADDED Requirements

### Requirement: The TURNIP_MAX_COMMENTS_PER_PROJECT setting
The Server SHALL read `TURNIP_MAX_COMMENTS_PER_PROJECT` as the most parts, each in a comment body of its own, that one Project's output may take in a results comment, 5 when unset. A value that is not a positive integer SHALL stop the Server at startup with an error naming the setting and the value. A repository SHALL NOT be able to change it.

#### Scenario: Unset
- **WHEN** `TURNIP_MAX_COMMENTS_PER_PROJECT` is unset
- **THEN** a Project's output takes at most 5 parts

#### Scenario: Zero
- **WHEN** `TURNIP_MAX_COMMENTS_PER_PROJECT=0`
- **THEN** the Server exits at startup with an error naming `TURNIP_MAX_COMMENTS_PER_PROJECT` and `0`

## MODIFIED Requirements

### Requirement: Size limits, splitting and dropping
No comment body SHALL exceed 65536 characters. A Project section too large for one body SHALL be split into several self-contained sections, each closing its own fence and `<details>`, with ` (output part N/M)` appended to the summary; the trailer SHALL appear only on the last part. A Project's output SHALL take at most as many parts as `TURNIP_MAX_COMMENTS_PER_PROJECT` allows (5 by default): when it would need more, the start of the output after its leading annotation lines SHALL be cut so that it fits, keeping the end, and the line `…(<K> earlier lines omitted)` put right after those annotation lines. Sections SHALL be packed greedily into as many bodies as needed; the first body opens with the notices and verdict line, every later body with `_(continued N/M)_`, and the footer appears only at the end of the last body. If a body still exceeds the limit, whole sections SHALL be dropped from its start, with `_(1 earlier section omitted because this comment reached GitHub's size limit)_` (or `_(K earlier sections omitted ...)_`) placed after the header; if a single remaining section still does not fit, the start of its output SHALL be cut and replaced with `…(earlier output omitted)`, keeping the end.

#### Scenario: One very large plan
- **WHEN** one Project's plan output is several times the comment limit
- **THEN** turnip posts several comments, the first with the verdict line, the rest headed `_(continued 2/3)_` and so on, each carrying one `(output part N/M)` piece, and every fence closed within its comment

#### Scenario: Output beyond the cap
- **WHEN** one Project's output would take 80 parts and `TURNIP_MAX_COMMENTS_PER_PROJECT` is unset
- **THEN** its section takes 5 parts, the first showing its leading annotation lines, then `…(<K> earlier lines omitted)`, then the end of the output

#### Scenario: A raised cap
- **WHEN** `TURNIP_MAX_COMMENTS_PER_PROJECT=8` and one Project's output would take 80 parts
- **THEN** its section takes 8 parts

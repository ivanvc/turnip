# pr-comment Specification

## Purpose
The consolidated results comment turnip posts on a pull request after a trigger's Operations finish, so end users can read every Project's outcome, output and next commands in one place without opening each check.

## Requirements

### Requirement: One results comment per trigger, split by kind
Once every Operation started by one trigger (an automatic plan, or one trigger comment with all its commands) has finished, turnip SHALL post the results of all its Projects together, including Projects refused before or during dispatch, as fresh pull request comments. Results whose Operation is its tool's plan operation (for Helmfile, `diff`) SHALL form the plan comment; every other Operation's results, and any result whose tool is unknown, SHALL form a separate apply comment. When a trigger has both, the plan comment SHALL be posted first. A trigger that produced no Project result (only replies, or nothing at all) SHALL post no results comment. Replies to malformed lines, unlocks and selection errors are separate comments (see the `commands` capability).

The results comment SHALL be posted once per trigger by whichever Server replica finishes the trigger (see the `operation-lifecycle` capability), so it SHALL be posted even when the replica that received the trigger is gone.

#### Scenario: Plan and apply in one comment
- **WHEN** a single trigger comment contains `/turnip diff web` and `/turnip apply api`
- **THEN** turnip posts one plan comment carrying `web`'s result and, after it, one apply comment carrying `api`'s result

#### Scenario: Refused Projects still reported
- **WHEN** a trigger comment asks for `apply` on a Project and the author lacks write permission, alongside a `diff` that runs
- **THEN** the refused Project appears in the apply comment as a failed section whose output is the refusal reason

#### Scenario: Automatic plan
- **WHEN** the automatic plan for a pushed commit finishes for every matched Project
- **THEN** turnip posts one plan comment covering all of them

#### Scenario: The receiving replica restarts
- **WHEN** the replica that received `/turnip apply web` restarts while the apply runs
- **THEN** the apply comment for `web` is still posted, once

### Requirement: Verdict line
Each results comment SHALL open with a bold verdict line summarizing every Project in that comment. For a single Project it SHALL read `**N changes in `name`.**` (`1 change` when one), `**No changes in `name`.**`, `**Succeeded in `name`.**` when a successful mutating Operation's counts were unreported (see the `plugins` capability), or `**`name` failed.**`. For several Projects the headline SHALL be `All N projects failed` when every one failed, otherwise `T changes across N projects` when any Project reported changes (T being the sum of additions, changes and destructions), otherwise `Succeeded across N projects` when any had unreported counts, otherwise `No changes across N projects`. It SHALL then list only the exceptions, comma-separated after a colon: `K with no changes`, `K succeeded` (unreported, shown only when other Projects reported changes) and `K failed` (when not all failed). A headline with no exceptions SHALL end with the period inside the bold.

#### Scenario: Mixed outcome across Projects
- **WHEN** a comment carries three Projects: one with 5 changes, one with no changes and one failed
- **THEN** the verdict line reads `**5 changes across 3 projects**: 1 with no changes, 1 failed.`

#### Scenario: Every Project clean
- **WHEN** two Projects both report no changes
- **THEN** the verdict line reads `**No changes across 2 projects.**`

#### Scenario: Unreported counts never read as no changes
- **WHEN** a single Project's `sync` succeeds and its counts are unreported
- **THEN** the verdict line reads `**Succeeded in `web`.**`

### Requirement: Selection notices
Command-level notices from Project selection (a pattern that matched nothing, a changed-file listing GitHub truncated; worded by the `commands` capability) SHALL be rendered above the verdict line as a GitHub warning alert: a `> [!WARNING]` line followed by one `> `-prefixed line per notice. A trigger's notices SHALL appear once, on the plan comment when there is one, otherwise on the apply comment.

#### Scenario: Unmatched pattern alongside a matched one
- **WHEN** a trigger comment selects `web` and the pattern `db-*`, which matches nothing
- **THEN** the results comment opens with `> [!WARNING]` and `> No project matched `db-*`.`, followed by the verdict line

### Requirement: Per-Project sections
Each Project result SHALL be one collapsible `<details>` section whose `<summary>`, visible while collapsed, reads `<mark> <code>name</code>: operation, detail`, where the mark is `✅` for success or `❌` for failure and the detail is the change text shared with the Project checks (`+A ~C -D`, `no changes`, or `succeeded` when unreported; see the `project-checks` capability) or `failed`. A failed result SHALL show no counts. When the Operation ran with arguments, the summary SHALL append `, <code>args</code>` with the arguments joined by spaces and truncated to 40 characters, the last replaced by `…`. Every value in the summary SHALL be HTML-escaped. Inside the section the Project's output SHALL appear in a code fence, tagged `diff` for a successful result and untagged for a failure, made of at least three backticks and always more backticks than the longest backtick run at the start of any output line, so the output cannot close it.

#### Scenario: Scoped plan with changes
- **WHEN** `web`'s `diff` ran with `-l name=api` and reported 1 addition, 4 changes and 2 destructions
- **THEN** its summary reads `✅ <code>web</code>: diff, +1 ~4 -2, <code>-l name=api</code>`
- **AND** its output is fenced as ```` ```diff ````

#### Scenario: Failed Operation
- **WHEN** `api`'s `diff` fails
- **THEN** its summary reads `❌ <code>api</code>: diff, failed` and its output sits in an untagged fence

#### Scenario: Output containing a fence
- **WHEN** a Project's output contains a line starting with three backticks
- **THEN** the section's fence uses four backticks, so the output renders verbatim inside it

### Requirement: Section trailer and next steps
After the fenced output, the final piece of a Project's section SHALL carry, in order: a blocked note when another pull request's Lock refused the plan (`Locked by [#N](url).`, or `Locked by pull request #N.` when no URL is known); the lock note explaining why this Project's Lock moved, when it moved (wording and transitions are the `locking` capability's); and a list of commands for that Project alone. The list SHALL offer `- Apply just this project: `/turnip <apply-op> <name>`` only for a successful plan while this pull request still holds the Project's Lock, whatever the plan's counts and whether they were reported, and never for the result of a mutating operation; `- Re-plan it: `/turnip <plan-op> <name>`` for any result its Runner reported; and `- Release its lock: `/turnip unlock <name>`` only while this pull request still holds the Project's Lock. The apply offered SHALL be the tool's own apply operation (`apply` for Helmfile). A refused Operation offers no command; a timed-out one offers only the unlock while its Lock is held.

#### Scenario: Plan with changes keeps its Lock
- **WHEN** `web`'s `diff` succeeds with changes and the pull request holds `web`'s Lock
- **THEN** the section ends with the three lines `Apply just this project: /turnip apply web`, `Re-plan it: /turnip diff web` and `Release its lock: /turnip unlock web`

#### Scenario: Plan blocked by another pull request
- **WHEN** `web`'s plan is refused because pull request #5 holds its Lock
- **THEN** the section's output is `locked by PR #5` and its trailer reads `Locked by [#5](https://github.com/<owner>/<repo>/pull/5).`

#### Scenario: A clean plan whose Lock is kept
- **WHEN** `web`'s Helmfile `diff` succeeds with no changes, and the pull request keeps `web`'s Lock because Helmfile acts without changes
- **THEN** the section ends with `Apply just this project: /turnip apply web`, `Re-plan it: /turnip diff web` and `Release its lock: /turnip unlock web`

#### Scenario: A plan whose counts were not reported
- **WHEN** `web`'s Helmfile `diff -q` succeeds, reads `succeeded`, and the pull request keeps `web`'s Lock
- **THEN** the section offers `Apply just this project: /turnip apply web`

#### Scenario: A clean plan whose Lock is released
- **WHEN** a plan for a tool that does not act without changes succeeds with no changes, and its Lock is released
- **THEN** the section offers only `Re-plan it`, with no apply and no unlock

#### Scenario: A successful apply
- **WHEN** `/turnip apply web` succeeds with changes
- **THEN** the section does not offer `Apply just this project`

#### Scenario: A failed plan
- **WHEN** `web`'s `diff` fails
- **THEN** the section does not offer `Apply just this project`

### Requirement: Footer
The last comment body SHALL end with a footer only when at least one Project in the comment is still locked by this pull request. The footer SHALL read `This pull request holds locks on `a`, `b` until applied or released.`, followed, when any locked Project's Operation ran with arguments, by `Applying replays the scope each plan recorded, which may be narrower than the whole Project.`, and then `` `/turnip apply` or `/turnip unlock` ``. The footer SHALL never print a command carrying arguments.

#### Scenario: Nothing locked
- **WHEN** every Project in the comment released or never held its Lock
- **THEN** the comment has no footer

#### Scenario: Scoped plan holding a Lock
- **WHEN** `web`'s plan ran with `-l name=api` and holds its Lock
- **THEN** the footer names `web`, carries the replay sentence, and ends with `` `/turnip apply` or `/turnip unlock` ``

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

### Requirement: Minimizing outdated plan comments
When `TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS` is true (default false), before posting a plan comment turnip SHALL mark every comment of the previous plan comment on the same pull request as outdated (GitHub's minimize, classifier OUTDATED), and after posting SHALL remember the new comment's bodies as the pull request's latest plan comment. Apply comments SHALL never be minimized or remembered. A failure to minimize or remember SHALL be logged and SHALL NOT stop the new comment from being posted. The remembered plan comment SHALL be forgotten when the pull request closes (see the `pull-request-lifecycle` capability), and otherwise expires 90 days after it was last written. When the setting is false, nothing is minimized or remembered.

#### Scenario: Re-plan collapses the earlier plan
- **WHEN** minimizing is enabled and a second plan comment is posted on a pull request
- **THEN** the first plan comment is shown as outdated and the new one is posted

#### Scenario: Apply comments stay
- **WHEN** minimizing is enabled and an apply comment follows a later plan
- **THEN** the apply comment is never minimized

### Requirement: Failures to post
Each comment body SHALL be posted once; a failure to post a body SHALL be logged and the remaining bodies SHALL still be posted. A problem recording a Project's check or the `turnip` check SHALL be stated as a note appended to that Project's output in the comment (worded by the `project-checks` and `merge-check` capabilities).

#### Scenario: GitHub rejects one body
- **WHEN** posting the first of two bodies fails
- **THEN** the Server logs `posting comment` with the error, and the second body is still posted

### Requirement: The TURNIP_MAX_COMMENTS_PER_PROJECT setting
The Server SHALL read `TURNIP_MAX_COMMENTS_PER_PROJECT` as the most parts, each in a comment body of its own, that one Project's output may take in a results comment, 5 when unset. A value that is not a positive integer SHALL stop the Server at startup with an error naming the setting and the value. A repository SHALL NOT be able to change it.

#### Scenario: Unset
- **WHEN** `TURNIP_MAX_COMMENTS_PER_PROJECT` is unset
- **THEN** a Project's output takes at most 5 parts

#### Scenario: Zero
- **WHEN** `TURNIP_MAX_COMMENTS_PER_PROJECT=0`
- **THEN** the Server exits at startup with an error naming `TURNIP_MAX_COMMENTS_PER_PROJECT` and `0`

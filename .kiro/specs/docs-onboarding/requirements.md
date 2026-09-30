# Requirements: Documentation a New User Can Follow (Slice 48)

## Introduction

turnip's documentation is organized by the order it was written in, not
by who reads it. Five pages carry everything: `configuration.md` alone
serves end users (the `turnip.yaml` fields), operators (every
`TURNIP_*` setting, the GitHub App, the Runner's ServiceAccount) and the
reasoning behind both, in one page. A newcomer has no starting point that
knows whether they run turnip or use it, and no guide that takes them
from nothing to a first plan.

Every slice adds to these pages, so the cost of reorganizing grows with
each one. Slice 47 showed the pattern: Helmfile's material had spread
across four files before `docs/plugins/helmfile.md` gathered it, split
by audience, into "Using" and "Running" sections. This slice applies the
same idea to the whole set, now, while the pages are still few.

**Three audiences** (decided 2026-09-30; "end user", not "author", so the
term is never read as turnip's own author):

| Audience | Who | Wants |
|---|---|---|
| End user | writes `turnip.yaml`, comments on pull requests | what to write, what commands do, what the pull request shows |
| Operator | deploys and configures the Server | how to install, every setting, what to grant, what can go wrong |
| Developer | works on turnip itself | how it is built, how to add a tool |

This slice moves and reorganizes; it rewrites only where a page has to
change to fit its reader, and adds only the two getting-started guides
and the entry page.

## Glossary

- **Guide**: a page that takes the reader through a task, in order.
- **Reference**: a page that lists every item of one kind (every
  `turnip.yaml` field, every Server setting), for looking up.
- **Plugin_Page**: `docs/plugins/<tool>.md`, one per tool, split into a
  "Using" section for end users and a "Running" section for operators.

## Requirements

### Requirement 1: One entry point, by audience

**User Story:** As someone new to turnip, I want one page that asks
whether I use turnip or run it, so that I start in the right place.

#### Acceptance Criteria

1. `docs/README.md` SHALL be the documentation's entry page, sending an
   end user, an operator and a developer each to their starting point
2. THE repository's root `README.md` SHALL introduce turnip and link to
   the entry page, rather than list the documentation itself

### Requirement 2: The pages grouped by audience

#### Acceptance Criteria

1. THE documentation SHALL be grouped into one directory per audience,
   plus `plugins/` for Plugin_Pages
2. EVERY page SHALL state, in its opening lines, which audience it is for
3. A page SHALL NOT repeat another audience's material; it SHALL link to
   it
4. `SECURITY.md` SHALL stay at the repository root, where GitHub looks
   for it, and be linked from the operator documentation

### Requirement 3: Guides to a first result

#### Acceptance Criteria

1. AN operator getting-started Guide SHALL take the reader from nothing
   to a running Server receiving webhooks from one repository
2. AN end-user getting-started Guide SHALL take the reader from nothing
   to a first `turnip.yaml`, a first plan on a pull request, and a first
   apply
3. EACH Guide SHALL end by pointing to the Reference and the Plugin_Pages
   rather than covering them

### Requirement 4: References that are complete

#### Acceptance Criteria

1. A `turnip.yaml` Reference SHALL list every field the schema accepts,
   top-level and per Project, each with a link to where it is explained
2. A Server settings Reference SHALL list every `TURNIP_*` setting the
   Server reads, with its default and whether it is required
3. EACH Reference SHALL be checked against the code by a test, so a field
   or setting added later without its row fails the build
4. EVERY setting the Server reads SHALL be read in one place, so the
   test sees all of them; `TURNIP_LOG_LEVEL`, read today by `cmd/server`
   on its own and documented nowhere, SHALL move there and gain its row

*Rationale for 4.3: a Reference that silently misses what was added last
is worse than none, and every slice adds settings. The schema and the
Server's settings are both enumerable from the code.*

### Requirement 5: Nothing lost, nothing broken

#### Acceptance Criteria

1. EVERY section of today's documentation SHALL land somewhere, recorded
   in a map from its old location to its new one
2. EVERY relative link and `#anchor` in the documentation SHALL resolve,
   checked by a test that fails the build on a broken one
3. EVERY Go string or code comment that cites a documentation page or
   section by name SHALL point to its new place

### Requirement 6: Later slices keep the layout

#### Acceptance Criteria

1. `CLAUDE.md` SHALL state where each audience's documentation lives, so
   a later slice adds its documentation to the right page
2. `docs/development.md`'s "Adding a tool" SHALL keep requiring a
   Plugin_Page with its "Using" and "Running" sections

### Requirement 7: What operators must know about Redis

#### Acceptance Criteria

1. THE operator documentation SHALL state that Redis must persist its
   data, and that losing it drops every Lock and recorded plan, so a
   pull request must plan again before it can apply

*Rationale: found 2026-09-29, when a Redis restart without persistence
dropped a plan that was four days old. The documentation said nothing
about it.*

## Out of Scope

- **A documentation site** (a generator, GitHub Pages). The layout is
  chosen to carry over to one unchanged; publishing it is a later
  decision.
- **Rewriting content** beyond what a page needs to fit its audience.
- **Plugin_Pages for tools that do not exist yet** (Slices 7 and 46).
- **The bare-apply refusal's wording**, found with the Redis restart and
  set aside by the repository owner.
- **`docs/internal/`**, which is private.

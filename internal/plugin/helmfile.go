package plugin

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ivanvc/turnip/internal/provisioning"
)

// HelmfilePlugin implements Plugin by shelling out to the helmfile CLI.
type HelmfilePlugin struct {
	run commandRunner
}

// NewHelmfilePlugin constructs a HelmfilePlugin backed by a real helmfile
// subprocess.
func NewHelmfilePlugin() *HelmfilePlugin {
	return &HelmfilePlugin{run: execCommand}
}

func (p *HelmfilePlugin) Name() string { return "helmfile" }

func (p *HelmfilePlugin) GetOperations() []string {
	return []string{"diff", "apply", "sync"}
}

// ActsWithoutChanges is true because GetOperations exposes `sync`
// alongside `apply`. `helmfile sync` runs `helm upgrade --install` for
// every release regardless of the diff — it acts precisely when a diff
// found nothing, which is the whole of its difference from `apply`.
//
// `helmfile apply` alone would answer false: it diffs first and syncs
// only the releases that changed, so it is inert relative to the diff
// that preceded it. Answering from the apply operation alone would remove
// `sync` from every Project whose diff came back clean, permanently — a
// re-plan would find no changes and release the Lock again.
func (p *HelmfilePlugin) ActsWithoutChanges() bool { return true }

// Provisioning runs in helmfile's own image rather than copying a binary
// out, because helmfile is a runtime, not a binary: it shells out to
// `helm`, `helmfile diff` needs the helm-diff plugin, helm-secrets needs
// `sops`, and helm locates its plugins through an environment this image
// sets. Copying one binary out fails with `exec: "helm": executable file
// not found in $PATH`.
func (p *HelmfilePlugin) Provisioning() provisioning.Spec {
	return provisioning.Spec{
		Strategy: provisioning.RunInImage,
		Image:    "ghcr.io/helmfile/helmfile",
	}
}

// ImageTags allows full versions and digests only, so the helmfile Alias
// cannot move to a new vendor release, or to a floating tag such as
// latest, with no change to turnip.yaml. helmfile's tags carry a leading
// v (v1.7.4), used verbatim.
func (p *HelmfilePlugin) ImageTags() []string {
	return []string{"v*.*.*", "sha256:*"}
}

func (p *HelmfilePlugin) GetPlanOperation() string  { return "diff" }
func (p *HelmfilePlugin) GetApplyOperation() string { return "apply" }

// olderReleaseTableBanners are the upper-case, colon-terminated banners
// Helmfile wrote before v1.6.0, before the "="-framed banner that
// parseReleaseTables reads. REINSTALLED RELEASES: appeared in v1.4.0 and a
// reinstalled release is listed only there, so a run that only reinstalled
// prints no other banner. Their presence anywhere in a run's transcript
// means the run's Release_Tables, if any, are in a format this Plugin does
// not parse, so its counts cannot be trusted: reporting zero would read as
// "no changes" when the run may have changed everything.
var olderReleaseTableBanners = []string{
	"UPDATED RELEASES:",
	"REINSTALLED RELEASES:",
	"DELETED RELEASES:",
	"FAILED RELEASES:",
	"FAILED TO DELETE RELEASES:",
}

// ChangesReported returns false when a successful run's counts cannot be
// read from output, the run's whole transcript, so a zero count from it
// would misreport a run that changed something as "no changes" rather than
// as succeeded with no counts (Requirement 5, Decision 2). For apply and
// sync that is so when:
//
//   - the transcript carries one of the older upper-case banners, which
//     parseReleaseTables cannot read; or
//   - the run's argv set Helmfile's log level above info (-q, --quiet or
//     --log-level warn and up), since Helmfile logs its Release_Tables at
//     info level and would have printed none.
//
// For diff only the second holds (fix-helmfile-diff-removals): Helmfile
// logs its "Affected releases are:" summary at info level too, so a quiet
// plan prints no removals, and its zero destroy count cannot be trusted.
// The older banners are an apply's, so they are not looked for here. Any
// other operation reports its counts.
//
// output is the transcript as the Server received it, both streams,
// rather than stderr alone: this is the one place that trade-off is
// accepted, since the older banner's text inside a stdout diff body (e.g.
// a ConfigMap holding it) can only hide a count, never invent one.
//
// It is the transcript as received, not the whole one: the Server calls
// this on the result's output, which the Runner cuts to 1 MiB, keeping
// the end and the leading annotation lines (fix-large-output). The argv
// annotation therefore always survives, and the banners print at the end
// of an apply; a banner printed only in a cut start would go unseen, and
// the run would read as reported.
func (p *HelmfilePlugin) ChangesReported(operation, output string) bool {
	switch operation {
	case "diff", "apply", "sync":
	default:
		return true
	}
	checkBanners := operation != "diff"
	for _, line := range strings.Split(stripANSI(output), "\n") {
		if argv, ok := commandAnnotation(line); ok && argsSilenceTables(argv) {
			return false
		}
		if !checkBanners {
			continue
		}
		trimmed := strings.TrimSpace(line)
		for _, banner := range olderReleaseTableBanners {
			if trimmed == banner {
				return false
			}
		}
	}
	return true
}

// commandAnnotation returns the arguments of helmfile's argv from turnip's
// command annotation line ("@@ turnip: helmfile ... @@"); ok is false for
// any other line.
func commandAnnotation(line string) (args []string, ok bool) {
	if !strings.HasPrefix(line, transcriptPrefix) {
		return nil, false
	}
	text := strings.TrimSuffix(strings.TrimPrefix(line, transcriptPrefix), transcriptSuffix)
	fields := strings.Fields(text)
	if len(fields) == 0 || fields[0] != "helmfile" {
		return nil, false
	}
	return fields[1:], true
}

// argsSilenceTables reports whether args set Helmfile's log level above
// info, where its Release_Tables and its "Affected releases are:" summary
// are not printed. It follows Helmfile's own precedence: --debug wins over
// --quiet, which wins over --log-level.
// Levels set through HELMFILE_QUIET or HELMFILE_LOG_LEVEL in the image are
// not visible here; the documentation says so.
func argsSilenceTables(args []string) bool {
	debug, quiet := false, false
	level := ""
	for i, arg := range args {
		if arg == "--" {
			break
		}
		switch arg {
		case "--debug", "--debug=true":
			debug = true
		case "--debug=false":
			debug = false
		case "-q", "--quiet", "-q=true", "--quiet=true":
			quiet = true
		case "-q=false", "--quiet=false":
			quiet = false
		case "--log-level":
			if i+1 < len(args) {
				level = args[i+1]
			}
		default:
			if v, found := strings.CutPrefix(arg, "--log-level="); found {
				level = v
			}
		}
	}
	switch {
	case debug:
		return false
	case quiet:
		return true
	}
	switch strings.ToLower(level) {
	case "warn", "error", "dpanic", "panic", "fatal":
		return true
	}
	return false
}

func (p *HelmfilePlugin) Execute(ctx context.Context, operation string, opts ExecuteOptions) (*ExecuteResult, error) {
	if !slices.Contains(p.GetOperations(), operation) {
		return nil, &UnsupportedOperationError{
			Plugin:    p.Name(),
			Operation: operation,
			Supported: p.GetOperations(),
		}
	}

	// --no-color, so the codes of Helmfile's colored banners (the release
	// tables after apply and sync) do not reach the comment and the logs as
	// text. A flag rather than NO_COLOR in the environment, so the command
	// line turnip annotates shows why the output has no color. It comes
	// first: Helmfile refuses --color beside it, with its own message.
	args := []string{"--no-color"}
	if env := opts.Config["environment"]; env != "" {
		args = append(args, "--environment", env)
	}
	args = append(args, operation)
	args = append(args, opts.ExtraArgs...)

	rec := opts.Record
	if rec == nil {
		rec = NewRecord()
	}
	exitCode, err := p.run(ctx, opts.WorkingDir, "helmfile", args, opts.ToolVersion, rec)
	rec.Close()
	if err != nil {
		return nil, err
	}

	// Counts are read from the record, one stream at a time, whatever the
	// output below shows.
	var summary ChangeSummary
	if operation == p.GetPlanOperation() {
		// stdout only. Helmfile writes "Comparing release=" and each
		// release's diff there, and everything it says itself ("Building
		// dependency", "Adding repo", a failed helm diff) to stderr, which
		// would otherwise read as the body of whichever release it landed
		// after and count an unchanged one as changed.
		summary.Change = parseChangedReleases(strings.TrimRight(rec.StreamText(Stdout), "\n"))
		// A release Helmfile will remove has no diff at all: it appears
		// only in the "Affected releases are:" summary its logger prints
		// to stderr, so that is where Destroy is read, and only there
		// (the same text inside a manifest on stdout must not count).
		summary.Destroy = parseAffectedReleases(rec.StreamText(Stderr))
	} else {
		// Every other operation (apply, sync) actually mutates, and
		// Helmfile prints its own account of what it changed as
		// Release_Tables on stderr; stdout carries arbitrary diff bodies
		// and manifest content that must not be read as one (see
		// parseReleaseTables and the design's Decision 1).
		summary = parseReleaseTables(rec.StreamText(Stderr))
	}

	// The whole record when the operation succeeded. When it failed,
	// standard error alone: that is where Helmfile reports its progress
	// and every error, while standard output holds the diffs of a plan
	// that cannot be applied, megabytes of them on a first install, with
	// the errors somewhere after.
	output := rec.Text()
	if exitCode != 0 {
		output = rec.Select(Stderr, omittedStdoutNote)
	}

	return &ExecuteResult{
		Output:        output,
		ChangeSummary: summary,
		PlanData:      nil,
		ExitCode:      exitCode,
		Error:         nil,
	}, nil
}

// omittedStdoutNote words the line a failed operation's output carries in
// place of its standard output; no line when there was none to omit.
func omittedStdoutNote(omitted int) string {
	switch omitted {
	case 0:
		return ""
	case 1:
		return "1 line of standard output omitted because the operation failed"
	default:
		return fmt.Sprintf("%d lines of standard output omitted because the operation failed", omitted)
	}
}

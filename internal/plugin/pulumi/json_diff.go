package pulumi

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// TODO: Use https://github.com/pulumi/pulumi/blob/e13780c0bd60fa5f8fda011e8221b1b956b97738/pkg/display/json.go

type diff struct {
	Steps         []step        `json:"steps"`
	Duration      time.Duration `json:"duration"`
	ChangeSummary changeSummary `json:"changeSummary"`
}

type step struct {
	Op       `json:"op"`
	URN      string   `json:"urn"`
	NewState newState `json:"newState"`
}

type Op string

const (
	opCreate Op = "create"
	opDelete Op = "delete"
	opModify Op = "modify"
)

type newState struct {
	URN       string         `json:"urn"`
	StateType string         `json:"type"`
	Inputs    map[string]any `json:"inputs"`
}

type changeSummary struct {
	Create int `json:"create"`
	Delete int `json:"delete"`
}

type Formatter struct {
	input []byte
}

func NewFormatter(input []byte) *Formatter {
	return &Formatter{input}
}

func (f *Formatter) Format() (string, error) {
	d := new(diff)
	err := json.Unmarshal(f.input, &d)
	if err != nil {
		log.Error("error parsing diff", "err", err, "input", string(f.input))
		return fmt.Sprintf("```\n%s\n```", string(f.input)), nil
	}
	log.Debug("parsed diff", "diff", d)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Changes: %d created, %d deleted\n", d.ChangeSummary.Create, d.ChangeSummary.Delete))
	sb.WriteString(fmt.Sprintf("Duration: %s\n\n```diff\n", d.Duration))

	if len(d.Steps) == 0 {
		sb.WriteString("No changes")
	}
	for _, s := range d.Steps {
		switch s.Op {
		case "same":
			continue
		case "create":
			sb.WriteString("+ ")
		case "delete":
			sb.WriteString("- ")
		case "modify":
			sb.WriteString("~ ")
		}
		sb.WriteString(fmt.Sprintf("%s:\n", s.NewState.StateType))
		sb.WriteString(fmt.Sprintf("\t[urn=%s]\n", s.NewState.URN))
		for k, v := range s.NewState.Inputs {
			if strings.HasPrefix(k, "__") {
				continue
			}
			sb.WriteString(fmt.Sprintf("\t%s: %s\n", k, formatInputValue(v, "\t")))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("```")

	return sb.String(), nil
}

func formatInputValue(value any, tabs string) string {
	switch value.(type) {
	case string:
		var sb strings.Builder
		for i, l := range strings.Split(value.(string), "\n") {
			if i == 0 {
				sb.WriteString(l)
			} else {
				sb.WriteString(fmt.Sprintf("\n%s%s", tabs, l))
			}
		}
		return sb.String()
	case []any:
		var sb strings.Builder
		sb.WriteString("[\n")
		for i, v := range value.([]any) {
			sb.WriteString(fmt.Sprintf("\t%s%s", tabs, formatInputValue(v, tabs+"\t")))
			if i < len(value.([]any))-1 {
				sb.WriteString(",\n")
			}
		}
		sb.WriteString(fmt.Sprintf("\n%s]", tabs))
		return sb.String()
	default:
		return fmt.Sprintf("%v", value)
	}
}

package pulumi

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TODO: Use https://github.com/pulumi/pulumi/blob/e13780c0bd60fa5f8fda011e8221b1b956b97738/pkg/display/json.go

type diff struct {
	steps         []step        `json:"steps"`
	duration      time.Duration `json:"duration"`
	changeSummary changeSummary `json:"changeSummary"`
}

type step struct {
	op       string   `json:"op"`
	urn      string   `json:"urn"`
	newState newState `json:"newState"`
}

type op string

const (
	opCreate op = "create"
	opDelete op = "delete"
	opModify op = "modify"
)

type newState struct {
	urn       string         `json:"urn"`
	stateType string         `json:"type"`
	inputs    map[string]any `json:"inputs"`
}

type changeSummary struct {
	create int `json:"create"`
	delete int `json:"delete"`
}

type Formatter struct {
	input []byte
}

func NewFormatter(input []byte) *Formatter {
	return &Formatter{input: input}
}

func (f *Formatter) Format() (string, error) {
	var d diff
	err := json.Unmarshal(f.input, &d)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, s := range d.steps {
		switch s.op {
		case "same":
			continue
		case "create":
			sb.WriteString("+ ")
		case "delete":
			sb.WriteString("- ")
		case "modify":
			sb.WriteString("~ ")
		}
		sb.WriteString(fmt.Sprintf("%s: (%s)\n", s.newState.stateType, s.op))
		sb.WriteString(fmt.Sprintf("\t[urn=%s]\n", s.newState.urn))
		for k, v := range s.newState.inputs {
			if strings.HasPrefix(k, "__") {
				continue
			}
			sb.WriteString(fmt.Sprintf("\t%s: %v\n", k, v))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("Changes: %d created, %d deleted\n", d.changeSummary.create, d.changeSummary.delete))
	sb.WriteString(fmt.Sprintf("Duration: %s\n", d.duration))

	return sb.String(), nil
}

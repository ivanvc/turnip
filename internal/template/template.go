package template

import (
	"bytes"
	"html/template"

	sprig "github.com/go-task/slim-sprig"

	"github.com/ivanvc/turnip/internal/yaml"
)

type Environment struct {
	Stack       string
	Workspace   string
	Environment string
	ProjectDir  string
}

type Template struct {
	Environment
}

func New(project yaml.Project) Template {
	return Template{
		Environment: Environment{
			Stack:       project.Stack,
			Workspace:   project.Workspace,
			Environment: project.Environment,
			ProjectDir:  project.Dir,
		},
	}
}

func (t Template) Execute(input string) (string, error) {
	tpl, err := template.New("template").Funcs(sprig.TxtFuncMap()).Parse(input)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, t.Environment); err != nil {
		return "", err
	}
	return buf.String(), nil
}

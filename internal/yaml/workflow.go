package yaml

import (
	"fmt"
	"reflect"
	"slices"
)

type Workflow struct {
	Terraform *TerraformAdapter `yaml:"terraform"`
	Helmfile  *HelmfileAdapter  `yaml:"helmfile"`
	Pulumi    *PulumiAdapter    `yaml:"pulumi"`

	PodAnnotations map[string]string `yaml:"podAnnotations"`
	Env            map[string]string `yaml:"env"`
	InitCommands   []Command         `yaml:"initCommands"`
}

func (w Workflow) GetAdapter() (Adapter, error) {
	adapters := []Adapter{w.Pulumi, w.Terraform, w.Helmfile}
	adapters = slices.DeleteFunc(adapters, func(a Adapter) bool {
		return reflect.ValueOf(a).IsNil()
	})
	if len(adapters) == 0 {
		return nil, fmt.Errorf("no adapters set must be one of Pulumi, Terraform, or Helmfile")
	}
	if len(adapters) > 1 {
		return nil, fmt.Errorf("multiple adapters set must be only one of Pulumi, Terraform, or Helmfile")
	}
	return adapters[0], nil
}

func (w Workflow) Validate() error {
	adapter, err := w.GetAdapter()
	if err != nil {
		return err
	}
	if err := adapter.Validate(); err != nil {
		return fmt.Errorf("adapter %s: %s", adapter.GetName(), err.Error())
	}

	return nil
}

func (w Workflow) GetEnv() []string {
	return envToSlice(w.Env)
}

package yaml

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/containers/image/v5/docker"
	"github.com/distribution/reference"
)

type Workflow struct {
	Terraform *TerraformAdapter `yaml:"terraform"`
	Helmfile  *HelmfileAdapter  `yaml:"helmfile"`
	Pulumi    *PulumiAdapter    `yaml:"pulumi"`

	Image          string            `yaml:"image"`
	PodAnnotations map[string]string `yaml:"podAnnotations"`
	Env            map[string]string `yaml:"env"`
	InitCommands   []Command         `yaml:"initCommands"`
}

func (w Workflow) GetAdapter() (Adapter, error) {
	adapters := []Adapter{w.Pulumi, w.Terraform, w.Helmfile}
	adapters = slices.DeleteFunc(adapters, func(a Adapter) bool {
		return a == nil
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

	image := w.Image
	if image == "" {
		return errors.New("image not set")
	}
	if !strings.HasPrefix(image, "//") {
		image = "//" + image
	}
	ref, err := docker.ParseReference(image)
	if err != nil {
		return fmt.Errorf("invalid image %s: %s", w.Image, err.Error())
	}
	if reference.Path(ref.DockerReference()) != "alpine" && reference.Path(ref.DockerReference()) != "debian" {
		return fmt.Errorf("invalid image %s: must be alpine or debian", w.Image)
	}

	return nil
}

func (w Workflow) GetEnv() []string {
	return envToSlice(w.Env)
}

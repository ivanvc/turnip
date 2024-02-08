package yaml

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds the turnip.yaml configuration
type Config struct {
	Projects  []Project           `yaml:"projects"`
	Workflows map[string]Workflow `yaml:"workflows"`
	Version   string              `yaml:"version"`
}

func Load(data []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	for i, p := range cfg.Projects {
		if w, ok := cfg.Workflows[p.Workflow]; ok {
			cfg.Projects[i].LoadedWorkflow = w
		} else {
			return cfg, fmt.Errorf("workflow %s not found", p.Workflow)
		}
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.Version != "v1alpha1" {
		return fmt.Errorf("unsupported turnip.yaml version: %s", c.Version)
	}

	for _, p := range c.Projects {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("project %s: %s", p.Dir, err.Error())
		}
		if _, ok := c.Workflows[p.Workflow]; !ok {
			return fmt.Errorf("project %s: workflow %s not found", p.Dir, p.Workflow)
		}
	}

	for name, w := range c.Workflows {
		if err := w.Validate(); err != nil {
			return fmt.Errorf("workflow %s: %s", name, err.Error())
		}
	}

	return nil
}

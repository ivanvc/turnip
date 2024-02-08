package yaml

type Command struct {
	Env        map[string]string `yaml:"env"`
	Run        string            `yaml:"run"`
	Pulumi     string            `yaml:"login"`
	AWS        string            `yaml:"aws"`
	OmitOutput bool              `yaml:"omitOutput"`
}

func (c Command) GetEnv() []string {
	return envToSlice(c.Env)
}

func envToSlice(env map[string]string) []string {
	output := make([]string, 0)
	for k, v := range env {
		output = append(output, k+"="+v)
	}
	return output
}

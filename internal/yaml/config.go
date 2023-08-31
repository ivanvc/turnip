package yaml

// Config holds the turnip.yaml configuration
type Config struct {
	Projects []Project `yaml:"projects"`
}

type Project struct {
	Dir string `yaml:"dir"`
}

package config

import "testing"

func TestParse_ConfigMapRoundTrips(t *testing.T) {
	data := []byte(`
version: 1
projects:
  - name: vpc
    directory: infra/vpc
    tool: terraform
    config:
      workspace: prod
      region: us-east-1
`)

	c, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	got := c.Projects[0].Config
	want := map[string]string{"workspace": "prod", "region": "us-east-1"}
	if len(got) != len(want) {
		t.Fatalf("Config = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Config[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestParse_ConfigMapAbsentIsNil(t *testing.T) {
	data := []byte(`
version: 1
projects:
  - name: vpc
    directory: infra/vpc
    tool: terraform
`)

	c, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if c.Projects[0].Config != nil {
		t.Errorf("Config = %v, want nil", c.Projects[0].Config)
	}
}

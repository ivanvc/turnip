package handlers

import "testing"

func TestPathMatchingForProjectRules(t *testing.T) {
	tt := []struct {
		dir   string
		rules []string
		path  string
	}{
		{"infra", []string{"./**/*"}, "infra/main.go"},
		{"infra", []string{"../infra.yaml", "./*.go"}, "infra.yaml"},
		{"infra", []string{"../infra.yaml", "./*.go"}, "infra/main.go"},
	}
	for _, tc := range tt {
		t.Run(tc.dir, func(t *testing.T) {
			if ok, err := doesPathMatchProjectRules(tc.dir, tc.path, tc.rules); err != nil {
				t.Errorf("Error checking path: %v", err)
			} else if !ok {
				t.Errorf("Expected path to match rules, rules: %v, path: %v", tc.rules, tc.path)
			}
		})
	}
}

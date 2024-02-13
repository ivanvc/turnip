package plugin

import "testing"

func TestFormatLine(t *testing.T) {
	tt := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{" -- output --", " -- output --"},
		{"  --output", "--  output"},
		{" +-+output", " +-+output"},
		{"  ~output", "~  output"},
		{" output ", " output "},
		{"output", "output"},
	}
	for _, tc := range tt {
		t.Run(tc.input, func(t *testing.T) {
			actual := formatLine(tc.input)
			if actual != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}

func TestProcessOutput(t *testing.T) {
	input := `Previewing update (dev)
Installing dependencies:
npm WARN deprecated har-validator@5.1.5: this library is no longer supported
npm WARN deprecated request@2.88.2: request has been deprecated, see
Finished installing dependencies
@loading
  -- output --
  -something
@loading`
	expected := `Previewing update (dev)
  -- output --
-  something
`
	out := processOutput([]byte(input))
	if string(out) != expected {
		t.Errorf("expected %q, got %q", expected, string(out))
	}
}

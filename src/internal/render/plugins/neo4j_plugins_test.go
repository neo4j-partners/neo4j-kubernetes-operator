package plugins

import "testing"

func TestNEO4JPluginsEnv(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want string
	}{
		{name: "empty", ids: nil, want: ""},
		{name: "apoc", ids: []string{"apoc"}, want: `["apoc"]`},
		{name: "gds maps to graph-data-science", ids: []string{"gds"}, want: `["graph-data-science"]`},
		{name: "multiple sorted", ids: []string{"gds", "apoc"}, want: `["apoc","graph-data-science"]`},
		{name: "dedupe", ids: []string{"apoc", "apoc"}, want: `["apoc"]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NEO4JPluginsEnv(tc.ids); got != tc.want {
				t.Fatalf("NEO4JPluginsEnv() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAssigned(t *testing.T) {
	if !Assigned([]string{"apoc", "gds"}, "apoc") {
		t.Fatal("expected apoc assigned")
	}
	if Assigned([]string{"apoc"}, "gds") {
		t.Fatal("expected gds not assigned")
	}
}

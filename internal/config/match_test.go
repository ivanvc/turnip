package config

import (
	"reflect"
	"testing"
)

func TestMatchProjects_SinglePatternMatch(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/vpc/*.tf"}},
	}
	modified := []string{"infra/vpc/main.tf"}

	got := MatchProjects(projects, modified)
	if len(got) != 1 || got[0].Name != "vpc" {
		t.Errorf("MatchProjects = %v, want [vpc]", got)
	}
}

func TestMatchProjects_RecursiveGlob(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infrastructure/vpc/**/*.tf"}},
	}
	modified := []string{"infrastructure/vpc/nested/deep/main.tf"}

	got := MatchProjects(projects, modified)
	if len(got) != 1 || got[0].Name != "vpc" {
		t.Errorf("MatchProjects = %v, want [vpc]", got)
	}
}

func TestMatchProjects_NoMatch(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/vpc/*.tf"}},
	}
	modified := []string{"infra/rds/main.tf"}

	got := MatchProjects(projects, modified)
	if got != nil {
		t.Errorf("MatchProjects = %v, want nil", got)
	}
}

func TestMatchProjects_MultiplePatternsOnlyOneMatches(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/rds/*.tf", "infra/vpc/*.tf"}},
	}
	modified := []string{"infra/vpc/main.tf"}

	got := MatchProjects(projects, modified)
	if len(got) != 1 || got[0].Name != "vpc" {
		t.Errorf("MatchProjects = %v, want [vpc]", got)
	}
}

func TestMatchProjects_ProjectsAreIndependent(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/vpc/*.tf"}},
		{Name: "rds", WhenModified: []string{"infra/rds/*.tf"}},
	}
	modified := []string{"infra/vpc/main.tf"}

	got := MatchProjects(projects, modified)
	if len(got) != 1 || got[0].Name != "vpc" {
		t.Errorf("MatchProjects = %v, want [vpc] only", got)
	}
}

func TestMatchProjects_DeterministicOrdering(t *testing.T) {
	projects := []Project{
		{Name: "b", WhenModified: []string{"infra/b/*.tf"}},
		{Name: "a", WhenModified: []string{"infra/a/*.tf"}},
	}
	modified := []string{"infra/b/main.tf", "infra/a/main.tf"}

	got := MatchProjects(projects, modified)
	want := []string{"b", "a"}
	var gotNames []string
	for _, p := range got {
		gotNames = append(gotNames, p.Name)
	}
	if !reflect.DeepEqual(gotNames, want) {
		t.Errorf("order = %v, want %v", gotNames, want)
	}
}

func TestMatchProjects_PathNormalization(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/vpc/*.tf"}},
	}

	tests := []string{
		"./infra/vpc/main.tf",
		`infra\vpc\main.tf`,
	}
	for _, f := range tests {
		got := MatchProjects(projects, []string{f})
		if len(got) != 1 {
			t.Errorf("MatchProjects(%q) = %v, want match", f, got)
		}
	}
}

func TestMatchProjects_EmptyModifiedFiles(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/vpc/*.tf"}},
	}

	got := MatchProjects(projects, nil)
	if got != nil {
		t.Errorf("MatchProjects = %v, want nil", got)
	}
}

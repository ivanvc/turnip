package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchProjects_SinglePatternMatch(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/vpc/*.tf"}},
	}
	modified := []string{"infra/vpc/main.tf"}

	got := MatchProjects(projects, modified)
	require.Len(t, got, 1)
	assert.Equal(t, "vpc", got[0].Name)
}

func TestMatchProjects_RecursiveGlob(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infrastructure/vpc/**/*.tf"}},
	}
	modified := []string{"infrastructure/vpc/nested/deep/main.tf"}

	got := MatchProjects(projects, modified)
	require.Len(t, got, 1)
	assert.Equal(t, "vpc", got[0].Name)
}

func TestMatchProjects_NoMatch(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/vpc/*.tf"}},
	}
	modified := []string{"infra/rds/main.tf"}

	got := MatchProjects(projects, modified)
	assert.Nil(t, got)
}

func TestMatchProjects_MultiplePatternsOnlyOneMatches(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/rds/*.tf", "infra/vpc/*.tf"}},
	}
	modified := []string{"infra/vpc/main.tf"}

	got := MatchProjects(projects, modified)
	require.Len(t, got, 1)
	assert.Equal(t, "vpc", got[0].Name)
}

func TestMatchProjects_ProjectsAreIndependent(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/vpc/*.tf"}},
		{Name: "rds", WhenModified: []string{"infra/rds/*.tf"}},
	}
	modified := []string{"infra/vpc/main.tf"}

	got := MatchProjects(projects, modified)
	require.Len(t, got, 1)
	assert.Equal(t, "vpc", got[0].Name)
}

func TestMatchProjects_DeterministicOrdering(t *testing.T) {
	projects := []Project{
		{Name: "b", WhenModified: []string{"infra/b/*.tf"}},
		{Name: "a", WhenModified: []string{"infra/a/*.tf"}},
	}
	modified := []string{"infra/b/main.tf", "infra/a/main.tf"}

	got := MatchProjects(projects, modified)
	var gotNames []string
	for _, p := range got {
		gotNames = append(gotNames, p.Name)
	}
	assert.Equal(t, []string{"b", "a"}, gotNames)
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
		assert.Lenf(t, got, 1, "MatchProjects(%q)", f)
	}
}

func TestMatchProjects_EmptyModifiedFiles(t *testing.T) {
	projects := []Project{
		{Name: "vpc", WhenModified: []string{"infra/vpc/*.tf"}},
	}

	got := MatchProjects(projects, nil)
	assert.Nil(t, got)
}

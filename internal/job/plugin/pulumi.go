package plugin

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"gopkg.in/yaml.v3"

	intyaml "github.com/ivanvc/turnip/internal/yaml"
)

type Pulumi struct {
	project  intyaml.Project
	execPath string
}

const downloadURL = "https://github.com/pulumi/pulumi/releases/download/v%s/pulumi-v%s-linux-x64.tar.gz"

func (p Pulumi) InstallDependencies(dest, repoDir string) ([]byte, error) {
	// TODO: Get version from "VersionFrom" or skip install if "SkipInstall" is true
	adapter, err := p.project.LoadedWorkflow.GetAdapter()
	if err != nil {
		log.Error("error getting adapter", "err", err)
		return []byte{}, err
	}

	version := adapter.GetVersion()

	wd, err := os.Getwd()
	if err != nil {
		log.Error("error getting working directory", "err", err)
		return []byte{}, err
	}
	defer os.Chdir(wd)

	if err := os.Chdir(dest); err != nil {
		log.Error("error changing directory", "err", err)
		return []byte{}, err
	}

	resp, err := http.Get(fmt.Sprintf(downloadURL, version, version))
	if err != nil {
		log.Error("error downloading", "err", err)
		return []byte{}, err
	}

	filePath := path.Join(dest, "pulumi.tgz")
	out, err := os.Create(filePath)
	if err != nil {
		log.Error("error creating file", "err", err)
		return []byte{}, err
	}
	defer out.Close()

	if _, err = io.Copy(out, resp.Body); err != nil {
		log.Error("error writing download", "err", err)
		return []byte{}, err
	}

	output := new(bytes.Buffer)
	cmd := exec.Command("tar", "zxf", "pulumi.tgz")
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		log.Error("error executing", "err", err, "cmd", cmd)
		return output.Bytes(), err
	}

	files, err := filepath.Glob(filepath.Join(dest, "pulumi", "*"))
	if err != nil {
		log.Error("error globbing", "err", err)
		return output.Bytes(), err
	}
	for _, file := range files {
		if err := copyFile(file, "/opt/turnip/bin"); err != nil {
			log.Error("error copying", "err", err)
			return output.Bytes(), err
		}
	}

	yamlFile := filepath.Join(repoDir, p.project.Dir, "Pulumi.yaml")
	return output.Bytes(), installRuntime(yamlFile, output)
}

func installRuntime(inputYAML string, buf *bytes.Buffer) error {
	f, err := os.Open(inputYAML)
	if err != nil {
		log.Error("error opening Pulumi.yaml", "err", err, "file", inputYAML)
		return err
	}

	b, err := io.ReadAll(f)
	if err != nil {
		log.Error("error reading Pulumi.yaml", "err", err, "file", inputYAML)
		return err
	}

	var yml struct {
		Runtime struct {
			Name string `yaml:"name"`
		} `yaml:"runtime"`
	}

	if err := yaml.Unmarshal(b, &yml); err != nil {
		log.Error("error unmarshalling Pulumi.yaml", "err", err)
		return err
	}

	args := []string{"add", "--no-cache", "git"}

	switch yml.Runtime.Name {
	case "python":
		args = append(args, "python3")
	}

	cmd := exec.Command("apk", args...)
	cmd.Stdout = buf
	cmd.Stderr = buf
	if err := cmd.Run(); err != nil {
		log.Error("error installing runtime", "err", err, "cmd", cmd)
		return err
	}

	return nil
}

func copyFile(src, dest string) error {
	srcFileStat, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !srcFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination := path.Join(dest, filepath.Base(src))
	destinationFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer destinationFile.Close()
	log.Debug("copying file", "src", src, "dest", destination)

	_, err = io.Copy(destinationFile, source)
	if err != nil {
		return err
	}

	return os.Chmod(destination, srcFileStat.Mode())
}

func (p Pulumi) Lift(repoDir string) (bool, []byte, error) {
	return p.runCommand("up", repoDir)
}

func (p Pulumi) Plot(repoDir string) (bool, []byte, error) {
	return p.runCommand("preview", repoDir)
}

func (p Pulumi) runCommand(command, repoDir string) (bool, []byte, error) {
	wd, err := os.Getwd()
	if err != nil {
		log.Error("error getting working directory", "err", err)
		return false, []byte{}, err
	}
	defer os.Chdir(wd)

	dir := filepath.Join(repoDir, p.project.Dir)
	if err := os.Chdir(dir); err != nil {
		log.Error("error changing directory", "err", err)
		return false, []byte{}, err
	}
	log.Info("changed directory", "dir", dir)

	output := new(bytes.Buffer)
	args := []string{
		"--non-interactive",
		"--color",
		"never",
		command,
		"--diff",
		"--stack",
		p.project.Stack,
	}
	if command == "up" {
		args = append(args, "--yes")
		args = append(args, "--skip-preview")
	}
	cmd := exec.Command("pulumi", args...)

	cmd.Stdout = output
	cmd.Stderr = output

	log.Debug("running pulumi preview", "cmd", cmd)

	if err := cmd.Run(); err != nil {
		log.Error("error running pulumi preview", "err", err, "output", output.String())
		return false, output.Bytes(), err
	}
	log.Debug("pulumi preview output", "exitCode", cmd.ProcessState.ExitCode())

	if cmd.ProcessState.ExitCode() != 0 {
		return true, output.Bytes(), nil
	}

	return false, processOutput(output.Bytes()), nil
}

func processOutput(in []byte) []byte {
	out := new(bytes.Buffer)
	skipLines := true
	s := bufio.NewScanner(bytes.NewReader(in))
	for s.Scan() {
		if strings.HasPrefix(s.Text(), "Finished installing dependencies") {
			skipLines = false
			continue
		}
		if skipLines || strings.HasPrefix(s.Text(), "@") {
			continue
		}
		spaces := 0
		prefix := ""
		text := strings.TrimLeftFunc(s.Text(), func(r rune) bool {
			switch r {
			case ' ':
				spaces++
			case '-', '+', '~', '=', '>', '<':
				prefix += string(r)
			default:
				return false
			}
			return true
		})
		out.WriteString(prefix)
		out.WriteString(strings.Repeat(" ", spaces))
		out.WriteString(text)
		out.WriteString("\n")
	}
	return out.Bytes()
}

func (p Pulumi) RunInitCommands(repoDir string) ([]byte, error) {
	output := make([]byte, 0)
	proj := p.project

	for _, cmd := range proj.LoadedWorkflow.InitCommands {
		log.Info("running init command", "cmd", cmd)
		var fields []string
		if len(cmd.Run) > 0 {
			fields = strings.Fields(cmd.Run)
		} else if len(cmd.Pulumi) > 0 {
			fields = []string{"pulumi", "--non-interactive"}
			fields = append(fields, strings.Fields(cmd.Pulumi)...)
		} else {
			continue
		}

		c := exec.Command(fields[0], fields[1:]...)
		c.Env = append(c.Environ(), cmd.GetEnv()...)
		c.Env = append(c.Environ(), proj.LoadedWorkflow.GetEnv()...)
		for k, v := range p.project.LoadedWorkflow.Env {
			c.Env = append(c.Env, fmt.Sprintf("%s=%s", k, v))
		}
		log.Info("running command", "cmd", c, "env", c.Env)

		out, err := c.CombinedOutput()
		if err != nil {
			log.Error("error running command", "err", err)
		}
		log.Info("command output", "output", string(out))

		if !cmd.OmitOutput {
			output = append(output, out...)
		}

		if c.ProcessState.ExitCode() != 0 {
			return output, fmt.Errorf("command %s exited with code %d", cmd.Run, c.ProcessState.ExitCode())
		}
	}

	return output, nil
}

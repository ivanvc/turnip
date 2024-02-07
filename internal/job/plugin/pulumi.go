package plugin

import (
	"errors"
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

type pulumiYAML struct {
	Runtime struct {
		Name string `yaml:"name"`
	} `yaml:"runtime"`
}

const downloadURL = "https://github.com/pulumi/pulumi/releases/download/v%s/pulumi-v%s-linux-x64.tar.gz"

func (p Pulumi) Install(dest, repoDir string) (string, error) {
	// TODO: Get version either from requirements.txt, go.mod, or package.json, .tool-versions, etc.
	version := p.project.LoadedWorkflow.Version
	if version == "" {
		return "", errors.New("no version specified, version guessing not implemented yet")
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Error("error getting working directory", "err", err)
		return "", err
	}
	defer os.Chdir(wd)

	if err := os.Chdir(dest); err != nil {
		log.Error("error changing directory", "err", err)
		return "", err
	}

	resp, err := http.Get(fmt.Sprintf(downloadURL, version, version))
	if err != nil {
		log.Error("error downloading", "err", err)
		return "", err
	}

	filePath := path.Join(dest, "pulumi.tgz")
	out, err := os.Create(filePath)
	if err != nil {
		log.Error("error creating file", "err", err)
		return "", err
	}
	defer out.Close()

	if _, err = io.Copy(out, resp.Body); err != nil {
		log.Error("error writing download", "err", err)
		return "", err
	}

	cmd := exec.Command("tar", "zxf", "pulumi.tgz")
	if err := cmd.Run(); err != nil {
		log.Error("error executing", "err", err, "cmd", cmd)
		return "", err
	}

	files, err := filepath.Glob(filepath.Join(dest, "pulumi", "*"))
	if err != nil {
		log.Error("error globbing", "err", err)
		return "", err
	}
	for _, file := range files {
		log.Info("copying file", "file", file)
		if err := copyFile(file, "/opt/turnip/bin"); err != nil {
			log.Error("error copying", "err", err)
			return "", err
		}
	}

	return "", installRuntime(filepath.Join(repoDir, p.project.Dir, "Pulumi.yaml"))
}

func installRuntime(inputYAML string) error {
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

	var yml pulumiYAML
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
	log.Info("copying file", "src", src, "dest", destination)

	_, err = io.Copy(destinationFile, source)
	if err != nil {
		return err
	}

	return os.Chmod(destination, srcFileStat.Mode())
}

func (p Pulumi) Plan(binDir, repoDir string) (bool, []byte, error) {
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

	cmd := exec.Command(
		"pulumi",
		"--non-interactive",
		"preview",
		"--json",
		"--stack",
		p.project.Stack,
	)

	cmd.Env = append(cmd.Environ(), "")
	for k, v := range p.project.LoadedWorkflow.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	log.Info("running pulumi preview", "cmd", cmd, "env", cmd.Env)

	out, err := cmd.Output()
	//out, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("error running pulumi preview", "err", err, "output", string(out))
		return false, out, err
	}
	log.Info("pulumi preview output", "output", string(out), "exitCode", cmd.ProcessState.ExitCode())

	return cmd.ProcessState.ExitCode() != 0, out, nil
}

func (p Pulumi) RunPreCommands(repoDir string, cmds []intyaml.Command) ([]byte, error) {
	output := make([]byte, 0)

	for _, cmd := range cmds {
		var fields []string
		if len(cmd.Run) > 0 {
			fields = strings.Fields(cmd.Run)
		} else if len(cmd.Login) > 0 {
			fields = []string{"pulumi", "login", cmd.Login}
		} else {
			return output, fmt.Errorf("no command or login specified")
		}

		c := exec.Command(fields[0], fields[1:]...)
		c.Env = append(c.Environ(), "")
		for k, v := range cmd.Env {
			c.Env = append(c.Env, fmt.Sprintf("%s=%s", k, v))
		}
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

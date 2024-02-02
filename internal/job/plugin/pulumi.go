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

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/yaml"
)

type Pulumi struct {
	project  yaml.Project
	execPath string
}

const downloadURL = "https://github.com/pulumi/pulumi/releases/download/v%s/pulumi-v%s-linux-x64.tar.gz"

func (p Pulumi) Install(dest string) (string, error) {
	// TODO: Get version either from requirements.txt, go.mod, or package.json, .tool-versions, etc.
	if p.project.Version == "" {
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

	resp, err := http.Get(fmt.Sprintf(downloadURL, p.project.Version, p.project.Version))
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

	return path.Join(dest, "pulumi"), nil
}

func (p Pulumi) Plan(binDir, repoDir string) ([]byte, error) {
	wd, err := os.Getwd()
	if err != nil {
		log.Error("error getting working directory", "err", err)
		return []byte{}, err
	}
	defer os.Chdir(wd)

	if err := os.Chdir(filepath.Join(repoDir, p.project.Dir)); err != nil {
		log.Error("error changing directory", "err", err)
		return []byte{}, err
	}

	cmd := exec.Command(
		"pulumi",
		"--non-interactive",
		"--color=never",
		"preview",
		"--stack",
		p.project.Stack,
	)

	cmd.Env = append(
		cmd.Environ(),
		fmt.Sprintf("PATH=%s:%s", binDir, os.ExpandEnv("$PATH")),
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Error("error getting stdout pipe", "err", err)
		return []byte{}, err
	}

	if err := cmd.Start(); err != nil {
		log.Error("error starting command", "err", err)
		return []byte{}, err
	}

	out, err := io.ReadAll(stdout)
	if err != nil {
		log.Error("error reading stdout", "err", err)
		return []byte{}, err
	}

	if err := cmd.Wait(); err != nil {
		log.Error("error waiting for command", "err", err)
		return []byte{}, err
	}

	return out, nil
}

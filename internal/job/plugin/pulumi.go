package plugin

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/yaml"
)

type Pulumi struct {
	project  yaml.Project
	execPath string
}

const downloadURL = "https://github.com/pulumi/pulumi/releases/download/v%s/pulumi-v%s-linux-x64.tar.gz"

func (p Pulumi) InstallTool(dest, repoDir string) error {
	// TODO: Get version either from requirements.txt, go.mod, or package.json, .tool-versions, etc.
	if p.project.Version == "" {
		return errors.New("no version specified, version guessing not implemented yet")
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Error("error getting working directory", "err", err)
		return err
	}
	defer os.Chdir(wd)

	if err := os.Chdir(dest); err != nil {
		log.Error("error changing directory", "err", err)
		return err
	}

	resp, err := http.Get(fmt.Sprintf(downloadURL, p.project.Version, p.project.Version))
	if err != nil {
		log.Error("error downloading", "err", err)
		return err
	}

	filePath := path.Join(dest, "pulumi.tgz")
	out, err := os.Create(filePath)
	if err != nil {
		log.Error("error creating file", "err", err)
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, resp.Body); err != nil {
		log.Error("error writing download", "err", err)
		return err
	}

	cmd := exec.Command("tar", "zxf", "pulumi.tgz")
	if err := cmd.Run(); err != nil {
		log.Error("error executing", "err", err, "cmd", cmd)
		return err
	}

	(&p).execPath = path.Join(wd, "pulumi/")

	return nil
}

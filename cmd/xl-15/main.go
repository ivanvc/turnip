package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"

	"github.com/ivanvc/turnip/internal/job/commands"
	intgit "github.com/ivanvc/turnip/internal/job/git"
	yamlconfig "github.com/ivanvc/turnip/internal/yaml"
	pb "github.com/ivanvc/turnip/pkg/turnip"
)

func main() {
	if os.Getenv("TURNIP_PROJECT") == "" {
		return
	}

	conn, err := grpc.Dial(fmt.Sprintf("%s:50001", os.Getenv("TURNIP_SERVER_NAME")), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	cli := pb.NewTurnipClient(conn)
	log.Info("Connected to server", "client", cli)

	var project yamlconfig.Project
	if err := yaml.Unmarshal([]byte(os.Getenv("TURNIP_PROJECT")), &project); err != nil {
		log.Fatal("error unmarshalling project", "error", err)
	}

	tmpDir, err := os.MkdirTemp("", "turnip-repo-*")
	if err != nil {
		log.Fatal("error creating temp dir", "error", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.Mkdir("repo", 0750); err != nil && !os.IsExist(err) {
		log.Fatal("Error creating repo dir", "error", err)
	}
	repoDir := filepath.Join(tmpDir, "repo")

	if err := intgit.Clone(
		repoDir,
		os.Getenv("TURNIP_CLONE_URL"),
		os.Getenv("TURNIP_BASE_REF"),
		os.Getenv("TURNIP_GITHUB_TOKEN"),
	); err != nil {
		log.Fatal("error cloning", "error", err)
	}

	if err := commands.InstallTool(tmpDir, repoDir, project); err != nil {
		log.Fatal("error installing tool", "error", err)
	}

	if err := commands.RunToolPlan(tmpDir, repoDir, project); err != nil {
		log.Fatal("error running plan", "error", err)
	}
}

/*
func noop() {
	//conn, err := grpc.Dial("turnip:50001", grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial("localhost:50001", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	c := pb.NewGreeterClient(conn)

	var p github.IssueComment
	if err := json.Unmarshal([]byte(os.Getenv("TURNIP_PAYLOAD")), &p); err != nil {
		log.Error("Error unmashalling", "error", err)
	}

	log.Info("Got Payload", "payload", p)

	opts := &git.CloneOptions{
		Auth: &http.BasicAuth{
			Username: "token",
			Password: os.Getenv("TURNIP_GITHUB_TOKEN"),
		},
		SingleBranch: true,
		URL:          p.Repository.CloneURL,
		//ReferenceName: plumbing.NewBranchReferenceName(p.Issue.PullRequest.Head.Ref),
		ReferenceName: plumbing.NewBranchReferenceName(p.Issue.PullRequest.Base.Ref),
		Depth:         1,
		Progress:      os.Stdout,
	}

	log.Info("Cloning repo", "opts", opts)

	tmpDir, err := os.MkdirTemp("", "turnip-repo-*")
	log.Info("Created temp dir", "dir", tmpDir)
	if err != nil {
		log.Fatal("Error creating temp dir", "error", err)
	}
	//defer os.RemoveAll(tmpDir)

	if err := os.Mkdir("repo", 0750); err != nil && !os.IsExist(err) {
		log.Fatal("Error creating repo dir", "error", err)
	}
	repoDir := filepath.Join(tmpDir, "repo")

	if err := os.Chdir(tmpDir); err != nil {
		log.Fatal("Error changing directories", "error", err)
	}

	repo, err := git.PlainClone(repoDir, false, opts)
	if err != nil {
		log.Fatal("Error cloning", "error", err)
	}

	data, err := os.ReadFile(filepath.Join(repoDir, "turnip.yaml"))
	if err != nil {
		log.Fatal("Error reading configuration", "error", err)
	}

	var cfg *yamlconfig.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatal("Error parsing configuration", "error", err)
	}
	log.Info("Configuration loaded", "config", cfg)

	headBranchRef := plumbing.NewBranchReferenceName(p.Issue.PullRequest.Head.Ref)
	log.Info("Fetching head branch", "headRef", headBranchRef, "ref", config.RefSpec(fmt.Sprintf("%s:%s", p.Issue.PullRequest.Head.Ref, headBranchRef)), "reff", p.Issue.PullRequest.Head)
	err = repo.Fetch(&git.FetchOptions{
		Auth: &http.BasicAuth{
			Username: "token",
			Password: os.Getenv("TURNIP_GITHUB_TOKEN"),
		},
		Depth:    1,
		RefSpecs: []config.RefSpec{config.RefSpec(fmt.Sprintf("%s:%s", p.Issue.PullRequest.Head.Ref, headBranchRef))},
		Progress: os.Stdout,
	})
	if err != nil {
		log.Fatal("error fetching remote", "error", err)
	}

	log.Info("Generating diff")
	ref, err := repo.Head()
	if err != nil {
		log.Fatal("could not get head", "error", err)
	}

	log.Info("Repo HEAD", "head", ref)
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		log.Fatal("could not get commit object for ref", "error", err, "ref", ref)
	}

	log.Info("Commit", "commit", commit)

	dirs := make(map[string]*os.File, 0)

	headRef, err := repo.ResolveRevision(plumbing.Revision(p.Issue.PullRequest.Head.SHA))
	if err != nil {
		log.Fatal("could not resolve revision", "error", err)
	}
	headCommit, err := repo.CommitObject(*headRef)
	if err != nil {
		log.Fatal("could not get commit object for ref", "error", err, "headRef", headRef, "headBranchRef", headBranchRef)
	}
	patch, err := commit.Patch(headCommit)
	if err != nil {
		log.Fatal("error getting patch", "error", err)
	}
	for _, fp := range patch.FilePatches() {
		from, _ := fp.Files()
		if from == nil {
			log.Info("Skipping deleted file", "file", from)
			continue
		}
		dir := filepath.Dir(from.Path())
		for _, pr := range cfg.Projects {
			if strings.HasPrefix(dir, pr.Dir) {
				log.Info("YAS! Run some command in", "pr.Dir", pr.Dir, "dir", dir)
				if d, err := os.Open(filepath.Join(repoDir, pr.Dir)); err != nil {
					log.Error("Error reading Dir", "dir", pr.Dir, "error", err)
				} else {
					dirs[pr.Dir] = d
				}
			}
		}
	}

	if len(dirs) == 0 {
		return
	}

	log.Info("Running commands", "cfg.Type", cfg.Type)

	switch cfg.Type {
	case yamlconfig.ProjectTypeHelmfile:
		cmd := exec.Command("wget", "-O", "hf.tgz",
			fmt.Sprintf("https://github.com/helmfile/helmfile/releases/download/v%s/helmfile_%s_linux_amd64.tar.gz", cfg.HelmfileVersion, cfg.HelmfileVersion),
		)
		if err := cmd.Run(); err != nil {
			log.Error("Error executing", "err", err, "cmd", cmd)
		}

		cmd = exec.Command("tar", "zxf", "hf.tgz")
		if err := cmd.Run(); err != nil {
			log.Error("Error executing", "err", err, "cmd", cmd)
		}

		out, err := exec.Command("ls").Output()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("The ls after is %s\n", out)

		cmd = exec.Command("mv", "helmfile", "/bin")
		if err := cmd.Run(); err != nil {
			log.Error("Error executing", "err", err, "cmd", cmd)
		}

		out, err = exec.Command("ls", "/bin").Output()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("The ls is %s\n", out)

		out, err = exec.Command("env").Output()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("The env is %s\n", out)

		for _, dir := range dirs {
			log.Infof("Running commands in dir %s", dir.Name())
			if err := dir.Chdir(); err != nil {
				log.Error("Error changing directories", "error", err, "dir", dir)
				continue
			}
			cmd := exec.Command("/bin/hemlfile", "diff")
			if err := cmd.Run(); err != nil {
				log.Error("Error executing", "err", err, "cmd", cmd)
			}
		}
	case yamlconfig.ProjectTypePulumi:
		cmd := exec.Command("wget", "-O", "pulumi.tgz",
			fmt.Sprintf("https://github.com/pulumi/pulumi/releases/download/v%s/pulumi-v%s-linux-x64.tar.gz", cfg.PulumiVersion, cfg.PulumiVersion),
		)
		if err := cmd.Run(); err != nil {
			log.Error("Error executing", "err", err, "cmd", cmd)
		}
		cmd = exec.Command("tar", "zxf", "pulumi.tgz")
		if err := cmd.Run(); err != nil {
			log.Error("Error executing", "err", err, "cmd", cmd)
		}
		for _, dir := range dirs {
			log.Infof("Running commands in dir %s", dir.Name())
			if err := dir.Chdir(); err != nil {
				log.Error("Error changing directories", "error", err, "dir", dir)
				continue
			}

			pulumiBin := filepath.Join(tmpDir, "pulumi/pulumi")

			err := execCommand(pulumiBin, "stack", "select", "staging")
			if err != nil {
				log.Error("Error selecting stack")
			}

			cmd := exec.Command(pulumiBin, "--non-interactive", "preview", "--stack", "staging", "--json")
			cmd.Env = append(cmd.Environ(), []string{`PULUMI_CONFIG_PASSPHRASE=test`}...)

			outp, err := cmd.Output()
			if err != nil {
				log.Error("Error running command", "error", err, "cmd", cmd)
			}
			log.Infof("Output: %s", string(outp))

			// Contact the server and print out its response.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			r, err := c.SayHello(ctx, &pb.HelloRequest{Name: string(outp)})
			if err != nil {
				log.Fatalf("could not greet: %v", err)
			}
			log.Infof("Greeting: %s", r)
		}
	}
}

func execCommandWithOutput(args ...string) (string, error) {
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		log.Error("Error executing", "err", err, "cmd", args)
		return "", err
	}
	return string(out), nil
}

func execCommand(args ...string) error {
	if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
		log.Error("Error executing", "err", err, "cmd", args)
		return err
	}
	return nil
}*/
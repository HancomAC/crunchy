package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type config struct {
	image    string
	beta     bool
	services []string
	workdir  string
	start    time.Time
	keepImgs int
	keepRevs int
	project  string
	region   string
	registryHost string
}

func main() {
	cfg, err := parseConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("⚡ Done in %.1fs\n", time.Since(cfg.start).Seconds())
}

func parseConfig() (*config, error) {
	image := flag.String("image", "", "Docker image name (required)")
	svc := flag.String("svc", "", "Semicolon separated Cloud Run services (required)")
	beta := flag.Bool("beta", false, "Use dev tag and skip cleanup")
	keepImgs := flag.Int("keep-images", 10, "Number of Docker image digests to retain (>=1)")
	keepRevs := flag.Int("keep-revisions", 10, "Number of Cloud Run revisions to retain (>=1)")
	project := flag.String("project", "", "GCP project to deploy to (required)")
	region := flag.String("region", "", "Cloud Run region (required)")
	registryHost := flag.String("registry-host", "", "Container registry host (e.g. gcr.io, asia.gcr.io). If omitted, inferred from --region")
	flag.Parse()

	if *image == "" {
		return nil, errors.New("--image is required")
	}

	if *svc == "" {
		return nil, errors.New("--svc is required")
	}

	services := splitServices(*svc)
	if len(services) == 0 {
		return nil, errors.New("no services provided via --svc")
	}

	if *keepImgs < 1 {
		return nil, errors.New("--keep-images must be >= 1")
	}

	if *keepRevs < 1 {
		return nil, errors.New("--keep-revisions must be >= 1")
	}

	if *project == "" {
		return nil, errors.New("--project is required")
	}

	if *region == "" {
		return nil, errors.New("--region is required")
	}

	workdir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("determine working directory: %w", err)
	}

	host := strings.TrimSpace(*registryHost)
	if host == "" {
		host = inferRegistryHost(*region)
	}

	return &config{
		image:    *image,
		beta:     *beta,
		services: services,
		workdir:  workdir,
		start:    time.Now(),
		keepImgs: *keepImgs,
		keepRevs: *keepRevs,
		project:  *project,
		region:   *region,
		registryHost: host,
	}, nil
}

func splitServices(raw string) []string {
	parts := strings.Split(raw, ";")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func inferRegistryHost(region string) string {
	region = strings.ToLower(strings.TrimSpace(region))

	switch {
	case strings.HasPrefix(region, "asia"):
		return "asia.gcr.io"
	case strings.HasPrefix(region, "europe"), strings.HasPrefix(region, "eu"):
		return "eu.gcr.io"
	case strings.HasPrefix(region, "us"):
		return "us.gcr.io"
	default:
		return "gcr.io"
	}
}

func run(cfg *config) error {
	logStep(cfg, "Building TS...")
	if err := runCommandStreaming(cfg.workdir, "pnpm", "run", "build"); err != nil {
		return fmt.Errorf("build project: %w", err)
	}

	imageRepo := fmt.Sprintf("%s/%s/%s", cfg.registryHost, cfg.project, cfg.image)
	imageTag := imageRepo
	if cfg.beta {
		imageTag += ":dev"
	}

	logStep(cfg, "Building Docker...")
	if err := runCommandStreaming(cfg.workdir, "docker", "build", "--platform", "linux/amd64", "-t", imageTag, "."); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}

	logStep(cfg, "Uploading image...")
	pushOutput, err := runCommandCapture(cfg.workdir, "docker", "push", imageTag)
	if err != nil {
		return fmt.Errorf("docker push: %w\n%s", err, pushOutput)
	}

	digest, err := parseDigest(pushOutput)
	if err != nil {
		return fmt.Errorf("parse digest: %w\n%s", err, pushOutput)
	}

	fullImagePath := fmt.Sprintf("%s@%s", imageRepo, digest)

	logStep(cfg, "Deploying...")
	if err := deployServices(cfg, fullImagePath); err != nil {
		return err
	}

	if !cfg.beta {
		logStep(cfg, "Cleaning up image...")
		if err := cleanupOldImages(cfg, cfg.image); err != nil {
			return err
		}
	}

	return nil
}

func logStep(cfg *config, message string) {
	fmt.Printf("%s (%.1fs)\n", message, time.Since(cfg.start).Seconds())
}

func runCommandStreaming(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

func runCommandCapture(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func parseDigest(output string) (string, error) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "digest:") {
			parts := strings.Split(line, "digest:")
			if len(parts) < 2 {
				continue
			}

			afterDigest := strings.TrimSpace(parts[1])
			fields := strings.Fields(afterDigest)
			if len(fields) == 0 {
				continue
			}
			return fields[0], nil
		}
	}
	return "", errors.New("digest not found in output")
}

func deployServices(cfg *config, image string) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(cfg.services))

	for _, service := range cfg.services {
		service := service
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := deployService(cfg, service, image); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	var buf bytes.Buffer
	for err := range errs {
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(err.Error())
	}

	if buf.Len() > 0 {
		return errors.New(buf.String())
	}

	return nil
}

func deployService(cfg *config, service, image string) error {
	revisions, err := listRevisions(cfg, service)
	if err != nil {
		return fmt.Errorf("list revisions for %s: %w", service, err)
	}

	if err := runCommandStreaming(cfg.workdir, "gcloud", "run", "deploy", service,
		"--image="+image,
		"--platform=managed",
		"--region="+cfg.region,
		"--project="+cfg.project,
	); err != nil {
		return fmt.Errorf("deploy service %s: %w", service, err)
	}

	logStep(cfg, fmt.Sprintf("Migrating %s...", service))
	if err := runCommandStreaming(cfg.workdir, "gcloud", "run", "services", "update-traffic", service,
		"--to-latest",
		"--region="+cfg.region,
	); err != nil {
		return fmt.Errorf("update traffic for %s: %w", service, err)
	}

	logStep(cfg, fmt.Sprintf("Deployed %s.", service))

	if len(revisions) <= cfg.keepRevs {
		return nil
	}

	for _, revision := range revisions[cfg.keepRevs:] {
		logStep(cfg, fmt.Sprintf("Deleting %s...", revision))
		if err := runCommandStreaming(cfg.workdir, "gcloud", "run", "revisions", "delete", revision,
			"--region="+cfg.region,
			"-q",
		); err != nil {
			return fmt.Errorf("delete revision %s: %w", revision, err)
		}
		logStep(cfg, fmt.Sprintf("Deleted %s.", revision))
	}

	return nil
}

func listRevisions(cfg *config, service string) ([]string, error) {
	output, err := runCommandCapture(cfg.workdir, "gcloud", "run", "revisions", "list",
		"--region="+cfg.region,
		"--service="+service,
		`--format=value(metadata.name)`,
	)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	revisions := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			revisions = append(revisions, trimmed)
		}
	}
	return revisions, nil
}

func cleanupOldImages(cfg *config, image string) error {
	repo := fmt.Sprintf("%s/%s/%s", cfg.registryHost, cfg.project, image)

	output, err := runCommandCapture(cfg.workdir,
		"gcloud", "container", "images", "list-tags", repo,
		"--format=json",
	)
	if err != nil {
		return fmt.Errorf("list image tags: %w\n%s", err, output)
	}

	var tags []struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal([]byte(output), &tags); err != nil {
		return fmt.Errorf("parse image tags json: %w", err)
	}

	if len(tags) <= cfg.keepImgs {
		return nil
	}

	for _, tag := range tags[cfg.keepImgs:] {
		if tag.Digest == "" {
			continue
		}
		imageWithDigest := fmt.Sprintf("%s@%s", repo, tag.Digest)
		logStep(cfg, fmt.Sprintf("Deleting %s...", imageWithDigest))
		if err := runCommandStreaming(cfg.workdir, "gcloud", "container", "images", "delete",
			imageWithDigest,
			"--force-delete-tags",
			"-q",
		); err != nil {
			return fmt.Errorf("delete image %s: %w", imageWithDigest, err)
		}
		logStep(cfg, fmt.Sprintf("Deleted %s.", imageWithDigest))
	}

	return nil
}

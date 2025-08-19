package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type DeploymentState struct {
	State      string `yaml:"state"`
	Timestamp  string `yaml:"timestamp"`
	LastAction string `yaml:"last_action"`
}

type deploymentInfo struct {
	Name         string
	Description  string
	State        string
	LastAction   string
	LastModified string
	Path         string
}

func loadTfvars(filename string) (map[string]string, error) {
	m := make(map[string]string)
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		m[key] = val
	}
	return m, scanner.Err()
}

func saveTfvars(filename string, updates map[string]string) error {
	input, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	lines := strings.Split(string(input), "\n")
	for i, line := range lines {
		for key, newval := range updates {
			if strings.HasPrefix(strings.TrimSpace(line), key+" ") || strings.HasPrefix(strings.TrimSpace(line), key+"=") {
				lines[i] = fmt.Sprintf("%s = %s", key, newval)
			}
		}
	}
	output := strings.Join(lines, "\n")
	return os.WriteFile(filename, []byte(output), 0644)
}

func runTerraformInit(appDir string) error {
	cmd := exec.Command("terraform", "init", "-input=false")
	cmd.Dir = appDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("terraform init failed: %v\n%s", err, string(out))
	}
	return nil
}

func runTerraformApply(appDir string) error {
	cmd := exec.Command("terraform", "apply", "-auto-approve", "-input=false")
	cmd.Dir = appDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("terraform apply failed: %v\n%s", err, string(out))
	}
	return nil
}

func runTerraformDestroy(appDir string) error {
	cmd := exec.Command("terraform", "destroy", "-auto-approve", "-input=false")
	cmd.Dir = appDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("terraform destroy failed: %v\n%s", err, string(out))
	}
	return nil
}

func runTerraformPlanDestroy(appDir string) error {
	cmd := exec.Command("terraform", "plan", "-destroy", "-input=false")
	cmd.Dir = appDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("terraform plan -destroy failed: %v\n%s", err, string(out))
	}
	return nil
}

// deleteS3Object removes a single S3 object that stores the remote state.
// It shells out to AWS CLI to avoid adding heavy SDK dependencies.
// Assumes the bucket is shared; only the object at <appDir>/s3/terraform.tfstate is removed.
func deleteS3Object(bucket, key, profile, region string) error {
	if bucket == "" || key == "" {
		return fmt.Errorf("missing bucket or key for S3 deletion")
	}
	s3URI := fmt.Sprintf("s3://%s/%s", bucket, key)
	cmd := exec.Command("aws", "s3", "rm", s3URI)
	env := os.Environ()
	if profile != "" {
		env = append(env, fmt.Sprintf("AWS_PROFILE=%s", profile))
	}
	if region != "" {
		env = append(env, fmt.Sprintf("AWS_REGION=%s", region))
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("aws s3 rm failed: %v\n%s", err, string(out))
	}
	return nil
}

// deleteS3Prefix removes all objects under an S3 prefix (acts like deleting a directory).
func deleteS3Prefix(bucket, prefix, profile, region string) error {
	if bucket == "" || prefix == "" {
		return fmt.Errorf("missing bucket or prefix for S3 deletion")
	}
	s3URI := fmt.Sprintf("s3://%s/%s", bucket, strings.TrimPrefix(prefix, "/"))
	cmd := exec.Command("aws", "s3", "rm", s3URI, "--recursive")
	env := os.Environ()
	if profile != "" {
		env = append(env, fmt.Sprintf("AWS_PROFILE=%s", profile))
	}
	if region != "" {
		env = append(env, fmt.Sprintf("AWS_REGION=%s", region))
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("aws s3 rm --recursive failed: %v\n%s", err, string(out))
	}
	return nil
}

func setDeploymentState(path string, state string, action string) error {
	s := DeploymentState{
		State:      state,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		LastAction: action,
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(path, "launcher.state"), data, 0644)
}

func getDeploymentState(path string) (DeploymentState, error) {
	var s DeploymentState
	data, err := os.ReadFile(filepath.Join(path, "launcher.state"))
	if err != nil {
		s.State = "UNKNOWN"
		s.Timestamp = ""
		s.LastAction = ""
		return s, err
	}
	err = yaml.Unmarshal(data, &s)
	if err != nil {
		s.State = "UNKNOWN"
	}
	return s, err
}

func listDeployments(appsDir string) ([]deploymentInfo, error) {
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, err
	}
	var infos []deploymentInfo
	for _, e := range entries {
		if e.IsDir() {
			full := filepath.Join(appsDir, e.Name())
			stat, err := os.Stat(full)
			if err != nil {
				continue
			}
			desc := ""
			tfvarsPath := filepath.Join(full, "terraform.tfvars")
			if vals, err := loadTfvars(tfvarsPath); err == nil {
				desc = strings.Trim(vals["platform_description"], "\"")
			}
			st, _ := getDeploymentState(full)
			state := st.State
			lastAction := ""
			if st.Timestamp != "" {
				if len(st.Timestamp) >= 16 {
					lastAction = st.Timestamp[:16] // YYYY-MM-DDTHH:MM
				} else {
					lastAction = st.Timestamp
				}
			}
			infos = append(infos, deploymentInfo{
				Name:         e.Name(),
				Description:  desc,
				State:        state,
				LastAction:   lastAction,
				LastModified: stat.ModTime().Format("2006-01-02 15:04"),
				Path:         full,
			})
		}
	}
	return infos, nil
}

func copyDir(src string, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			in, err := os.Open(srcPath)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.Create(dstPath)
			if err != nil {
				return err
			}
			defer out.Close()
			if _, err = io.Copy(out, in); err != nil {
				return err
			}
			info, _ := os.Stat(srcPath)
			if err = out.Chmod(info.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}

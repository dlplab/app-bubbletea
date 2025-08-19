package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Repo          string `yaml:"repo"`
	AppsPath      string `yaml:"apps_path"`
	TemplatePath  string `yaml:"template_path"`
	PresetsPath   string `yaml:"presets_path"`
	AWSProfile    string `yaml:"aws_profile"`
	S3Bucket      string `yaml:"s3_bucket"`
	AWSRegion     string `yaml:"aws_region"`
	TerraformPath string `yaml:"terraform_path"`
	BackendType   string `yaml:"backend_type"` // optional, future use (s3|gitlab|github)
}

type Options struct {
	Clusters []string `yaml:"clusters"`
	Zones    []string `yaml:"zones"`
}

func loadConfig(path string) (Config, error) {
	var cfg Config
	f, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = yaml.Unmarshal(f, &cfg)
	return cfg, err
}

func loadOptions(path string) (Options, error) {
	var o Options
	f, err := os.ReadFile(path)
	if err != nil {
		return o, err
	}
	err = yaml.Unmarshal(f, &o)
	return o, err
}

// getEnvStatus reports Vault and AWS status in that order.
func getEnvStatus(cfg Config) (vaultOK, awsOK bool) {
	roleID := os.Getenv("TF_VAR_role_id")
	secretID := os.Getenv("TF_VAR_secret_id")
	vaultOK = roleID != "" && secretID != ""

	awsProfile := os.Getenv("AWS_PROFILE")
	awsRegion := os.Getenv("AWS_REGION")
	if awsProfile == "" {
		awsProfile = cfg.AWSProfile
	}
	if awsRegion == "" {
		awsRegion = cfg.AWSRegion
	}
	awsOK = awsProfile != "" && awsRegion != ""
	return
}

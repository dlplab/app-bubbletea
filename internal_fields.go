package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type FieldMeta struct {
	Label    string `yaml:"label"`
	Help     string `yaml:"help"`
	ReadOnly bool   `yaml:"readOnly"`
	Type     string `yaml:"type"`
}

// FieldsYaml is the structure for the fields.yaml file
type FieldsYaml struct {
	Fields map[string]FieldMeta `yaml:"fields"`
}

func loadFieldMeta(path string) (map[string]FieldMeta, error) {
	var fy FieldsYaml
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &fy); err != nil {
		return nil, err
	}
	return fy.Fields, nil
}

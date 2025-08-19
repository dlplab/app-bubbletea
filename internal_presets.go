package main

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Preset struct {
	Name   string
	Values map[string]interface{}
}

func loadPresets(presetsDir string) ([]Preset, error) {
	entries, err := os.ReadDir(presetsDir)
	if err != nil {
		return nil, err
	}
	var out []Preset
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			path := filepath.Join(presetsDir, e.Name())
			values, err := loadPreset(path)
			if err != nil {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".yaml")
			out = append(out, Preset{Name: name, Values: values})
		}
	}
	return out, nil
}

func loadPreset(path string) (map[string]interface{}, error) {
	var out map[string]interface{}
	f, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(f, &out)
	return out, err
}

package cli

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
)

type Config struct {
	Store         string `json:"object_store"`
	Region        string `json:"aws_region"`
	ECRRepository string `json:"ecr_repository"`
	IAMRole       string `json:"iam_role"`
}

func WriteConfig(cfg *Config, configPath string) error {
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(path.Dir(configPath), 070)
	return ioutil.WriteFile(configPath, encoded, 0644)
}

func ReadConfig(configPath string) (*Config, error) {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", configPath, err)
	}
	return &cfg, nil
}

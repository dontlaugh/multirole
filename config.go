package main

import "github.com/BurntSushi/toml"

type Config struct {
	IdentityProfile string          `toml:"identity_profile"`
	MFASerial       string          `toml:"mfa_serial"`
	Profiles        []AssumeProfile `toml:"profile"`
}

type AssumeProfile struct {
	Name string `toml:"name"`
	ARN  string `toml:"arn"`
}

func LoadConfig(path string) (*Config, error) {
	var conf Config

	_, err := toml.DecodeFile(path, &conf)
	if err != nil {
		return nil, err
	}
	return &conf, nil
}

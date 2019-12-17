package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/aws/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/sasbury/mini"
	"github.com/urfave/cli"
)

func main() {

	app := cli.NewApp()
	app.Usage = "Assume multiple AWS roles at once from the command line"
	app.Version = "0.1.0"

	var (
		awsCredsFile = cli.StringFlag{
			Name:   "aws-creds-file",
			Value:  filepath.Join(os.ExpandEnv("$HOME"), ".aws/credentials"),
			EnvVar: "AWS_SHARED_CREDENTIALS_FILE",
		}
		configFile = cli.StringFlag{
			Name:  "config",
			Value: filepath.Join(os.ExpandEnv("$HOME"), ".config/multirole/config.toml"),
		}
	)
	app.Flags = []cli.Flag{awsCredsFile, configFile}
	app.Action = func(ctx *cli.Context) error {
		conf, err := LoadConfig(ctx.GlobalString("config"))
		if err != nil {
			return err
		}
		awsCreds := ctx.GlobalString("aws-creds-file")
		return AssumeAll(conf, awsCreds)
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func AssumeAll(conf *Config, awsCredsFile string) error {
	ctx := context.TODO()
	cfg, err := external.LoadDefaultAWSConfig(
		external.WithSharedConfigFiles([]string{awsCredsFile}),
		external.WithSharedConfigProfile(conf.IdentityProfile),
	)
	if err != nil {
		return fmt.Errorf("load aws config: %v", err)
	}
	idCreds, err := cfg.Credentials.Retrieve()
	if err != nil {
		return fmt.Errorf("cannot export initial creds: %v", err)
	}
	mfatoken, err := stscreds.StdinTokenProvider()
	if err != nil {
		return fmt.Errorf("token from stdin: %v", err)
	}

	stsClient := sts.New(cfg)
	// Log into the default session and retrieve/save default creds
	str := sts.GetSessionTokenInput{}
	str.TokenCode = strPtr(mfatoken)
	str.SerialNumber = strPtr(conf.MFASerial)
	reqq := stsClient.GetSessionTokenRequest(&str)
	sessResp, err := reqq.Send(ctx)
	if err != nil {
		return err
	}
	defaultCreds := sessResp.Credentials
	// Create new config with temporary default creds
	// is there a function that does this conversion?
	dc := aws.Credentials{
		AccessKeyID:     *defaultCreds.AccessKeyId,
		SecretAccessKey: *defaultCreds.SecretAccessKey,
		SessionToken:    *defaultCreds.SessionToken,
		Expires:         *defaultCreds.Expiration,
	}
	cfg, err = external.LoadDefaultAWSConfig(
		external.WithCredentialsValue(dc),
	)

	t := time.Now().UTC()
	sessionName := fmt.Sprintf("%d%02d%02d_%02d%02d%02d",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second())

	// get a new client with our default "logged in" creds
	stsClient = sts.New(cfg)
	var assumedRoles = make(map[string]*sts.Credentials)
	for _, profile := range conf.Profiles {
		ari := sts.AssumeRoleInput{}
		// TODO: use "session chaining" instead of role chaining here.
		// Role chaining has a timeout of 1 hour, tops.
		ari.DurationSeconds = int64Ptr(60 * 60) // 1 hour
		ari.RoleArn = strPtr(profile.ARN)
		ari.RoleSessionName = strPtr(sessionName) // What do we put here?
		req := stsClient.AssumeRoleRequest(&ari)
		resp, err := req.Send(ctx)
		if err != nil {
			return fmt.Errorf("could not assume role %s: %v", profile.Name, err)
		}
		assumedRoles[profile.Name] = resp.Credentials
	}

	output := bytes.NewBuffer([]byte(""))

	// write identity
	writeStanza(output, "identity", &sts.Credentials{
		AccessKeyId:     strPtr(idCreds.AccessKeyID),
		SecretAccessKey: strPtr(idCreds.SecretAccessKey),
	})
	// write defaults
	writeStanza(output, "default", defaultCreds)
	// write assumed
	for k, v := range assumedRoles {
		writeStanza(output, k, v)
	}

	// overwrite AWS creds file
	ioutil.WriteFile(awsCredsFile, output.Bytes(), 0664)
	return nil
}

func writeStanza(w io.Writer, name string, creds *sts.Credentials) {
	writeVal := func(k, v string) { w.Write([]byte(fmt.Sprintf("%s = %s\n", k, v))) }
	writeStanza := func(s string) { w.Write([]byte(fmt.Sprintf("[%s]\n", s))) }
	writeStanza(name)
	if creds.AccessKeyId != nil && creds.SecretAccessKey != nil {
		writeVal("aws_access_key_id", *creds.AccessKeyId)
		writeVal("aws_secret_access_key", *creds.SecretAccessKey)
	}

	if creds.SessionToken != nil {
		writeVal("aws_session_token", *creds.SessionToken)
	}
	if creds.Expiration != nil {
		t := *creds.Expiration
		// assuming utc here
		s := fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d+00:00",
			t.Year(), t.Month(), t.Day(),
			t.Hour(), t.Minute(), t.Second())
		writeVal("awsmfa_expiration", s)
	}
	w.Write([]byte("\n"))
}

type Profile struct {
	Name               string
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSSessionToken    string
	AWSMFAExpiration   time.Time
}

func LoadExistingProfile(path, profile string) (Profile, error) {
	var p Profile
	loaded, err := mini.LoadConfiguration(path)
	if err != nil {
		return p, err
	}
	sections := loaded.SectionNames()
	for _, name := range sections {
		if name == profile {
			p.Name = name
			p.AWSAccessKeyID = loaded.StringFromSection(name, "aws_access_key_id", "")
			p.AWSSecretAccessKey = loaded.StringFromSection(name, "aws_secret_access_key", "")
			p.AWSSessionToken = loaded.StringFromSection(name, "aws_session_token", "")
			if timeString := loaded.StringFromSection(name, "awsmfa_expiration", ""); timeString != "" {

				t, err := time.Parse("2019-07-03T08:36:31+00:00", timeString)
				if err != nil {
					return p, fmt.Errorf("timestamp parse error in section %s: %v", name, err)
				}
				p.AWSMFAExpiration = t
			}
		}
	}
	return p, nil
}

func strPtr(s string) *string { return &s }
func int64Ptr(i int64) *int64 { return &i }

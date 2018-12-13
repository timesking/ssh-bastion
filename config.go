package main

import (
	"fmt"
	"io/ioutil"

	yaml "gopkg.in/yaml.v2"
)

type SSHConfig struct {
	Global       SSHConfigGlobal               `yaml:"global"`
	Servers      map[string]SSHConfigServer    `yaml:"servers"`
	AWSInstances map[string]AWSSSHConfigServer `yaml:"awsinstances"`
	ACLs         map[string]SSHConfigACL       `yaml:"acls"`
	Users        map[string]SSHConfigUser      `yaml:"users"`
}

type SSHConfigGlobal struct {
	MOTDPath     string   `yaml:"motd_path"`
	LogPath      string   `yaml:"log_path"`
	HostKeyPaths []string `yaml:"host_keys"`
	AuthType     string   `yaml:"auth_type"`
	LDAP_Server  string   `yaml:"ldap_server"`
	LDAP_Domain  string   `yaml:"ldap_domain"`
	PassPassword bool     `yaml:"pass_password"`
	ListenPath   string   `yaml:"listen_path"`
}

type AWSSSHConfigServer struct {
	SSHConfigServer `yaml:",inline"`
	RegexFilter     string   `yaml:"regex"`
	Regions         []string `yaml:"regions"`
	AwsAccessKey    string   `yaml:"AWS_ACCESS_KEY_ID,omitempty"`
	AwsSecretKey    string   `yaml:"AWS_SECRET_ACCESS_KEY,omitempty"`
}

type SSHConfigServer struct {
	HostPubKeyFiles []string `yaml:"host_pubkeys"`
	ConnectPath     string   `yaml:"connect_path"`
	LoginUser       string   `yaml:"login_user"`
	LoginPrivate    string   `yaml:"login_privatekey"`
}

type SSHConfigACL struct {
	AllowedServers    []string `yaml:"allow_list"`
	AWSAllowedServers []string `yaml:"aws_allow_list"`
}

type SSHConfigUser struct {
	ACL                string `yaml:"acl"`
	AuthorizedKeysFile string `yaml:"authorized_keys_file"`
	AwsUser            string `yaml:"awsuser"`
}

func fetchConfig(filename string) (*SSHConfig, error) {
	configData, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("Failed to open config file: %s", err)
	}

	config := &SSHConfig{}

	err = yaml.Unmarshal(configData, config)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse YAML config file: %s", err)
	}

	return config, nil
}

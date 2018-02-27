//
// Copyright (c) SAS Institute Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"gopkg.in/yaml.v2"
)

const (
	defaultSigXchg = "relic.signatures"
	sigKey         = "relic.signatures"
)

var Version = "unknown" // set this at link time
var UserAgent = "relic/" + Version
var Author = "SAS Institute Inc."

type TokenConfig struct {
	Type       string  // Provider type: file or pkcs11 (default)
	Provider   string  // Path to PKCS#11 provider module (required)
	Label      string  // Select a token by label
	Serial     string  // Select a token by serial number
	Pin        *string // PIN to use, otherwise will be prompted. Can be empty. (optional)
	Timeout    int     // (server) Terminate command after N seconds (default 300)
	User       *uint   // User argument for PKCS#11 login (optional)
	UseKeyring bool    // Read PIN from system keyring

	name string
}

type KeyConfig struct {
	Token           string   // Token section to use for this key (linux)
	Alias           string   // This is an alias for another key
	Label           string   // Select a key by label
	ID              string   // Select a key by ID (hex notation)
	PgpCertificate  string   // Path to PGP certificate associated with this key
	X509Certificate string   // Path to X.509 certificate associated with this key
	KeyFile         string   // For "file" tokens, path to the private key
	Roles           []string // List of user roles that can use this key
	Timestamp       bool     // If true, attach a timestamped countersignature when possible
	Hide            bool     // If true, then omit this key from 'remote list-keys'

	name  string
	token *TokenConfig
}

type ServerConfig struct {
	Listen     string // Port to listen for TLS connections
	ListenHTTP string // Port to listen for plaintext connections
	KeyFile    string // Path to TLS key file
	CertFile   string // Path to TLS certificate chain
	LogFile    string // Optional error log

	Disabled    bool // Always return 503 Service Unavailable
	ListenDebug bool // Serve debug info on an alternate port

	TokenCheckInterval int
	TokenCheckFailures int
	TokenCheckTimeout  int

	// URLs to all servers in the cluster. If a client uses DirectoryURL to
	// point to this server (or a load balancer), then we will give them these
	// URLs as a means to distribute load without needing a middle-box.
	Siblings []string
}

type ClientConfig struct {
	Nickname string   // Name that appears in audit log entries
	Roles    []string // List of roles that this client possesses
}

type RemoteConfig struct {
	URL          string `,omitempty` // URL of remote server
	DirectoryURL string `,omitempty` // URL of directory server
	KeyFile      string `,omitempty` // Path to TLS client key file
	CertFile     string `,omitempty` // Path to TLS client certificate
	CaCert       string `,omitempty` // Path to CA certificate
}

type TimestampConfig struct {
	URLs    []string // List of timestamp server URLs
	MsURLs  []string // List of microsoft-style URLs
	Timeout int      // Connect timeout in seconds
	CaCert  string   // Path to CA certificate
}

type AmqpConfig struct {
	URL        string // AMQP URL to report signatures to i.e. amqp://user:password@host
	CaCert     string
	KeyFile    string
	CertFile   string
	SigsXchg   string // Name of exchange to send to (default relic.signatures)
	SealingKey string // Name of key to seal audit related information
}

type Config struct {
	Tokens    map[string]*TokenConfig  `,omitempty`
	Keys      map[string]*KeyConfig    `,omitempty`
	Server    *ServerConfig            `,omitempty`
	Clients   map[string]*ClientConfig `,omitempty`
	Remote    *RemoteConfig            `,omitempty`
	Timestamp *TimestampConfig         `,omitempty`
	Amqp      *AmqpConfig              `,omitempty`

	PinFile string `,omitempty` // Optional YAML file with additional token PINs

	path string
}

func ReadFile(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config := new(Config)
	err = yaml.Unmarshal(data, config)
	if err != nil {
		return nil, err
	}
	config.path = path
	return config, config.Normalize()
}

func (config *Config) Normalize() error {
	normalized := make(map[string]*ClientConfig)
	for fingerprint, client := range config.Clients {
		if len(fingerprint) != 64 {
			return errors.New("Client keys must be hex-encoded SHA256 digests of the public key")
		}
		lower := strings.ToLower(fingerprint)
		normalized[lower] = client
	}
	config.Clients = normalized
	if config.PinFile != "" {
		contents, err := ioutil.ReadFile(config.PinFile)
		if err != nil {
			return fmt.Errorf("error reading PinFile: %s", err)
		}
		pinMap := make(map[string]string)
		if err := yaml.Unmarshal(contents, pinMap); err != nil {
			return fmt.Errorf("error reading PinFile: %s", err)
		}
		for token, pin := range pinMap {
			tokenConf := config.Tokens[token]
			if tokenConf != nil {
				ppin := pin
				tokenConf.Pin = &ppin
			}
		}
	}
	for tokenName, tokenConf := range config.Tokens {
		tokenConf.name = tokenName
		if tokenConf.Type == "" {
			tokenConf.Type = "pkcs11"
		}
	}
	for keyName, keyConf := range config.Keys {
		keyConf.name = keyName
		if keyConf.Token != "" {
			keyConf.token = config.Tokens[keyConf.Token]
		}
	}
	return nil
}

func (config *Config) GetToken(tokenName string) (*TokenConfig, error) {
	if config.Tokens == nil {
		return nil, errors.New("No tokens defined in configuration")
	}
	tokenConf, ok := config.Tokens[tokenName]
	if !ok {
		return nil, fmt.Errorf("Token \"%s\" not found in configuration", tokenName)
	}
	return tokenConf, nil
}

func (config *Config) NewToken(name string) *TokenConfig {
	if config.Tokens == nil {
		config.Tokens = make(map[string]*TokenConfig)
	}
	tok := &TokenConfig{name: name}
	config.Tokens[name] = tok
	return tok
}

func (config *Config) GetKey(keyName string) (*KeyConfig, error) {
	keyConf, ok := config.Keys[keyName]
	if !ok {
		return nil, fmt.Errorf("Key \"%s\" not found in configuration", keyName)
	} else if keyConf.Alias != "" {
		keyConf, ok = config.Keys[keyConf.Alias]
		if !ok {
			return nil, fmt.Errorf("Alias \"%s\" points to undefined key \"%s\"", keyName, keyConf.Alias)
		}
	}
	if keyConf.Token == "" {
		return nil, fmt.Errorf("Key \"%s\" does not specify required value 'token'", keyName)
	}
	return keyConf, nil
}

func (config *Config) NewKey(name string) *KeyConfig {
	if config.Keys == nil {
		config.Keys = make(map[string]*KeyConfig)
	}
	key := &KeyConfig{name: name}
	config.Keys[name] = key
	return key
}

func (config *Config) Path() string {
	return config.path
}

func (config *Config) GetTimestampConfig() (*TimestampConfig, error) {
	tconf := config.Timestamp
	if tconf == nil {
		return nil, errors.New("No timestamp section exists in the configuration")
	} else if len(tconf.URLs) == 0 {
		return nil, errors.New("No timestamp urls are defined in the configuration")
	}
	return tconf, nil
}

func (tconf *TokenConfig) Name() string {
	return tconf.name
}

func (aconf *AmqpConfig) ExchangeName() string {
	if aconf.SigsXchg != "" {
		return aconf.SigsXchg
	}
	return defaultSigXchg
}

func (aconf *AmqpConfig) RoutingKey() string {
	return sigKey
}

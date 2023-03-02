package config

import (
	xyaml "github.com/ignite/cli/ignite/pkg/yaml"
	"gopkg.in/yaml.v2"
	"io"
)

type Config struct {
	Binary         string      `yaml:"binary"`
	HomeDir        string      `yaml:"home_dir"`
	KeyringBackend string      `yaml:"keyring_backend"`
	Passphrase     string      `yaml:"passphrase"`
	Seed           Seed        `yaml:"seed"`
	Accounts       []Account   `yaml:"accounts"`
	Genesis        xyaml.Map   `yaml:"genesis,omitempty"`
	Validators     []Validator `yaml:"validators"`
}

// Decode decodes the config file values from YAML.
func (c *Config) Decode(r io.Reader) error {
	return yaml.NewDecoder(r).Decode(c)
}

// Account holds the options related to setting up Cosmos wallets.
type Account struct {
	Name     string   `yaml:"name"`
	Coins    []string `yaml:"coins,omitempty"`
	Mnemonic string   `yaml:"mnemonic,omitempty"`
	Address  string   `yaml:"address,omitempty"`
	CoinType string   `yaml:"cointype,omitempty"`

	// The RPCAddress off the chain that account is issued at.
	RPCAddress string `yaml:"rpc_address,omitempty"`
}

type Seed struct {
	// Name is the name of the seed node.
	Name string `yaml:"name"`

	Addr string `yaml:"addr"`

	// App overwrites appd's config/app.toml configs.
	App xyaml.Map `yaml:"app,omitempty"`

	// Config overwrites appd's config/config.toml configs.
	Config xyaml.Map `yaml:"config,omitempty"`

	// Client overwrites appd's config/client.toml configs.
	Client xyaml.Map `yaml:"client,omitempty"`
}

type Validator struct {
	// Name is the name of the validator.
	Name string `yaml:"name"`

	// Bonded is how much the validator has staked.
	Bonded string `yaml:"bonded"`

	// App overwrites appd's config/app.toml configs.
	App xyaml.Map `yaml:"app,omitempty"`

	// Config overwrites appd's config/config.toml configs.
	Config xyaml.Map `yaml:"config,omitempty"`

	// Client overwrites appd's config/client.toml configs.
	Client xyaml.Map `yaml:"client,omitempty"`
}

package main

import (
	"bytes"
	"cosmos-init/config"
	"fmt"
	"github.com/ignite/cli/ignite/pkg/confile"
	"github.com/imdario/mergo"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const ConfigFile = "config.yml"

func main() {
	cfg := GetConfig(ConfigFile)

	os.RemoveAll(cfg.HomeDir)
	if err := os.Mkdir(cfg.HomeDir, os.ModePerm); err != nil {
		panic(err)
	}

	// init validators
	for i, val := range cfg.Validators {
		dir := cfg.HomeDir + "/" + strconv.Itoa(i)
		cmd := exec.Command(cfg.Binary, "init", val.Name, "--home", dir, "--chain-id", cfg.Genesis["chain_id"].(string))
		if err := cmd.Run(); err != nil {
			panic(err)
		}

		// rewrite val's config.toml from config
		if err := UpdateTomlFile(dir+"/config/config.toml", val.Config); err != nil {
			panic(err)
		}

		// rewrite val's app.toml from config
		if err := UpdateTomlFile(dir+"/config/app.toml", val.App); err != nil {
			panic(err)
		}

		// rewrite val's client.toml from config
		if err := UpdateTomlFile(dir+"/config/client.toml", val.Client); err != nil {
			panic(err)
		}

		// 1. generate address (key) for validator
		// 2. add funds to genesis account
		// 3. create gentx
		// 4. move gentx to first val folder
		{
			address := addKey(dir, val.Name, cfg)

			if i != 0 {
				cmd := exec.Command(cfg.Binary, "add-genesis-account", address, val.Bonded, "--home", cfg.HomeDir+"/0")
				if err := cmd.Run(); err != nil {
					panic(err)
				}
			}

			{
				cmd := exec.Command(cfg.Binary, "add-genesis-account", address, val.Bonded, "--home", dir)
				if err := cmd.Run(); err != nil {
					panic(err)
				}
			}

			params := []string{"gentx", val.Name, val.Bonded,
				"--home", dir,
				"--keyring-backend", cfg.KeyringBackend,
				"--chain-id", cfg.Genesis["chain_id"].(string)}

			if val.Gentx != nil {
				params = append(params, val.Gentx.Fields()...)
			}

			cmd := exec.Command(cfg.Binary, params...)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				panic(err)
			}

			if err := cmd.Start(); err != nil {
				panic(err)
			}

			enterPassphrase(stdin, cfg)

			if err := cmd.Wait(); err != nil {
				panic(err)
			}
		}

		mustExec("mv %s/config/gentx/*.json %s/0/config/gentx/validator%d.json", dir, cfg.HomeDir, i)
	}

	// rewrite genesis params from config
	if err := UpdateJsonFile(cfg.HomeDir+"/0/config/genesis.json", cfg.Genesis); err != nil {
		panic(err)
	}

	// create genesis accounts
	for _, acc := range cfg.Accounts {
		dir := cfg.HomeDir + "/0/"
		address := addKey(dir, acc.Name, cfg)

		cmd := exec.Command(cfg.Binary, "add-genesis-account", address, strings.Join(acc.Coins, ","), "--home", dir)
		if err := cmd.Run(); err != nil {
			panic(err)
		}
	}

	// collect gentxs
	mustExec("%s collect-gentxs --keyring-backend %s --chain-id %s --home %s", cfg.Binary, cfg.KeyringBackend, cfg.Genesis["chain_id"].(string), cfg.HomeDir+"/0/")

	// populate genesis
	for i := range cfg.Validators {
		if i == 0 {
			continue
		}

		mustExec("cp %s/0/config/genesis.json %s/%d/config/genesis.json", cfg.HomeDir, cfg.HomeDir, i)
	}

	// create seed node
	nodeId := createSeed(cfg)
	for i := range cfg.Validators {
		err := UpdateTomlFile(cfg.HomeDir+"/"+strconv.Itoa(i)+"/config/config.toml", map[string]interface{}{
			"p2p": map[string]interface{}{
				"persistent_peers": nodeId + "@" + cfg.Seed.Addr,
			},
		})
		if err != nil {
			panic(err)
		}
	}
}

func mustExec(command string, args ...any) []byte {
	output, err := exec.Command("bash", "-c", fmt.Sprintf(command, args...)).CombinedOutput()
	if err != nil {
		panic(err)
	}

	return output
}

func createSeed(config *config.Config) string {
	dir := config.HomeDir + "/seed"
	cmd := exec.Command(config.Binary, "init", "seed", "--home", dir, "--chain-id", config.Genesis["chain_id"].(string))
	if err := cmd.Run(); err != nil {
		panic(err)
	}

	// rewrite val's configs from config
	if err := UpdateTomlFile(dir+"/config/config.toml", config.Seed.Config); err != nil {
		panic(err)
	}

	if err := UpdateTomlFile(dir+"/config/app.toml", config.Seed.App); err != nil {
		panic(err)
	}

	if err := UpdateTomlFile(dir+"/config/client.toml", config.Seed.Client); err != nil {
		panic(err)
	}

	mustExec("cp -r %s/config/genesis.json %s/config/genesis.json", config.HomeDir+"/0", dir)

	return strings.Trim(string(mustExec("%s tendermint show-node-id --home %s", config.Binary, dir)), "\n")
}

func GetConfig(file string) *config.Config {
	cfg := &config.Config{}

	f, err := os.Open(file)
	if err != nil {
		panic(err)
	}

	if err := cfg.Decode(f); err != nil {
		panic(err)
	}

	return cfg
}

func addKey(dir, keyname string, config *config.Config) string {
	cmd := exec.Command(config.Binary, "keys", "add", keyname, "--home", dir, "--keyring-backend", config.KeyringBackend, "--algo", "eth_secp256k1")
	result := bytes.NewBuffer(nil)
	cmd.Stdout = result
	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}

	if err := cmd.Start(); err != nil {
		panic(err)
	}

	enterPassphrase(stdin, config)

	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	return strings.Trim(strings.Split(strings.Split(result.String(), "\n")[0], ":")[1], " ")
}

func enterPassphrase(stdin io.WriteCloser, config *config.Config) {
	if _, err := stdin.Write([]byte(config.Passphrase + "\n")); err != nil {
		panic(err)
	}

	if _, err := stdin.Write([]byte(config.Passphrase + "\n")); err != nil {
		panic(err)
	}
}

func UpdateJsonFile(path string, data map[string]interface{}) error {
	content := make(map[string]interface{})
	cf := confile.New(confile.DefaultJSONEncodingCreator, path)
	if err := cf.Load(&content); err != nil {
		return err
	}

	if err := mergo.Merge(&content, data, mergo.WithOverride); err != nil {
		return err
	}

	if err := cf.Save(content); err != nil {
		return err
	}

	return nil
}

func UpdateTomlFile(path string, data map[string]interface{}) error {
	content := make(map[string]interface{})
	cf := confile.New(confile.DefaultTOMLEncodingCreator, path)
	if err := cf.Load(&content); err != nil {
		return err
	}

	if err := mergo.Merge(&content, data, mergo.WithOverride); err != nil {
		return err
	}

	if err := cf.Save(content); err != nil {
		return err
	}

	return nil
}

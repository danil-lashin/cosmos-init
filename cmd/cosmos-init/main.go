package main

import (
	"bytes"
	"cosmos-init/config"
	"fmt"
	"github.com/ignite/cli/ignite/pkg/confile"
	"github.com/imdario/mergo"
	"os"
	"os/exec"
	"strings"
)

const DefaultConfigFile = "config.yml"

func main() {
	configFile := DefaultConfigFile
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	cfg := GetConfig(configFile)

	os.RemoveAll(cfg.HomeDir)
	if err := os.Mkdir(cfg.HomeDir, os.ModePerm); err != nil {
		panic(err)
	}

	// init validators
	for i, val := range cfg.Validators {
		dir := cfg.ValDir(val)
		mustExec("%s init %s --home %s --chain-id %s", cfg.Binary, val.Name, dir, cfg.Genesis["chain_id"].(string))

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

		address := addKey(dir, val.Name, cfg)

		if i != 0 {
			mustExec("%s add-genesis-account %s %s --home %s", cfg.Binary, address, val.Bonded, cfg.FirstValDir())
		}

		mustExec("%s add-genesis-account %s %s --home %s", cfg.Binary, address, val.Bonded, dir)

		params := []string{"gentx", val.Name, val.Bonded,
			"--home", dir,
			"--keyring-backend", cfg.KeyringBackend,
			"--chain-id", cfg.Genesis["chain_id"].(string)}

		if val.Gentx != nil {
			params = append(params, val.Gentx.ToParams()...)
		}

		mustExecWithPassphrase(cfg.Passphrase, "%s %s", cfg.Binary, strings.Join(params, " "))

		mustExec("mv %s/config/gentx/*.json %s/config/gentx/validator%d.json", dir, cfg.FirstValDir(), i)
	}

	// rewrite genesis params from config
	if err := UpdateJsonFile(cfg.FirstValDir()+"/config/genesis.json", cfg.Genesis); err != nil {
		panic(err)
	}

	// create genesis accounts
	for _, acc := range cfg.Accounts {
		address := addKey(cfg.FirstValDir(), acc.Name, cfg)

		mustExec("%s add-genesis-account %s %s --home %s", cfg.Binary, address, strings.Join(acc.Coins, ","), cfg.FirstValDir())
	}

	// collect gentxs
	mustExec("%s collect-gentxs --home %s", cfg.Binary, cfg.FirstValDir())

	// populate genesis
	for i, val := range cfg.Validators {
		if i == 0 {
			continue
		}

		mustExec("cp %s/config/genesis.json %s/config/genesis.json", cfg.FirstValDir(), cfg.ValDir(val))
	}

	// create seed node
	nodeId := createSeed(cfg)
	for _, val := range cfg.Validators {
		err := UpdateTomlFile(cfg.ValDir(val)+"/config/config.toml", map[string]interface{}{
			"p2p": map[string]interface{}{
				"persistent_peers": nodeId + "@" + cfg.Seed.Addr,
			},
		})
		if err != nil {
			panic(err)
		}
	}
}

func mustExecWithPassphrase(passphrase string, command string, args ...any) []byte {
	cmd := exec.Command("bash", "-c", fmt.Sprintf(command, args...))

	output := bytes.NewBuffer(nil)
	cmd.Stdout = output
	cmd.Stderr = output

	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}

	if err := cmd.Start(); err != nil {
		panic(err)
	}

	if _, err := stdin.Write([]byte(passphrase + "\n")); err != nil {
		panic(err)
	}

	if _, err := stdin.Write([]byte(passphrase + "\n")); err != nil {
		panic(err)
	}

	if err := cmd.Wait(); err != nil {
		println(output.String())
		panic(err)
	}

	return output.Bytes()
}

func mustExec(command string, args ...any) []byte {
	output, err := exec.Command("bash", "-c", fmt.Sprintf(command, args...)).CombinedOutput()
	if err != nil {
		println(string(output))
		panic(err)
	}

	return output
}

func createSeed(cfg *config.Config) string {
	dir := cfg.HomeDir + "/seed"
	mustExec("%s init seed --home %s --chain-id %s", cfg.Binary, dir, cfg.ChainID())

	// rewrite val's configs from config
	if err := UpdateTomlFile(dir+"/config/config.toml", cfg.Seed.Config); err != nil {
		panic(err)
	}

	if err := UpdateTomlFile(dir+"/config/app.toml", cfg.Seed.App); err != nil {
		panic(err)
	}

	if err := UpdateTomlFile(dir+"/config/client.toml", cfg.Seed.Client); err != nil {
		panic(err)
	}

	mustExec("cp -r %s/config/genesis.json %s/config/genesis.json", cfg.FirstValDir(), dir)

	return strings.Trim(string(mustExec("%s tendermint show-node-id --home %s", cfg.Binary, dir)), "\n")
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

func addKey(dir, keyname string, cfg *config.Config) string {
	algo := ""
	if cfg.KeyAlgo != "" {
		algo = "--algo " + cfg.KeyAlgo
	}

	result := mustExecWithPassphrase(cfg.Passphrase, "%s keys add %s --home %s --keyring-backend %s %s", cfg.Binary, keyname, dir, cfg.KeyringBackend, algo)

	return strings.Trim(strings.Split(strings.Split(string(result), "\n")[1], ":")[1], " ")
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

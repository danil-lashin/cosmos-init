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
	config := GetConfig(ConfigFile)

	os.RemoveAll(config.HomeDir)
	if err := os.Mkdir(config.HomeDir, os.ModePerm); err != nil {
		panic(err)
	}

	// init validators
	for i, val := range config.Validators {
		dir := config.HomeDir + "/" + strconv.Itoa(i)
		cmd := exec.Command(config.Binary, "init", val.Name, "--home", dir, "--chain-id", config.Genesis["chain_id"].(string))
		if err := cmd.Run(); err != nil {
			panic(err)
		}

		// rewrite val's configs from config
		if err := UpdateTomlFile(dir+"/config/config.toml", val.Config); err != nil {
			panic(err)
		}

		if err := UpdateTomlFile(dir+"/config/app.toml", val.App); err != nil {
			panic(err)
		}

		if err := UpdateTomlFile(dir+"/config/client.toml", val.Client); err != nil {
			panic(err)
		}

		{
			address := addKey(dir, val.Name, config)

			if i != 0 {
				cmd := exec.Command(config.Binary, "add-genesis-account", address, val.Bonded, "--home", config.HomeDir+"/0")
				if err := cmd.Run(); err != nil {
					panic(err)
				}
			}

			{
				cmd := exec.Command(config.Binary, "add-genesis-account", address, val.Bonded, "--home", dir)
				if err := cmd.Run(); err != nil {
					panic(err)
				}
			}

			cmd := exec.Command(config.Binary, "gentx", val.Name, val.Bonded, "--home", config.HomeDir+"/"+strconv.Itoa(i), "--keyring-backend", config.KeyringBackend, "--chain-id", config.Genesis["chain_id"].(string))
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
		}

		{
			entries, err := os.ReadDir(dir + "/config/gentx/")
			if err != nil {
				panic(err)
			}

			if err := exec.Command("mv", dir+"/config/gentx/"+
				entries[0].Name(), config.HomeDir+"/0/config/gentx/validator"+strconv.Itoa(i)+".json").Run(); err != nil {
				panic(err)
			}
		}
	}

	// rewrite genesis params from config
	if err := UpdateJsonFile(config.HomeDir+"/0/config/genesis.json", config.Genesis); err != nil {
		panic(err)
	}

	// create genesis accounts
	for _, acc := range config.Accounts {
		dir := config.HomeDir + "/0/"
		address := addKey(dir, acc.Name, config)

		cmd := exec.Command(config.Binary, "add-genesis-account", address, strings.Join(acc.Coins, ","), "--home", dir)
		if err := cmd.Run(); err != nil {
			panic(err)
		}
	}

	if output, err := exec.Command(config.Binary, "collect-gentxs", "--keyring-backend", config.KeyringBackend, "--chain-id", config.Genesis["chain_id"].(string), "--home", config.HomeDir+"/0/").CombinedOutput(); err != nil {
		println(string(output))
		panic(err)
	}

	// populate genesis
	for i := range config.Validators {
		if i == 0 {
			continue
		}

		if err := exec.Command("cp", config.HomeDir+"/0/config/genesis.json", config.HomeDir+"/"+strconv.Itoa(i)+"/config/genesis.json").Run(); err != nil {
			panic(err)
		}
	}

	nodeId := createSeed(config)

	for i := range config.Validators {
		err := UpdateTomlFile(config.HomeDir+"/"+strconv.Itoa(i)+"/config/config.toml", map[string]interface{}{
			"p2p": map[string]interface{}{
				"persistent_peers": nodeId + "@" + config.Seed.Addr,
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
	config := &config.Config{}

	f, err := os.Open(file)
	if err != nil {
		panic(err)
	}

	if err := config.Decode(f); err != nil {
		panic(err)
	}

	return config
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

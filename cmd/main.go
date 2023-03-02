package main

import (
	"bytes"
	"github.com/ignite/cli/ignite/config/chain/v1"
	"github.com/ignite/cli/ignite/pkg/confile"
	"github.com/imdario/mergo"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const BinaryName = "evmosd"
const ConfigFile = "config.yml"
const HomeDir = "./chain_data"
const KeyringBackend = "file"
const Passphrase = "12345678"

func main() {
	config := GetConfig(ConfigFile)

	os.RemoveAll(HomeDir)
	if err := os.Mkdir(HomeDir, os.ModePerm); err != nil {
		panic(err)
	}

	// init validators
	for i, val := range config.Validators {
		dir := HomeDir + "/" + strconv.Itoa(i)
		cmd := exec.Command(BinaryName, "init", val.Name, "--home", dir, "--chain-id", config.Genesis["chain_id"].(string))
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
			address := addKey(dir, val.Name)

			if i != 0 {
				cmd := exec.Command(BinaryName, "add-genesis-account", address, val.Bonded, "--home", HomeDir+"/0")
				if err := cmd.Run(); err != nil {
					panic(err)
				}
			}

			{
				cmd := exec.Command(BinaryName, "add-genesis-account", address, val.Bonded, "--home", dir)
				if err := cmd.Run(); err != nil {
					panic(err)
				}
			}

			cmd := exec.Command(BinaryName, "gentx", val.Name, val.Bonded, "--home", HomeDir+"/"+strconv.Itoa(i), "--keyring-backend", KeyringBackend, "--chain-id", config.Genesis["chain_id"].(string))
			stdin, err := cmd.StdinPipe()
			if err != nil {
				panic(err)
			}

			if err := cmd.Start(); err != nil {
				panic(err)
			}

			enterPassphrase(stdin)

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
				entries[0].Name(), HomeDir+"/0/config/gentx/validator"+strconv.Itoa(i)+".json").Run(); err != nil {
				panic(err)
			}
		}
	}

	// rewrite genesis params from config
	if err := UpdateJsonFile(HomeDir+"/0/config/genesis.json", config.Genesis); err != nil {
		panic(err)
	}

	// create genesis accounts
	for _, acc := range config.Accounts {
		dir := HomeDir + "/0/"
		address := addKey(dir, acc.Name)

		cmd := exec.Command(BinaryName, "add-genesis-account", address, strings.Join(acc.Coins, ","), "--home", dir)
		if err := cmd.Run(); err != nil {
			panic(err)
		}
	}

	if output, err := exec.Command(BinaryName, "collect-gentxs", "--keyring-backend", KeyringBackend, "--chain-id", config.Genesis["chain_id"].(string), "--home", HomeDir+"/0/").CombinedOutput(); err != nil {
		println(string(output))
		panic(err)
	}

	// populate genesis
	for i := range config.Validators {
		if i == 0 {
			continue
		}

		if err := exec.Command("cp", HomeDir+"/0/config/genesis.json", HomeDir+"/"+strconv.Itoa(i)+"/config/genesis.json").Run(); err != nil {
			panic(err)
		}
	}
}

func GetConfig(file string) *v1.Config {
	config := &v1.Config{}

	f, err := os.Open(file)
	if err != nil {
		panic(err)
	}

	if err := config.Decode(f); err != nil {
		panic(err)
	}

	return config
}

func addKey(dir, keyname string) string {
	cmd := exec.Command(BinaryName, "keys", "add", keyname, "--home", dir, "--keyring-backend", KeyringBackend, "--algo", "eth_secp256k1")
	result := bytes.NewBuffer(nil)
	cmd.Stdout = result
	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}

	if err := cmd.Start(); err != nil {
		panic(err)
	}

	enterPassphrase(stdin)

	if err := cmd.Wait(); err != nil {
		panic(err)
	}

	return strings.Trim(strings.Split(strings.Split(result.String(), "\n")[0], ":")[1], " ")
}

func enterPassphrase(stdin io.WriteCloser) {
	if _, err := stdin.Write([]byte(Passphrase + "\n")); err != nil {
		panic(err)
	}

	if _, err := stdin.Write([]byte(Passphrase + "\n")); err != nil {
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

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
	config := &v1.Config{}

	f, err := os.Open(ConfigFile)
	if err != nil {
		panic(err)
	}

	if err := config.Decode(f); err != nil {
		panic(err)
	}

	if err := os.RemoveAll(HomeDir); err != nil {
		panic(err)
	}

	if err := os.Mkdir(HomeDir, os.ModePerm); err != nil {
		panic(err)
	}

	for i, val := range config.Validators {
		dir := HomeDir + "/" + strconv.Itoa(i)
		cmd := exec.Command(BinaryName, "init", val.Name, "--home", dir, "--chain-id", config.Genesis["chain_id"].(string))
		if err := cmd.Run(); err != nil {
			panic(err)
		}

		if err := UpdateTomlFile(dir+"/config/config.toml", val.Config); err != nil {
			panic(err)
		}

		if err := UpdateTomlFile(dir+"/config/app.toml", val.App); err != nil {
			panic(err)
		}

		if err := UpdateTomlFile(dir+"/config/client.toml", val.Client); err != nil {
			panic(err)
		}
	}

	if err := UpdateJsonFile(HomeDir+"/0/config/genesis.json", config.Genesis); err != nil {
		panic(err)
	}

	for _, acc := range config.Accounts {
		dir := HomeDir + "/0/"
		address := addKey(dir, acc.Name)

		cmd := exec.Command(BinaryName, "add-genesis-account", address, strings.Join(acc.Coins, ","), "--home", dir)
		if err := cmd.Run(); err != nil {
			panic(err)
		}
	}

	for i, val := range config.Validators {
		dir := HomeDir + "/" + strconv.Itoa(i)
		keyname := "validator" + strconv.Itoa(i)

		address := addKey(dir, keyname)

		for j := range config.Validators {
			cmd := exec.Command(BinaryName, "add-genesis-account", address, val.Bonded, "--home", HomeDir+"/"+strconv.Itoa(j)+"/")
			if err := cmd.Run(); err != nil {
				panic(err)
			}
		}

		{
			cmd := exec.Command(BinaryName, "gentx", "validator"+strconv.Itoa(i), val.Bonded, "--home", HomeDir+"/"+strconv.Itoa(i), "--keyring-backend", KeyringBackend, "--chain-id", config.Genesis["chain_id"].(string))
			b := bytes.NewBuffer(nil)
			cmd.Stdout = b
			cmd.Stderr = b

			stdin, err := cmd.StdinPipe()
			if err != nil {
				panic(err)
			}

			if err := cmd.Start(); err != nil {
				panic(err)
			}

			enterPassphrase(stdin)

			if err := cmd.Wait(); err != nil {
				println(b.String())
				panic(err)
			}
		}
	}

	for i := range config.Validators {
		entries, err := os.ReadDir(HomeDir + "/" + strconv.Itoa(i) + "/config/gentx/")
		if err != nil {
			panic(err)
		}

		output, err := exec.Command("mv", HomeDir+"/"+strconv.Itoa(i)+"/config/gentx/"+
			entries[0].Name(), HomeDir+"/0/config/gentx/validator"+strconv.Itoa(i)+".json").CombinedOutput()
		if err != nil {
			println(string(output))
			panic(err)
		}
	}

	if err := exec.Command(BinaryName, "collect-gentxs", "--keyring-backend", KeyringBackend, "--chain-id", config.Genesis["chain_id"].(string), "--home", HomeDir+"/0/").Run(); err != nil {
		panic(err)
	}

	for i := range config.Validators {
		if i == 0 {
			continue
		}

		output, err := exec.Command("cp", HomeDir+"/0/config/genesis.json", HomeDir+"/"+strconv.Itoa(i)+"/config/genesis.json").CombinedOutput()
		if err != nil {
			println(string(output))
			panic(err)
		}
	}
}

func addKey(dir, keyname string) string {
	cmd := exec.Command(BinaryName, "keys", "add", keyname, "--home", dir, "--keyring-backend", KeyringBackend, "--algo", "eth_secp256k1")

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

	{
		cmd := exec.Command(BinaryName, "keys", "show", keyname, "--home", dir, "--keyring-backend", KeyringBackend)
		result := bytes.Buffer{}
		cmd.Stdout = &result
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

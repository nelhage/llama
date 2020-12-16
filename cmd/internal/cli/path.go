package cli

import (
	"log"
	"os"
	"path"
)

func ConfigDir() string {
	if dir := os.Getenv("LLAMA_DIR"); dir != "" {
		return dir
	}
	if home := os.Getenv("HOME"); home != "" {
		return path.Join(home, ".llama")
	}

	dir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Cannot find homedir: %s", err.Error())
	}
	return path.Join(dir, ".llama")
}

func ConfigPath() string {
	return path.Join(ConfigDir(), "llama.json")
}

func SocketPath() string {
	return path.Join(ConfigDir(), "llama.sock")
}

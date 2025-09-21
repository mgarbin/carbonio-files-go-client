package main

import (
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/graphql"
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Main MainConfig `yaml:"Main"`
}

type MainConfig struct {
	Endpoint string `yaml:"endpoint"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func printAllFlags() {
	fmt.Println("Available flags:")
	flag.VisitAll(func(f *flag.Flag) {
		fmt.Printf("  -%s: %s (default: %q)\n", f.Name, f.Usage, f.DefValue)
	})
}

func main() {

	cfg, err := LoadConfig("config.yaml")
	if err != nil {
		panic(err)
	}

	carbonio := &carbonio.HTTPAuthenticator{Endpoint: cfg.Main.Endpoint}
	zmAuthToken, err := carbonio.CarbonioZxAuth(cfg.Main.Username, cfg.Main.Password)

	if err != nil {
		panic(err)
	}

	listAllNode := flag.Bool("getAllNode", false, "Use this flag to obtain all files node")
	printFlagInfo := flag.Bool("v", false, "output helper with all flags")

	// Parse the flags
	flag.Parse()

	if *printFlagInfo {
		printAllFlags()
	}

	if *listAllNode {
		fmt.Println("ZM_AUTH_TOKEN obatined :", zmAuthToken)
		fmt.Println("---------")
		fmt.Println("Here all nodes found with graphl query!")
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: zmAuthToken}
		base_folder := "LOCAL_ROOT"
		graphqlAuthenticator.GetAllNode(base_folder)
	}

}

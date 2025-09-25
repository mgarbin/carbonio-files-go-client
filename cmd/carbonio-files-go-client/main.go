package main

import (
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/graphql"
	"flag"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Main MainConfig `yaml:"Main"`
}

type MainConfig struct {
	Endpoint  string  `yaml:"endpoint"`
	Username  string  `yaml:"username"`
	Password  string  `yaml:"password"`
	AuthToken *string `yaml:"authToken"`
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

func recursiveNode(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, level int) {
	nodes, nodesErr := graphqlAuthenticator.GetAllNode(id, "NAME_ASC", nil, nil)
	if nodesErr != nil {
		panic(nodesErr)
	}

	var z string

	z = ""

	if level > 0 {
		z = strings.Repeat(" ", level)
	}

	for _, child := range nodes {
		fmt.Printf("%s|", z)
		if child.Type == "FOLDER" {
			fmt.Printf("%s (%s) \n", child.Name, child.Type)
			recursiveNode(graphqlAuthenticator, child.ID, level+1)
		} else {
			if child.Extension != nil {
				fmt.Printf("%s.%s (%s) \n", child.Name, *child.Extension, child.Type)
			} else {
				fmt.Printf("%s (%s) \n", child.Name, child.Type)
			}
		}
	}
}

func main() {

	cfg, err := LoadConfig("config.yaml")
	if err != nil {
		panic(err)
	}

	var zmAuthToken *string
	zmAuthToken = cfg.Main.AuthToken

	if zmAuthToken == nil {

		carbonio := &carbonio.HTTPAuthenticator{Endpoint: cfg.Main.Endpoint}
		carbonioToken, errCarbonioToken := carbonio.CarbonioZxAuth(cfg.Main.Username, cfg.Main.Password)

		if errCarbonioToken != nil {
			panic(errCarbonioToken)
		}

		if carbonioToken != nil {
			zmAuthToken = carbonioToken
		} else {
			panic("Invalid ZM_AUTH_TOKEN")
		}
	}

	listAllNode := flag.Bool("getAllNode", false, "Use this flag to obtain all files node")
	printFlagInfo := flag.Bool("v", false, "output helper with all flags")

	// Parse the flags
	flag.Parse()

	if *printFlagInfo {
		printAllFlags()
	}

	if *listAllNode {
		//fmt.Println("ZM_AUTH_TOKEN obatined :", zmAuthToken)
		//fmt.Println("---------")
		fmt.Println("Here all nodes found with graphl query!")
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		base_folder := "LOCAL_ROOT"
		recursiveNode(graphqlAuthenticator, base_folder, 0)
		/*nodes, nodesErr := graphqlAuthenticator.GetAllNode(base_folder, "NAME_ASC", nil, nil)
		if nodesErr != nil {
			panic(nodesErr)
		}
		recursiveNode(graphqlAuthenticator, base_folder, 1)

		for _, child := range nodes {
			if child.Type == "FOLDER" {
				fmt.Printf("%s (%s) \n", child.Name, child.Type)
				recursiveNode(graphqlAuthenticator, child.ID, 1)
			} else {
				if child.Extension != nil {
					fmt.Printf("%s.%s (%s) \n", child.Name, *child.Extension, child.Type)
				} else {
					fmt.Printf("%s (%s) \n", child.Name, child.Type)
				}
			}
		}*/
	}

	//obatin hash file openssl sha384 -binary /tmp/Rec_2025-09-09_1512_Hyperion_meeting_Agosto_2025_upgrade_25.6.webm | base64

}

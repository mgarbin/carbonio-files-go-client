package main

import (
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/graphql"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

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

func recursiveListNode(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, level int) {
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
			recursiveListNode(graphqlAuthenticator, child.ID, level+1)
		} else {
			if child.Extension != nil {
				fmt.Printf("%s.%s (%s) \n", child.Name, *child.Extension, child.Type)
			} else {
				fmt.Printf("%s (%s) \n", child.Name, child.Type)
			}
		}
	}
}

func createFolder(path string) error {
	err := os.Mkdir(path, 0755)
	if err != nil {
		if os.IsExist(err) {
			// Folder already exists, skip
			fmt.Errorf("folder already exist error: %w", err)
			return nil
		}
		// Other error, return it
		return err
	}
	return nil
}

func recursiveFileDownloader(graphqlAuthenticator *graphql.GraphQLAuthenticator, carbonio *carbonio.HTTPAuthenticator, id, folderPath string) {
	nodes, nodesErr := graphqlAuthenticator.GetAllNode(id, "NAME_ASC", nil, nil)
	if nodesErr != nil {
		panic(nodesErr)
	}

	maxRetries := 3

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1) // max 1 goroutines

	for _, child := range nodes {
		if child.Type == "FOLDER" {
			folderPath := folderPath + "/" + child.Name
			//fmt.Printf(folderPath + "\n")
			err := createFolder(folderPath)
			if err != nil {
				fmt.Errorf("folder create error: %w", err)
			}
			recursiveFileDownloader(graphqlAuthenticator, carbonio, child.ID, folderPath)
		} else {
			if child.Extension != nil {
				fileName := child.Name + "." + *child.Extension
				wg.Add(1)
				sem <- struct{}{} // acquire semaphore slot
				go func() {
					exitStat, downErr := carbonio.DownloadFile(graphqlAuthenticator.AuthToken, child.ID, folderPath, fileName, int64(*child.Size), maxRetries, &wg, sem)
					if downErr != nil {
						fmt.Printf("[ERROR] %s - ", downErr)
					}
					if exitStat != nil {
						fmt.Printf("[INFO] %s - ", *exitStat)
					}
					fmt.Printf("%s/%s.%s\n", folderPath, child.Name, *child.Extension)
				}()
			} else {
				wg.Add(1)
				sem <- struct{}{} // acquire semaphore slot
				go func() {
					exitStat, downErr := carbonio.DownloadFile(graphqlAuthenticator.AuthToken, child.ID, folderPath, child.Name, int64(*child.Size), maxRetries, &wg, sem)
					if downErr != nil {
						fmt.Printf("[ERROR] %s - ", downErr)
					}
					if exitStat != nil {
						fmt.Printf("[INFO] %s - ", *exitStat)
					}
					fmt.Printf("%s/%s\n", folderPath, child.Name)
				}()
			}
		}
	}

	wg.Wait()
}

func main() {

	cfg, err := LoadConfig("config.yaml")
	if err != nil {
		panic(err)
	}

	var zmAuthToken *string
	zmAuthToken = cfg.Main.AuthToken

	carbonio := &carbonio.HTTPAuthenticator{Endpoint: cfg.Main.Endpoint}

	if zmAuthToken == nil {

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
	downloadAllFiles := flag.Bool("downloadAllFiles", false, "Use this flag to create Folder directory tree and download all files")
	printFlagInfo := flag.Bool("v", false, "output helper with all flags")

	flag.Parse()

	if *printFlagInfo {
		printAllFlags()
	}

	if *listAllNode {
		fmt.Println("Here all nodes found with graphl query!")
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		base_folder := "LOCAL_ROOT"
		recursiveListNode(graphqlAuthenticator, base_folder, 0)
	}

	if *downloadAllFiles {
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		base_folder := "LOCAL_ROOT"
		recursiveFileDownloader(graphqlAuthenticator, carbonio, base_folder, "files")
	}

	//obatin hash file openssl sha384 -binary /tmp/Rec_2025-09-09_1512_Hyperion_meeting_Agosto_2025_upgrade_25.6.webm | base64

}

package utils

import (
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/graphql"
	"carbonio-files-go-client/pkg/localfs"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"
)

// RecursiveListNodeItems walks the remote node tree starting at id and returns
// a flat map keyed by relative path (folderPath-prefixed) to localfs.ItemInfo,
// mirroring the shape produced by localfs.ReadFolderRecursive so the two can
// be compared directly.
func RecursiveListNodeItems(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, folderPath string) (map[string]localfs.ItemInfo, error) {

	items := make(map[string]localfs.ItemInfo)

	nodes, nodesErr := graphqlAuthenticator.GetAllNode(id, "NAME_ASC", nil, nil)
	if nodesErr != nil {
		panic(nodesErr)
	}

	for _, child := range nodes {

		item := localfs.ItemInfo{}
		currentFilePath := ""

		if child.Type == "FOLDER" {
			item.IsFile = false
			item.NodeId = child.ID
			newFolderPath := ""
			if folderPath == "" {
				newFolderPath = child.Name
			} else {
				newFolderPath = folderPath + "/" + child.Name
			}
			newNodeItems, err := RecursiveListNodeItems(graphqlAuthenticator, child.ID, newFolderPath)
			if err != nil {
				return nil, err
			}
			maps.Insert(items, maps.All(newNodeItems))
			items[newFolderPath] = item
		} else {
			item.IsFile = true
			item.NodeId = child.ID
			item.Digest = *child.Digest
			item.Size = *child.Size
			item.ModifyTimestamp = *child.UpdatedAt
			item.FileVersion = *child.Version
			fileName := child.Name
			if child.Extension != nil {
				fileName = child.Name + "." + *child.Extension
			}
			if folderPath == "" {
				currentFilePath = fileName
			} else {
				currentFilePath = folderPath + "/" + fileName
			}
			items[currentFilePath] = item
		}
	}

	return items, nil
}

// RecursiveListNode prints the remote node tree starting at id, indenting
// each level to reflect folder depth.
func RecursiveListNode(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, level int) {
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
			RecursiveListNode(graphqlAuthenticator, child.ID, level+1)
		} else {
			if child.Extension != nil {
				fmt.Printf("%s.%s (%s) - DIGEST [%s] \n", child.Name, *child.Extension, child.Type, *child.Digest)
			} else {
				fmt.Printf("%s (%s) - DIGEST [%s]\n", child.Name, child.Type, *child.Digest)
			}
		}
	}
}

// CreateLocalFolder creates the directory at path, silently succeeding if it
// already exists.
func CreateLocalFolder(path string) error {
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

// EpochToTime converts an integer timestamp of unknown epoch precision
// (seconds, milliseconds, microseconds or nanoseconds) into a time.Time.
func EpochToTime(ts int64) time.Time {
	if ts > 1_000_000_000_000_000_000 {
		return time.Unix(0, ts)
	}
	if ts > 1_000_000_000_000_000 {
		return time.UnixMicro(ts)
	}
	if ts > 1_000_000_000_000 {
		return time.UnixMilli(ts)
	}
	return time.Unix(ts, 0)
}

// RecursiveFileDownloader recreates the remote folder tree rooted at id under
// folderPath on the local filesystem and downloads every file it finds.
func RecursiveFileDownloader(graphqlAuthenticator *graphql.GraphQLAuthenticator, carbonio *carbonio.HTTPAuthenticator, id, folderPath string) {
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
			err := CreateLocalFolder(folderPath)
			if err != nil {
				fmt.Errorf("folder create error: %w", err)
			}
			RecursiveFileDownloader(graphqlAuthenticator, carbonio, child.ID, folderPath)
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

package utils

import (
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/graphql"
	"carbonio-files-go-client/pkg/localfs"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// RecursiveListNodeItems walks the remote node tree starting at id and returns
// a flat map keyed by relative path (folderPath-prefixed) to localfs.ItemInfo,
// mirroring the shape produced by localfs.ReadFolderRecursive so the two can
// be compared directly.
func RecursiveListNodeItems(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, folderPath string) (map[string]localfs.ItemInfo, error) {

	items := make(map[string]localfs.ItemInfo)

	nodes, nodesErr := graphqlAuthenticator.GetAllNode(id, "NAME_ASC", nil, nil)
	if nodesErr != nil {
		return nil, nodesErr
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

// RecursiveListNode logs the remote node tree starting at id, one structured
// entry per node, with level indicating folder depth.
func RecursiveListNode(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, level int) {
	nodes, nodesErr := graphqlAuthenticator.GetAllNode(id, "NAME_ASC", nil, nil)
	if nodesErr != nil {
		panic(nodesErr)
	}

	for _, child := range nodes {
		evt := log.Info().Int("level", level).Str("name", child.Name).Str("type", child.Type)
		if child.Type == "FOLDER" {
			evt.Msg("Node")
			RecursiveListNode(graphqlAuthenticator, child.ID, level+1)
			continue
		}
		if child.Extension != nil {
			evt = evt.Str("extension", *child.Extension)
		}
		evt.Str("digest", *child.Digest).Msg("Node")
	}
}

// CreateLocalFolder creates the directory at path, silently succeeding if it
// already exists.
func CreateLocalFolder(path string) error {
	err := os.Mkdir(path, 0755)
	if err != nil {
		if os.IsExist(err) {
			log.Debug().Str("path", path).Msg("Folder already exists, skip")
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
			if err := CreateLocalFolder(folderPath); err != nil {
				log.Warn().Err(err).Str("path", folderPath).Msg("Creating local folder failed")
			}
			RecursiveFileDownloader(graphqlAuthenticator, carbonio, child.ID, folderPath)
		} else {
			if child.Extension != nil {
				fileName := child.Name + "." + *child.Extension
				wg.Add(1)
				sem <- struct{}{} // acquire semaphore slot
				go func() {
					exitStat, downErr := carbonio.DownloadFile(graphqlAuthenticator.AuthToken, child.ID, folderPath, fileName, int64(*child.Size), maxRetries, &wg, sem)
					destPath := folderPath + "/" + fileName
					if downErr != nil {
						log.Error().Err(downErr).Str("path", destPath).Msg("Download failed")
						return
					}
					evt := log.Info().Str("path", destPath)
					if exitStat != nil {
						evt = evt.Str("status", *exitStat)
					}
					evt.Msg("Downloaded file")
				}()
			} else {
				wg.Add(1)
				sem <- struct{}{} // acquire semaphore slot
				go func() {
					exitStat, downErr := carbonio.DownloadFile(graphqlAuthenticator.AuthToken, child.ID, folderPath, child.Name, int64(*child.Size), maxRetries, &wg, sem)
					destPath := folderPath + "/" + child.Name
					if downErr != nil {
						log.Error().Err(downErr).Str("path", destPath).Msg("Download failed")
						return
					}
					evt := log.Info().Str("path", destPath)
					if exitStat != nil {
						evt = evt.Str("status", *exitStat)
					}
					evt.Msg("Downloaded file")
				}()
			}
		}
	}

	wg.Wait()
}

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

// RecursiveListNodeItems walks the remote node tree starting at id and
// returns a flat map keyed by relative path (folderPath-prefixed) to
// localfs.ItemInfo, mirroring the shape produced by
// localfs.ReadFolderRecursive so the two can be compared directly.
//
// It is best-effort below the root: graphql.GraphQLAuthenticator.GetAllNode
// already retries transient errors internally, but if a subtree's listing
// still fails afterwards, only that subtree is skipped - its own entry
// (name, id, permissions - already known from its parent's successful
// response) is kept, only its descendants are missing - instead of
// discarding every already-fetched sibling folder. Its path is appended to
// the returned failedPaths. Only a failure fetching id itself (nothing
// usable retrieved at all) is fatal and returns a non-nil error.
//
// Callers MUST treat every path under a failedPaths entry (the entry
// itself, or anything prefixed with entry+"/") as an incomplete listing: a
// path tracked from a previous run that falls under one must never be
// inferred as remotely deleted just because it's absent from the returned
// map here - it may simply not have been fetched this cycle.
func RecursiveListNodeItems(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, folderPath string) (map[string]localfs.ItemInfo, []string, error) {
	return recursiveListNodeItems(graphqlAuthenticator, id, folderPath, true)
}

// recursiveListNodeItems is RecursiveListNodeItems' implementation. isRoot
// distinguishes the very first call (a GetAllNode failure there is fatal,
// nothing usable was retrieved) from every recursive call for a discovered
// subfolder (a failure there is merely recorded in failedPaths) -
// folderPath alone can't tell them apart from a child folder that happens
// to be named the same as its parent's path so far.
func recursiveListNodeItems(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, folderPath string, isRoot bool) (map[string]localfs.ItemInfo, []string, error) {

	items := make(map[string]localfs.ItemInfo)

	nodes, nodesErr := graphqlAuthenticator.GetAllNode(id, "NAME_ASC", nil, nil)
	if nodesErr != nil {
		if isRoot {
			return nil, nil, nodesErr
		}
		log.Warn().Err(nodesErr).Str("path", folderPath).
			Msg("Fetching remote subtree failed after retries, skipping it for this cycle")
		return items, []string{folderPath}, nil
	}

	var failedPaths []string

	for _, child := range nodes {

		item := localfs.ItemInfo{}
		if child.Permissions != nil {
			item.CanWriteFile = child.Permissions.CanWriteFile
			item.CanAddVersion = child.Permissions.CanAddVersion
			item.CanDelete = child.Permissions.CanDelete
		}
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
			newNodeItems, childFailedPaths, err := recursiveListNodeItems(graphqlAuthenticator, child.ID, newFolderPath, false)
			if err != nil {
				return nil, nil, err
			}
			maps.Insert(items, maps.All(newNodeItems))
			failedPaths = append(failedPaths, childFailedPaths...)
			items[newFolderPath] = item
		} else {
			item.IsFile = true
			item.NodeId = child.ID
			if child.Digest != nil {
				item.Digest = *child.Digest
			}
			if child.Size != nil {
				item.Size = *child.Size
			}
			if child.UpdatedAt != nil {
				item.ModifyTimestamp = *child.UpdatedAt
			}
			if child.Version != nil {
				item.FileVersion = *child.Version
			}
			if child.MimeType != nil {
				item.MimeType = *child.MimeType
			}
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

	return items, failedPaths, nil
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
			folderPath := folderPath + "/" + localfs.SanitizeRelativePath(child.Name)
			if err := CreateLocalFolder(folderPath); err != nil {
				log.Warn().Err(err).Str("path", folderPath).Msg("Creating local folder failed")
			}
			RecursiveFileDownloader(graphqlAuthenticator, carbonio, child.ID, folderPath)
		} else {
			remoteName := child.Name
			if child.Extension != nil {
				remoteName = child.Name + "." + *child.Extension
			}
			fileName := localfs.SanitizeRelativePath(remoteName)
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
		}
	}

	wg.Wait()
}

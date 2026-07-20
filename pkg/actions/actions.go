// Package actions holds the business logic behind every CLI flag exposed by
// carbonio-files-go-client. Flag declarations and parsing stay in main; each
// function here implements what used to be the body of one `if <flag>` block.
package actions

import (
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/graphql"
	"carbonio-files-go-client/pkg/localfs"
	sqlitecache "carbonio-files-go-client/pkg/sqlite"
	"carbonio-files-go-client/pkg/utils"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PrintFlagInfo prints every registered flag with its usage and default
// value. Backs the -v flag.
func PrintFlagInfo() {
	fmt.Println("Available flags:")
	flag.VisitAll(func(f *flag.Flag) {
		fmt.Printf("  -%s: %s (default: %q)\n", f.Name, f.Usage, f.DefValue)
	})
}

// ListAllNode prints the whole remote node tree starting from LOCAL_ROOT.
// Backs the -getAllNode flag.
func ListAllNode(endpoint, authToken string) {
	fmt.Println("Here all nodes found with graphl query!")
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: authToken}
	baseFolder := "LOCAL_ROOT"
	utils.RecursiveListNode(graphqlAuthenticator, baseFolder, 0)
}

// DownloadAllFiles recreates the remote folder tree locally under "files"
// and downloads every file. Backs the -downloadAllFiles flag.
func DownloadAllFiles(endpoint, authToken string, carbonioAuth *carbonio.HTTPAuthenticator) {
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: authToken}
	baseFolder := "LOCAL_ROOT"
	utils.RecursiveFileDownloader(graphqlAuthenticator, carbonioAuth, baseFolder, "files")
}

// UploadFile uploads uploadFile as a new node under parentId. Backs the
// -uploadFile flag.
func UploadFile(carbonioAuth *carbonio.HTTPAuthenticator, authToken, parentId, uploadFile string, nodeId *string) {
	newNodeID, uploadErr := carbonioAuth.UploadFile(authToken, parentId, uploadFile, false, false, nodeId)
	if uploadErr != nil {
		fmt.Println("[ERROR]:", uploadErr)
	} else {
		fmt.Println("[INFO] Uploaded file, nodeId:", newNodeID)
	}
}

// UploadNewVersionFile uploads uploadNewVersionFile as a new version of
// nodeId under parentId. Backs the -uploadNewVersionFile flag.
func UploadNewVersionFile(carbonioAuth *carbonio.HTTPAuthenticator, authToken, parentId, uploadNewVersionFile string, overwriteVersion bool, nodeId *string) {
	newNodeID, uploadErr := carbonioAuth.UploadFile(authToken, parentId, uploadNewVersionFile, true, overwriteVersion, nodeId)
	if uploadErr != nil {
		fmt.Println("[ERROR]:", uploadErr)
	} else {
		fmt.Println("[INFO] Uploaded new version, nodeId:", newNodeID)
	}
}

// CreateFolder creates a remote folder named folderName under parentId.
// Backs the -createFolder flag.
func CreateFolder(endpoint, authToken, parentId, folderName string) {
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: authToken}
	newFolder, err := graphqlAuthenticator.CreateFolder(parentId, folderName)
	if err != nil {
		fmt.Println("[ERROR]: ", err)
	} else {
		fmt.Println("[INFO] New folder id ", newFolder.ID)
	}
}

// MoveNodes moves the comma-separated nodesIdList to destinationId. Backs
// the -moveNodes flag. A non-nil error means the caller should abort, the
// message has already been printed.
func MoveNodes(endpoint, authToken, destinationId, nodesIdList string) error {
	if destinationId == "" || nodesIdList == "" {
		fmt.Println("Error: destinationId and nodesIdList must be provided for moveNodes")
		return fmt.Errorf("destinationId and nodesIdList must be provided for moveNodes")
	}
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: authToken}
	nodeIDs := strings.Split(nodesIdList, ",")
	moveResp, err := graphqlAuthenticator.MoveNodes(nodeIDs, destinationId)
	if err != nil {
		fmt.Printf("[ERROR] moving nodes: %v\n", err)
		return err
	}
	fmt.Printf("[INFO] Moved nodes to destination %s: %v\n", destinationId, moveResp)
	return nil
}

// TrashNodes moves the comma-separated nodesIdList to trash. Backs the
// -trashNodes flag. A non-nil error means the caller should abort, the
// message has already been printed.
func TrashNodes(endpoint, authToken, nodesIdList string) error {
	if nodesIdList == "" {
		fmt.Println("Error: nodesIdList must be provided for trashNodes")
		return fmt.Errorf("nodesIdList must be provided for trashNodes")
	}
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: authToken}
	nodeIDs := strings.Split(nodesIdList, ",")
	trashResp, err := graphqlAuthenticator.TrashNodes(nodeIDs)
	if err != nil {
		fmt.Printf("[ERROR] trashing nodes: %v\n", err)
		return err
	}
	fmt.Printf("[INFO] Trashed nodes: %v\n", trashResp)
	return nil
}

// DeleteNodes permanently deletes the comma-separated nodesIdList. Backs the
// -deleteNodes flag. A non-nil error means the caller should abort, the
// message has already been printed.
func DeleteNodes(endpoint, authToken, nodesIdList string) error {
	if nodesIdList == "" {
		fmt.Println("Error: nodesIdList must be provided for deleteNodes")
		return fmt.Errorf("nodesIdList must be provided for deleteNodes")
	}
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: authToken}
	nodeIDs := strings.Split(nodesIdList, ",")
	deleteResp, err := graphqlAuthenticator.DeleteNodes(nodeIDs)
	if err != nil {
		fmt.Printf("[ERROR] deleting nodes: %v\n", err)
		return err
	}
	fmt.Printf("[INFO] Deleted nodes: %v\n", deleteResp)
	return nil
}

// LiveSyncCheck compares localFolder against the remote tree and prints the
// differences found. Backs the -liveSyncCheck flag. A non-nil error means
// the caller should abort, the message has already been printed.
func LiveSyncCheck(endpoint, authToken, localFolder string, cacheSync bool) error {

	if cacheSync {
		fmt.Println("Cache sync not yet implemented")
	}

	localMapItems, err := localfs.ReadFolderRecursive(localFolder, false)
	if err != nil {
		fmt.Println("Error:", err)
		return err
	}

	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: authToken}
	baseFolder := "LOCAL_ROOT"

	remoteMapItems, err := utils.RecursiveListNodeItems(graphqlAuthenticator, baseFolder, "")
	if err != nil {
		fmt.Println("Error:", err)
		return err
	}

	diffs := localfs.ComparePathMapsMulti(localMapItems, remoteMapItems)
	for path, diffList := range diffs {
		fmt.Printf("Path: %s\n", path)
		for _, diff := range diffList {
			fmt.Printf("  Difference: %s\n", diff.Diff)
			if diff.Local != nil {
				fmt.Printf("    Local: %+v\n", *diff.Local)
			}
			if diff.Remote != nil {
				fmt.Printf("    Remote: %+v\n", *diff.Remote)
			}
		}
	}

	return nil
}

// UpdateCacheSync initializes (or refreshes) the sqlite sync cache with the
// current local/remote state. Backs the -updateCacheSync flag. A non-nil
// error means the caller should abort, the message has already been
// printed.
func UpdateCacheSync(endpoint, authToken, localFolder string) error {

	// Initialize SQLite database
	newdb, err := sqlitecache.NewSqliteHelper("./file_sync_cache.db")
	if err != nil {
		fmt.Println("Error:", err)
		return err
	}
	defer newdb.Close()
	fmt.Println("SQLite cache initialized successfully")

	localMapItems, err := localfs.ReadFolderRecursive(localFolder, false)
	if err != nil {
		fmt.Println("Error reading local folder:", err)
		return err
	}
	fmt.Printf("Found %d local items\n", len(localMapItems))

	// Fetch remote items from GraphQL
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: authToken}
	baseFolder := "LOCAL_ROOT"
	remoteMapItems, err := utils.RecursiveListNodeItems(graphqlAuthenticator, baseFolder, "")
	if err != nil {
		fmt.Println("Error fetching remote items:", err)
		return err
	}
	fmt.Printf("Found %d remote items\n", len(remoteMapItems))

	// Check if the database is already populated
	count, countErr := newdb.CountRecords()
	if countErr != nil {
		fmt.Println("Error counting records:", countErr)
		return countErr
	}

	now := time.Now().Format(time.RFC3339)
	insertCount := 0

	// trackedPaths collects every path already covered by an existing DB record.
	// New paths not present in this set will be inserted as fresh entries.
	trackedPaths := make(map[string]struct{})

	if count > 0 {
		// DB is already populated: detect which local or remote files have been deleted
		// and update their flags accordingly.
		allRecords, err := newdb.QueryAll()
		if err != nil {
			fmt.Println("Error querying existing records:", err)
			return err
		}

		for _, rec := range allRecords {
			updateFields := make(map[string]interface{})

			if rec.LocalPath != "" {
				trackedPaths[rec.LocalPath] = struct{}{}
				if rec.LocalDeleted == 0 {
					if _, exists := localMapItems[rec.LocalPath]; !exists {
						updateFields["local_deleted"] = 1
						fmt.Printf("[INFO] Local file deleted: %s\n", rec.LocalPath)
					}
				}
			}

			if rec.RemotePath != "" {
				trackedPaths[rec.RemotePath] = struct{}{}
				if rec.RemoteDeleted == 0 {
					if _, exists := remoteMapItems[rec.RemotePath]; !exists {
						updateFields["remote_deleted"] = 1
						fmt.Printf("[INFO] Remote file deleted: %s\n", rec.RemotePath)
					}
				}
			}

			// Update remote fields when the remote node exists and its content has changed.
			hasContentUpdate := false
			if rec.RemotePath != "" && rec.RemoteDeleted == 0 {
				if remoteItem, exists := remoteMapItems[rec.RemotePath]; exists {
					newRemoteSize := int64(remoteItem.Size)
					newRemoteDigest := remoteItem.Digest
					newRemoteLastModified := strconv.FormatInt(remoteItem.ModifyTimestamp, 10)
					if newRemoteSize != rec.RemoteSize || newRemoteDigest != rec.RemoteDigest || newRemoteLastModified != rec.RemoteLastModified {
						updateFields["remote_size"] = newRemoteSize
						updateFields["remote_digest"] = newRemoteDigest
						updateFields["remote_last_modified"] = newRemoteLastModified
						hasContentUpdate = true
						fmt.Printf("[INFO] Remote node updated: %s\n", rec.RemotePath)
					}
				}
			}

			// Update local fields when the local item exists and its content has changed.
			if rec.LocalPath != "" && rec.LocalDeleted == 0 {
				if localItem, exists := localMapItems[rec.LocalPath]; exists {
					newLocalSize := int64(localItem.Size)
					newLocalDigest := localItem.Digest
					newLocalLastModified := strconv.FormatInt(localItem.ModifyTimestamp, 10)
					if newLocalSize != rec.LocalSize || newLocalDigest != rec.LocalDigest || newLocalLastModified != rec.LocalLastModified {
						updateFields["local_size"] = newLocalSize
						updateFields["local_digest"] = newLocalDigest
						updateFields["local_last_modified"] = newLocalLastModified
						hasContentUpdate = true
						fmt.Printf("[INFO] Local item updated: %s\n", rec.LocalPath)
					}
				}
			}

			// Recalculate sync_status when content fields changed and both sides are present
			// and non-deleted. This keeps the sync_status accurate after remote or local updates.
			_, localBeingDeleted := updateFields["local_deleted"]
			_, remoteBeingDeleted := updateFields["remote_deleted"]
			if hasContentUpdate && rec.RemotePath != "" && rec.LocalPath != "" &&
				!localBeingDeleted && !remoteBeingDeleted &&
				rec.LocalDeleted == 0 && rec.RemoteDeleted == 0 {
				finalRemoteDigest := rec.RemoteDigest
				if v, ok := updateFields["remote_digest"]; ok {
					finalRemoteDigest = v.(string)
				}
				finalRemoteSize := rec.RemoteSize
				if v, ok := updateFields["remote_size"]; ok {
					finalRemoteSize = v.(int64)
				}
				finalLocalDigest := rec.LocalDigest
				if v, ok := updateFields["local_digest"]; ok {
					finalLocalDigest = v.(string)
				}
				finalLocalSize := rec.LocalSize
				if v, ok := updateFields["local_size"]; ok {
					finalLocalSize = v.(int64)
				}
				if finalRemoteDigest == finalLocalDigest && finalRemoteSize == finalLocalSize {
					updateFields["sync_status"] = "synced"
				} else {
					updateFields["sync_status"] = "out_of_sync"
				}
			}

			if len(updateFields) > 0 {
				if updateErr := newdb.UpdateFileSync("id", rec.ID, updateFields); updateErr != nil {
					fmt.Printf("[WARN] DB update for record %d: %v\n", rec.ID, updateErr)
				}
			}
		}
	} else {
		// DB is empty: reset auto-increment counter and do a full fresh initialization.
		if err = newdb.DeleteAllAndResetAutoIncrement(); err != nil {
			fmt.Println("Error clearing cache:", err)
			return err
		}
	}

	// Build the union of paths that are not yet tracked in the DB.
	allPaths := make(map[string]struct{})
	for p := range localMapItems {
		if _, tracked := trackedPaths[p]; !tracked {
			allPaths[p] = struct{}{}
		}
	}
	for p := range remoteMapItems {
		if _, tracked := trackedPaths[p]; !tracked {
			allPaths[p] = struct{}{}
		}
	}

	// Insert each untracked item into SQLite.
	for itemPath := range allPaths {
		localItem, hasLocal := localMapItems[itemPath]
		remoteItem, hasRemote := remoteMapItems[itemPath]

		nodeID := ""
		isDirectory := false
		remotePath := ""
		remotePathHash := ""
		localPath := ""
		localPathHash := ""
		remoteLastModified := ""
		localLastModified := ""
		var remoteSize int64
		var localSize int64
		remoteDigest := ""
		localDigest := ""
		localDeleted := 0
		remoteDeleted := 0

		if hasRemote {
			remotePath = itemPath
			remotePathHash = localfs.PathHash(itemPath)
			nodeID = remoteItem.NodeId
			isDirectory = !remoteItem.IsFile
			remoteLastModified = strconv.FormatInt(remoteItem.ModifyTimestamp, 10)
			remoteSize = int64(remoteItem.Size)
			remoteDigest = remoteItem.Digest
			if remoteItem.DeleteTimestamp != 0 {
				remoteDeleted = 1
			}
		}

		if hasLocal {
			localPath = itemPath
			localPathHash = localfs.PathHash(itemPath)
			isDirectory = !localItem.IsFile
			localLastModified = strconv.FormatInt(localItem.ModifyTimestamp, 10)
			localSize = int64(localItem.Size)
			localDigest = localItem.Digest
		}

		// Determine sync status based on presence and content comparison
		syncStatus := "unknown"
		if hasLocal && hasRemote {
			if localDigest == remoteDigest && localSize == remoteSize {
				syncStatus = "synced"
			} else {
				syncStatus = "out_of_sync"
			}
		} else if hasLocal {
			syncStatus = "local_only"
		} else if hasRemote {
			syncStatus = "remote_only"
		}

		_, insertErr := newdb.InsertFileSync(
			nodeID, "", remotePath, remotePathHash, localPath, localPathHash,
			isDirectory,
			remoteLastModified, localLastModified,
			remoteSize, localSize,
			remoteDigest, localDigest, syncStatus, now,
			localDeleted, remoteDeleted,
		)
		if insertErr != nil {
			fmt.Printf("Error inserting %s: %v\n", itemPath, insertErr)
		} else {
			insertCount++
		}
	}

	if count > 0 {
		fmt.Printf("Cache sync updated: deletions detected, %d new items inserted\n", insertCount)
	} else {
		fmt.Printf("Cache sync initialized with %d items\n", insertCount)
	}

	return nil
}

// LiveCacheSync reconciles localFolder and the remote tree using the sqlite
// sync cache: it downloads remote-only items, uploads local-only items,
// resolves out-of-sync items by timestamp, and propagates deletions in both
// directions. Backs the -liveCacheSync flag. A non-nil error means the
// caller should abort, the message has already been printed.
func LiveCacheSync(endpoint, authToken, localFolder string, carbonioAuth *carbonio.HTTPAuthenticator) error {

	// Open the existing SQLite cache database
	cacheDb, err := sqlitecache.NewSqliteHelper("./file_sync_cache.db")
	if err != nil {
		fmt.Println("Error opening cache:", err)
		return err
	}
	defer cacheDb.Close()

	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: authToken}

	// Build a path → node_id map from every record that already has a remote presence
	allRecords, err := cacheDb.QueryAll()
	if err != nil {
		fmt.Println("Error querying cache:", err)
		return err
	}
	pathToNodeID := make(map[string]string)
	for _, rec := range allRecords {
		if rec.NodeID != "" && rec.RemotePath != "" {
			pathToNodeID[rec.RemotePath] = rec.NodeID
		}
	}

	maxRetries := 3
	now := time.Now().Format(time.RFC3339)

	// --- Download remote_only items to local ---
	remoteOnly, err := cacheDb.QueryBySyncStatus("remote_only")
	if err != nil {
		fmt.Println("Error querying remote_only:", err)
		return err
	}
	fmt.Printf("[INFO] Found %d remote_only items to download\n", len(remoteOnly))

	// Process shallowest paths first so parent directories are created before children
	sort.Slice(remoteOnly, func(i, j int) bool {
		return strings.Count(remoteOnly[i].RemotePath, "/") < strings.Count(remoteOnly[j].RemotePath, "/")
	})

	for _, rec := range remoteOnly {
		if rec.RemoteDeleted != 0 {
			continue
		}
		if rec.IsDirectory {
			localDirPath := filepath.Join(localFolder, filepath.FromSlash(rec.RemotePath))
			if err := os.MkdirAll(localDirPath, 0755); err != nil {
				fmt.Printf("[ERROR] creating local dir %s: %v\n", localDirPath, err)
				continue
			}
			fmt.Printf("[INFO] Created local dir: %s\n", localDirPath)
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"local_path":      rec.RemotePath,
				"local_path_hash": localfs.PathHash(rec.RemotePath),
				"sync_status":     "synced",
				"last_synced":     now,
			}); updateErr != nil {
				fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
			}
		} else {
			dirPart := path.Dir(rec.RemotePath)
			fileName := path.Base(rec.RemotePath)
			destPath := localFolder
			if dirPart != "." {
				destPath = filepath.Join(localFolder, filepath.FromSlash(dirPart))
			}
			if err := os.MkdirAll(destPath, 0755); err != nil {
				fmt.Printf("[ERROR] creating local dir %s: %v\n", destPath, err)
				continue
			}
			var wg sync.WaitGroup
			sem := make(chan struct{}, 1)
			wg.Add(1)
			sem <- struct{}{}
			exitStat, downErr := carbonioAuth.DownloadFile(authToken, rec.NodeID, destPath, fileName, rec.RemoteSize, maxRetries, &wg, sem)
			wg.Wait()
			if downErr != nil {
				fmt.Printf("[ERROR] downloading %s: %v\n", rec.RemotePath, downErr)
				continue
			} else if exitStat != nil {
				fmt.Printf("[INFO] %s - %s\n", *exitStat, rec.RemotePath)
			}
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"local_path":      rec.RemotePath,
				"local_path_hash": localfs.PathHash(rec.RemotePath),
				"local_size":      rec.RemoteSize,
				"local_digest":    rec.RemoteDigest,
				"sync_status":     "synced",
				"last_synced":     now,
			}); updateErr != nil {
				fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
			}
		}
	}

	// --- Upload local_only items to remote ---
	localOnly, err := cacheDb.QueryBySyncStatus("local_only")
	if err != nil {
		fmt.Println("Error querying local_only:", err)
		return err
	}
	fmt.Printf("[INFO] Found %d local_only items to upload\n", len(localOnly))

	// Process shallowest paths first so parent folders are created on remote before their children
	sort.Slice(localOnly, func(i, j int) bool {
		return strings.Count(localOnly[i].LocalPath, "/") < strings.Count(localOnly[j].LocalPath, "/")
	})

	for _, rec := range localOnly {
		if rec.LocalDeleted != 0 {
			continue
		}
		parentPath := path.Dir(rec.LocalPath)
		//print parentPath
		fmt.Printf("[DEBUG] Processing %s, parent path: %s\n", rec.LocalPath, parentPath)
		parentNodeID := "LOCAL_ROOT"
		if parentPath != "." {
			if id, ok := pathToNodeID[parentPath]; ok {
				parentNodeID = id
				fmt.Printf("[DEBUG] Found parent node ID in cache for %s: %s\n", parentPath, parentNodeID)
			} else {
				// Fall back to a direct DB lookup for an existing folder at that path
				parentRec, dbErr := cacheDb.QueryFolderByPath(parentPath)
				if dbErr != nil {
					fmt.Printf("[ERROR] failed to query parent folder %s: %v\n", parentPath, dbErr)
				}
				if parentRec != nil && parentRec.NodeID != "" {
					parentNodeID = parentRec.NodeID
					pathToNodeID[parentPath] = parentRec.NodeID
					fmt.Printf("[DEBUG] Found parent node ID in DB for %s: %s\n", parentPath, parentNodeID)
					if parentRec.RemotePath != parentPath {
						fmt.Printf("[WARN] remote path mismatch for parent folder %s: cache has %s\n", parentPath, parentRec.RemotePath)
					}
					if parentRec.LocalDeleted != 0 || parentRec.RemoteDeleted != 0 {
						fmt.Printf("[WARN] parent folder %s is marked deleted in cache, using LOCAL_ROOT as parent for %s\n", parentPath, rec.LocalPath)
						parentNodeID = "LOCAL_ROOT"
					}
				} else {
					fmt.Printf("[WARN] remote parent folder %s not found in cache, using LOCAL_ROOT for %s\n", parentPath, rec.LocalPath)
				}
			}
		}
		if rec.IsDirectory {
			folderName := path.Base(rec.LocalPath)
			newFolder, err := graphqlAuthenticator.CreateFolder(parentNodeID, folderName)
			if err != nil {
				fmt.Printf("[ERROR] creating remote folder %s: %v\n", rec.LocalPath, err)
				continue
			}
			if newFolder != nil {
				pathToNodeID[rec.LocalPath] = newFolder.ID
				fmt.Printf("[INFO] Created remote folder: %s (id: %s)\n", rec.LocalPath, newFolder.ID)
				if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
					"node_id":          newFolder.ID,
					"remote_path":      rec.LocalPath,
					"remote_path_hash": localfs.PathHash(rec.LocalPath),
					"sync_status":      "synced",
					"last_synced":      now,
				}); updateErr != nil {
					fmt.Printf("[WARN] DB update for %s: %v\n", rec.LocalPath, updateErr)
				}
			}
		} else {
			filePath := filepath.Join(localFolder, filepath.FromSlash(rec.LocalPath))
			uploadedNodeID, uploadErr := carbonioAuth.UploadFile(authToken, parentNodeID, filePath, false, false, nil)
			if uploadErr != nil {
				fmt.Printf("[ERROR] uploading %s: %v\n", rec.LocalPath, uploadErr)
				continue
			}
			fmt.Printf("[INFO] Uploaded: %s (nodeId: %s)\n", rec.LocalPath, uploadedNodeID)
			pathToNodeID[rec.LocalPath] = uploadedNodeID
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"node_id":          uploadedNodeID,
				"remote_path":      rec.LocalPath,
				"remote_path_hash": localfs.PathHash(rec.LocalPath),
				"remote_size":      rec.LocalSize,
				"remote_digest":    rec.LocalDigest,
				"sync_status":      "synced",
				"last_synced":      now,
			}); updateErr != nil {
				fmt.Printf("[WARN] DB update for %s: %v\n", rec.LocalPath, updateErr)
			}
		}
	}

	// --- Resolve out_of_sync items ---
	outOfSync, err := cacheDb.QueryBySyncStatus("out_of_sync")
	if err != nil {
		fmt.Println("Error querying out_of_sync:", err)
		return err
	}
	fmt.Printf("[INFO] Found %d out_of_sync items to process\n", len(outOfSync))

	for _, rec := range outOfSync {
		if rec.LocalDeleted != 0 || rec.RemoteDeleted != 0 {
			continue
		}
		if rec.IsDirectory {
			// Directories have no file content; mark them synced.
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"sync_status": "synced",
				"last_synced": now,
			}); updateErr != nil {
				fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
			}
			continue
		}

		// Parse timestamps so we can decide which side is more recent.
		var remoteTs, localTs int64
		if rec.RemoteLastModified != "" {
			var parseErr error
			remoteTs, parseErr = strconv.ParseInt(rec.RemoteLastModified, 10, 64)
			if parseErr != nil {
				fmt.Printf("[WARN] could not parse remote_last_modified for %s: %v\n", rec.RemotePath, parseErr)
			}
		}
		if rec.LocalLastModified != "" {
			var parseErr error
			localTs, parseErr = strconv.ParseInt(rec.LocalLastModified, 10, 64)
			if parseErr != nil {
				fmt.Printf("[WARN] could not parse local_last_modified for %s: %v\n", rec.LocalPath, parseErr)
			}
		}

		// Verify that content actually differs before acting.
		// Use digest comparison when both sides have a digest, otherwise fall back to size.
		contentDiffers := rec.RemoteSize != rec.LocalSize
		if rec.RemoteDigest != "" && rec.LocalDigest != "" {
			contentDiffers = rec.RemoteDigest != rec.LocalDigest
		}
		if !contentDiffers {
			// Content is identical despite the out_of_sync status; just update the flag.
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"sync_status": "synced",
				"last_synced": now,
			}); updateErr != nil {
				fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
			}
			continue
		}

		//convert remoteTs and localTs to time.Time for better readability in logs
		remoteTime := utils.EpochToTime(remoteTs)
		localTime := utils.EpochToTime(localTs)

		if remoteTime.Equal(localTime) || remoteTime.After(localTime) {
			// Remote is more recent (or timestamps are equal): download the remote version.
			fmt.Printf("[INFO] Remote version is more recent for %s; downloading update\n", rec.RemotePath)
			dirPart := path.Dir(rec.RemotePath)
			fileName := path.Base(rec.RemotePath)
			destPath := localFolder
			if dirPart != "." {
				destPath = filepath.Join(localFolder, filepath.FromSlash(dirPart))
			}
			if err := os.MkdirAll(destPath, 0755); err != nil {
				fmt.Printf("[ERROR] creating local dir %s: %v\n", destPath, err)
				continue
			}
			var wg sync.WaitGroup
			sem := make(chan struct{}, 1)
			wg.Add(1)
			sem <- struct{}{}
			exitStat, downErr := carbonioAuth.DownloadFile(authToken, rec.NodeID, destPath, fileName, rec.RemoteSize, maxRetries, &wg, sem)
			wg.Wait()
			if downErr != nil {
				fmt.Printf("[ERROR] downloading updated file %s: %v\n", rec.RemotePath, downErr)
				continue
			} else if exitStat != nil {
				fmt.Printf("[INFO] %s - %s\n", *exitStat, rec.RemotePath)
			}
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"local_path":          rec.RemotePath,
				"local_path_hash":     localfs.PathHash(rec.RemotePath),
				"local_size":          rec.RemoteSize,
				"local_digest":        rec.RemoteDigest,
				"local_last_modified": rec.RemoteLastModified,
				"sync_status":         "synced",
				"last_synced":         now,
			}); updateErr != nil {
				fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
			}
		} else {
			// Local is more recent: upload a new version to remote.
			fmt.Printf("[INFO] Local version is more recent for %s; uploading update\n", rec.LocalPath)
			parentPath := path.Dir(rec.LocalPath)
			parentNodeID := "LOCAL_ROOT"
			if parentPath != "." {
				if id, ok := pathToNodeID[parentPath]; ok {
					parentNodeID = id
				} else {
					parentRec, dbErr := cacheDb.QueryFolderByPath(parentPath)
					if dbErr != nil {
						fmt.Printf("[ERROR] failed to query parent folder %s: %v\n", parentPath, dbErr)
					}
					if parentRec != nil && parentRec.NodeID != "" {
						parentNodeID = parentRec.NodeID
						pathToNodeID[parentPath] = parentRec.NodeID
					}
				}
			}
			filePath := filepath.Join(localFolder, filepath.FromSlash(rec.LocalPath))
			nodeIDStr := rec.NodeID
			uploadedNodeID, uploadErr := carbonioAuth.UploadFile(authToken, parentNodeID, filePath, true, false, &nodeIDStr)
			if uploadErr != nil {
				fmt.Printf("[ERROR] uploading new version %s: %v\n", rec.LocalPath, uploadErr)
				continue
			}
			fmt.Printf("[INFO] Uploaded new version: %s (nodeId: %s)\n", rec.LocalPath, uploadedNodeID)
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"remote_size":          rec.LocalSize,
				"remote_digest":        rec.LocalDigest,
				"remote_last_modified": rec.LocalLastModified,
				"sync_status":          "synced",
				"last_synced":          now,
			}); updateErr != nil {
				fmt.Printf("[WARN] DB update for %s: %v\n", rec.LocalPath, updateErr)
			}
		}
	}

	// --- Delete local items whose remote counterpart has been deleted ---
	remoteDeleted, err := cacheDb.QueryRemoteDeleted()
	if err != nil {
		fmt.Println("Error querying remote deleted:", err)
		return err
	}
	fmt.Printf("[INFO] Found %d remote deleted items to clean up locally\n", len(remoteDeleted))

	// Process deepest paths first so child files/dirs are removed before their parents.
	sort.Slice(remoteDeleted, func(i, j int) bool {
		di := strings.Count(remoteDeleted[i].LocalPath, "/")
		dj := strings.Count(remoteDeleted[j].LocalPath, "/")
		if di != dj {
			return di > dj
		}
		return remoteDeleted[i].LocalPath > remoteDeleted[j].LocalPath
	})

	for _, rec := range remoteDeleted {
		localItemPath := filepath.Join(localFolder, filepath.FromSlash(rec.LocalPath))
		var removeErr error
		if rec.IsDirectory {
			removeErr = os.RemoveAll(localItemPath)
		} else {
			removeErr = os.Remove(localItemPath)
		}
		if removeErr != nil {
			if !os.IsNotExist(removeErr) {
				fmt.Printf("[ERROR] removing local %s: %v\n", localItemPath, removeErr)
				continue
			}
			// File already absent locally – still update the DB record.
		} else {
			fmt.Printf("[INFO] Deleted local item (remote was deleted): %s\n", localItemPath)
		}
		if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
			"local_deleted": 1,
			"sync_status":   "remote_deleted",
			"last_synced":   now,
		}); updateErr != nil {
			fmt.Printf("[WARN] DB update for %s: %v\n", rec.LocalPath, updateErr)
		}
	}

	// --- Trash remote items whose local counterpart has been deleted ---
	localDeleted, err := cacheDb.QueryLocalDeleted()
	if err != nil {
		fmt.Println("Error querying local deleted:", err)
		return err
	}
	fmt.Printf("[INFO] Found %d locally deleted items to remove from remote\n", len(localDeleted))

	// Process deepest paths first so child files/dirs are removed before their parents.
	sort.Slice(localDeleted, func(i, j int) bool {
		di := strings.Count(localDeleted[i].RemotePath, "/")
		dj := strings.Count(localDeleted[j].RemotePath, "/")
		if di != dj {
			return di > dj
		}
		return localDeleted[i].RemotePath > localDeleted[j].RemotePath
	})

	for _, rec := range localDeleted {
		_, trashErr := graphqlAuthenticator.TrashNodes([]string{rec.NodeID})
		if trashErr != nil {
			fmt.Printf("[ERROR] trashing remote %s (nodeId: %s): %v\n", rec.RemotePath, rec.NodeID, trashErr)
			continue
		}
		fmt.Printf("[INFO] Trashed remote item (local was deleted): %s\n", rec.RemotePath)
		if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
			"remote_deleted": 1,
			"sync_status":    "local_deleted",
			"last_synced":    now,
		}); updateErr != nil {
			fmt.Printf("[WARN] DB update for %s: %v\n", rec.RemotePath, updateErr)
		}
	}

	fmt.Println("[INFO] liveCacheSync completed.")
	return nil
}

// Package actions holds the business logic behind every CLI flag exposed by
// carbonio-files-go-client. Flag declarations and parsing stay in main; each
// function here implements what used to be the body of one `if <flag>` block.
package actions

import (
	"carbonio-files-go-client/pkg/appdir"
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

	"github.com/rs/zerolog/log"
)

// PrintFlagInfo prints every registered flag with its usage and default
// value. Backs the -v flag.
func PrintFlagInfo() {
	log.Info().Msg("Available flags:")
	flag.VisitAll(func(f *flag.Flag) {
		log.Info().Str("flag", f.Name).Str("usage", f.Usage).Str("default", f.DefValue).Msg("flag")
	})
}

// ListAllNode prints the whole remote node tree starting from LOCAL_ROOT.
// Backs the -getAllNode flag.
func ListAllNode(endpoint string, session *carbonio.Session) {
	log.Info().Msg("Here all nodes found with graphl query!")
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: session.Token(), TokenRefresher: session.Reauthenticate}
	baseFolder := "LOCAL_ROOT"
	utils.RecursiveListNode(graphqlAuthenticator, baseFolder, 0)
}

// DownloadAllFiles recreates the remote folder tree locally under "files"
// and downloads every file. Backs the -downloadAllFiles flag.
func DownloadAllFiles(endpoint string, session *carbonio.Session, carbonioAuth *carbonio.HTTPAuthenticator) {
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: session.Token(), TokenRefresher: session.Reauthenticate}
	baseFolder := "LOCAL_ROOT"
	utils.RecursiveFileDownloader(graphqlAuthenticator, carbonioAuth, baseFolder, "files")
}

// UploadFile uploads uploadFile as a new node under parentId. Backs the
// -uploadFile flag.
func UploadFile(carbonioAuth *carbonio.HTTPAuthenticator, session *carbonio.Session, parentId, uploadFile string, nodeId *string) {
	newNodeID, uploadErr := carbonioAuth.UploadFile(session.Token(), parentId, uploadFile, false, false, nodeId)
	if uploadErr != nil {
		log.Error().Err(uploadErr).Msg("Upload file failed")
	} else {
		log.Info().Str("nodeId", newNodeID).Msg("Uploaded file")
	}
}

// UploadNewVersionFile uploads uploadNewVersionFile as a new version of
// nodeId under parentId. Backs the -uploadNewVersionFile flag.
func UploadNewVersionFile(carbonioAuth *carbonio.HTTPAuthenticator, session *carbonio.Session, parentId, uploadNewVersionFile string, overwriteVersion bool, nodeId *string) {
	newNodeID, uploadErr := carbonioAuth.UploadFile(session.Token(), parentId, uploadNewVersionFile, true, overwriteVersion, nodeId)
	if uploadErr != nil {
		log.Error().Err(uploadErr).Msg("Upload new version failed")
	} else {
		log.Info().Str("nodeId", newNodeID).Msg("Uploaded new version")
	}
}

// CreateFolder creates a remote folder named folderName under parentId.
// Backs the -createFolder flag.
func CreateFolder(endpoint string, session *carbonio.Session, parentId, folderName string) {
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: session.Token(), TokenRefresher: session.Reauthenticate}
	newFolder, err := graphqlAuthenticator.CreateFolder(parentId, folderName)
	if err != nil {
		log.Error().Err(err).Msg("Create folder failed")
	} else {
		log.Info().Str("nodeId", newFolder.ID).Msg("New folder created")
	}
}

// MoveNodes moves the comma-separated nodesIdList to destinationId. Backs
// the -moveNodes flag. A non-nil error means the caller should abort, the
// message has already been printed.
func MoveNodes(endpoint string, session *carbonio.Session, destinationId, nodesIdList string) error {
	if destinationId == "" || nodesIdList == "" {
		log.Error().Msg("destinationId and nodesIdList must be provided for moveNodes")
		return fmt.Errorf("destinationId and nodesIdList must be provided for moveNodes")
	}
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: session.Token(), TokenRefresher: session.Reauthenticate}
	nodeIDs := strings.Split(nodesIdList, ",")
	moveResp, err := graphqlAuthenticator.MoveNodes(nodeIDs, destinationId)
	if err != nil {
		log.Error().Err(err).Msg("Moving nodes failed")
		return err
	}
	log.Info().Str("destinationId", destinationId).Strs("movedNodes", moveResp).Msg("Moved nodes")
	return nil
}

// DeleteModeTrash and DeleteModeDelete are the two values accepted for the
// "deleteRemoteNode" preference (config.yaml Sync.deleteRemoteNode or the
// GUI's Preferences > Synchronization dropdown, persisted as
// sqlitecache.ConfigRecord.DeleteRemoteNode). They select which action
// LiveCacheSync uses to propagate a local deletion to the remote node: see
// resolveDeleteMode.
const (
	DeleteModeTrash  = "trash"
	DeleteModeDelete = "delete"
)

// resolveDeleteMode normalizes mode to a valid DeleteMode* constant,
// defaulting to DeleteModeTrash for "" or any unrecognized value so a
// missing/legacy preference never turns into a permanent deletion.
func resolveDeleteMode(mode string) string {
	if mode == DeleteModeDelete {
		return DeleteModeDelete
	}
	return DeleteModeTrash
}

// TrashNodes moves the comma-separated nodesIdList to trash. Backs the
// -trashNodes flag. A non-nil error means the caller should abort, the
// message has already been printed.
func TrashNodes(endpoint string, session *carbonio.Session, nodesIdList string) error {
	if nodesIdList == "" {
		log.Error().Msg("nodesIdList must be provided for trashNodes")
		return fmt.Errorf("nodesIdList must be provided for trashNodes")
	}
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: session.Token(), TokenRefresher: session.Reauthenticate}
	nodeIDs := strings.Split(nodesIdList, ",")
	trashResp, err := graphqlAuthenticator.TrashNodes(nodeIDs)
	if err != nil {
		log.Error().Err(err).Msg("Trashing nodes failed")
		return err
	}
	log.Info().Strs("trashedNodes", trashResp).Msg("Trashed nodes")
	return nil
}

// DeleteNodes permanently deletes the comma-separated nodesIdList. Backs the
// -deleteNodes flag. A non-nil error means the caller should abort, the
// message has already been printed.
func DeleteNodes(endpoint string, session *carbonio.Session, nodesIdList string) error {
	if nodesIdList == "" {
		log.Error().Msg("nodesIdList must be provided for deleteNodes")
		return fmt.Errorf("nodesIdList must be provided for deleteNodes")
	}
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: session.Token(), TokenRefresher: session.Reauthenticate}
	nodeIDs := strings.Split(nodesIdList, ",")
	deleteResp, err := graphqlAuthenticator.DeleteNodes(nodeIDs)
	if err != nil {
		log.Error().Err(err).Msg("Deleting nodes failed")
		return err
	}
	log.Info().Strs("deletedNodes", deleteResp).Msg("Deleted nodes")
	return nil
}

// LiveSyncCheck compares localFolder against the remote tree and prints the
// differences found. Backs the -liveSyncCheck flag. A non-nil error means
// the caller should abort, the message has already been printed.
func LiveSyncCheck(endpoint string, session *carbonio.Session, localFolder string, cacheSync bool) error {

	if cacheSync {
		log.Warn().Msg("Cache sync not yet implemented")
	}

	localMapItems, err := localfs.ReadFolderRecursive(localFolder, false)
	if err != nil {
		log.Error().Err(err).Msg("Reading local folder failed")
		return err
	}

	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: session.Token(), TokenRefresher: session.Reauthenticate}
	baseFolder := "LOCAL_ROOT"

	remoteMapItems, err := utils.RecursiveListNodeItems(graphqlAuthenticator, baseFolder, "")
	if err != nil {
		log.Error().Err(err).Msg("Fetching remote items failed")
		return err
	}

	// Diff against the same keys the download path will actually create on
	// disk (see LiveCacheSync/localfs.SanitizeRelativePath), otherwise a
	// remote name containing characters illegal on this OS would show up
	// as a permanent false "missing" diff even after a successful sync.
	sanitizedRemoteMapItems := make(map[string]localfs.ItemInfo, len(remoteMapItems))
	for remotePath, item := range remoteMapItems {
		sanitizedRemoteMapItems[localfs.SanitizeRelativePath(remotePath)] = item
	}

	diffs := localfs.ComparePathMapsMulti(localMapItems, sanitizedRemoteMapItems)
	for itemPath, diffList := range diffs {
		for _, diff := range diffList {
			evt := log.Info().Str("path", itemPath).Str("difference", string(diff.Diff))
			if diff.Local != nil {
				evt = evt.Interface("local", *diff.Local)
			}
			if diff.Remote != nil {
				evt = evt.Interface("remote", *diff.Remote)
			}
			evt.Msg("Sync difference detected")
		}
	}

	return nil
}

// UpdateCacheSync initializes (or refreshes) the sqlite sync cache with the
// current local/remote state. Backs the -updateCacheSync flag. A non-nil
// error means the caller should abort, the message has already been
// printed. The outcome (timestamp + error, if any) is persisted to the
// cache's sync_meta table via recordCacheSyncRun so the GUI dashboard can
// display "last sync" time and surface the last error without re-running
// the scan.
func UpdateCacheSync(endpoint string, session *carbonio.Session, localFolder string) error {
	err := updateCacheSync(endpoint, session, localFolder)
	recordCacheSyncRun(err)
	return err
}

// recordCacheSyncRun best-effort persists the outcome of the most recent
// UpdateCacheSync run to the sqlite cache's sync_meta table. Failures here
// are only logged: they must never override the original runErr returned
// to UpdateCacheSync's caller.
func recordCacheSyncRun(runErr error) {
	newdb, err := sqlitecache.NewSqliteHelper(appdir.Path("file_sync_cache.db"))
	if err != nil {
		log.Error().Err(err).Msg("Opening sqlite cache failed while recording sync run result")
		return
	}
	defer newdb.Close()

	errMsg := ""
	if runErr != nil {
		errMsg = runErr.Error()
	}
	if err := newdb.SetSyncRunResult(time.Now().Format(time.RFC3339), errMsg); err != nil {
		log.Warn().Err(err).Msg("Persisting sync run result failed")
	}
}

// updateCacheSync is UpdateCacheSync's implementation, wrapped so every
// return path - including the early ones below - gets its outcome recorded
// by recordCacheSyncRun without threading that call through each one.
func updateCacheSync(endpoint string, session *carbonio.Session, localFolder string) error {

	// Initialize SQLite database
	newdb, err := sqlitecache.NewSqliteHelper(appdir.Path("file_sync_cache.db"))
	if err != nil {
		log.Error().Err(err).Msg("Opening sqlite cache failed")
		return err
	}
	defer newdb.Close()
	log.Info().Msg("SQLite cache initialized successfully")

	localMapItems, err := localfs.ReadFolderRecursive(localFolder, false)
	if err != nil {
		log.Error().Err(err).Msg("Reading local folder failed")
		return err
	}
	log.Info().Int("count", len(localMapItems)).Msg("Found local items")

	// Fetch remote items from GraphQL
	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: session.Token(), TokenRefresher: session.Reauthenticate}
	baseFolder := "LOCAL_ROOT"
	remoteMapItems, err := utils.RecursiveListNodeItems(graphqlAuthenticator, baseFolder, "")
	if err != nil {
		log.Error().Err(err).Msg("Fetching remote items failed")
		return err
	}
	log.Info().Int("count", len(remoteMapItems)).Msg("Found remote items")

	// Check if the database is already populated
	count, countErr := newdb.CountRecords()
	if countErr != nil {
		log.Error().Err(countErr).Msg("Counting records failed")
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
			log.Error().Err(err).Msg("Querying existing records failed")
			return err
		}

		for _, rec := range allRecords {
			updateFields := make(map[string]interface{})

			if rec.LocalPath != "" {
				trackedPaths[rec.LocalPath] = struct{}{}
				if rec.LocalDeleted == 0 {
					if _, exists := localMapItems[rec.LocalPath]; !exists {
						// localMapItems is keyed by the exact byte string
						// ReadFolderRecursive produced for the current walk, so
						// a lookup miss here only proves the path isn't a
						// byte-exact match - not that it's actually gone from
						// disk. On case-insensitive, case-preserving
						// filesystems (Windows, default macOS) a path that
						// merely changed case - which the OS treats as a
						// no-op, not a deletion - produces exactly this kind
						// of miss. Confirm via a live stat, which resolves
						// the path the same way the OS does, before trusting
						// it: this flag drives an irreversible remote
						// trash/delete later in LiveCacheSync.
						if _, statErr := os.Lstat(filepath.Join(localFolder, filepath.FromSlash(rec.LocalPath))); statErr != nil {
							updateFields["local_deleted"] = 1
							log.Info().Str("path", rec.LocalPath).Msg("Local file deleted")
						}
					}
				}
			}

			if rec.RemotePath != "" {
				trackedPaths[rec.RemotePath] = struct{}{}
				if rec.RemoteDeleted == 0 {
					if _, exists := remoteMapItems[rec.RemotePath]; !exists {
						updateFields["remote_deleted"] = 1
						log.Info().Str("path", rec.RemotePath).Msg("Remote file deleted")
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
						log.Info().Str("path", rec.RemotePath).Msg("Remote node updated")
					}
					if remoteItem.CanWriteFile != rec.CanWriteFile || remoteItem.CanAddVersion != rec.CanAddVersion ||
						remoteItem.CanDelete != rec.CanDelete || remoteItem.MimeType != rec.MimeType {
						updateFields["can_write_file"] = remoteItem.CanWriteFile
						updateFields["can_add_version"] = remoteItem.CanAddVersion
						updateFields["can_delete"] = remoteItem.CanDelete
						updateFields["mime_type"] = remoteItem.MimeType
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
						log.Info().Str("path", rec.LocalPath).Msg("Local item updated")
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
					log.Warn().Err(updateErr).Int64("recordId", rec.ID).Msg("DB update failed")
				}
			}
		}
	} else {
		// DB is empty: reset auto-increment counter and do a full fresh initialization.
		if err = newdb.DeleteAllAndResetAutoIncrement(); err != nil {
			log.Error().Err(err).Msg("Clearing cache failed")
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
		canWriteFile := false
		canAddVersion := false
		canDelete := false
		mimeType := ""

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
			canWriteFile = remoteItem.CanWriteFile
			canAddVersion = remoteItem.CanAddVersion
			canDelete = remoteItem.CanDelete
			mimeType = remoteItem.MimeType
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
			canWriteFile, canAddVersion, canDelete, mimeType,
		)
		if insertErr != nil {
			log.Error().Err(insertErr).Str("path", itemPath).Msg("Inserting record failed")
		} else {
			insertCount++
		}
	}

	if count > 0 {
		log.Info().Int("inserted", insertCount).Msg("Cache sync updated: deletions detected, new items inserted")
	} else {
		log.Info().Int("inserted", insertCount).Msg("Cache sync initialized")
	}

	return nil
}

// SyncChange describes one document a LiveCacheSync/FullCacheSync run
// pulled from remote to local - the unit the GUI's desktop notification
// lists, one line per change (see App.notifySyncSummary).
type SyncChange struct {
	// Path is the document's path relative to the sync folder (the same
	// on both sides once synced - see the per-record "remote_path"/
	// "local_path" bookkeeping below).
	Path string
	// IsDirectory distinguishes "folder" from "file" in the notification
	// text (e.g. "Created a new folder" vs "Created a new file").
	IsDirectory bool
}

// SyncSummary collects every document a LiveCacheSync/FullCacheSync run
// changed by pulling it from remote to local - the only direction the
// GUI's desktop notification tracks (see notifySyncSummary): New covers
// remote_only items downloaded to local, Modified covers out_of_sync
// files where the remote version was newer and got downloaded
// (directories have no content to modify, so they never appear here),
// and Deleted covers local items removed because their remote
// counterpart was deleted. The opposite direction - local_only items
// uploaded to remote, out_of_sync files where the local version won, and
// remote items removed because their local counterpart was deleted - are
// real sync actions too, but never appear here: the user already knows
// about edits they just made locally, so those don't need a desktop
// notification. Housekeeping-only updates (e.g. flipping an out_of_sync
// record back to synced without touching any file because its content
// already matched) are not included either - they produced no visible
// change for the user.
type SyncSummary struct {
	New      []SyncChange
	Modified []SyncChange
	Deleted  []SyncChange
}

// HasChanges reports whether the summary contains any counted change,
// i.e. whether a desktop notification is warranted.
func (s SyncSummary) HasChanges() bool {
	return len(s.New) > 0 || len(s.Modified) > 0 || len(s.Deleted) > 0
}

// findExistingRemoteChild looks up parentNodeID's children on the remote
// server for one already matching name (folders) or name+size+digest
// (files, when a digest is available on both sides). It backs the
// local_only upload loop's ambiguous-failure handling below:
// CreateFolder/UploadFile can return a client-side error (e.g. a timeout)
// even after the mutation already succeeded server-side, and neither call
// is safe to blindly retry - a second call would create a duplicate
// remote node with the same name, since Carbonio identifies nodes by ID,
// not by uniqueness of name within a parent. Returns (nil, nil) when
// nothing matches, which the caller must treat as a genuine failure to
// retry next cycle rather than a success.
func findExistingRemoteChild(graphqlAuthenticator *graphql.GraphQLAuthenticator, parentNodeID, name string, isDirectory bool, size int64, digest string) (*graphql.Node, error) {
	children, err := graphqlAuthenticator.GetAllNode(parentNodeID, "NAME_ASC", nil, nil)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		if child == nil || child.Name != name {
			continue
		}
		if isDirectory {
			if child.Type == "FOLDER" {
				return child, nil
			}
			continue
		}
		if child.Type != "FILE" || child.Size == nil || int64(*child.Size) != size {
			continue
		}
		if digest != "" && (child.Digest == nil || *child.Digest != digest) {
			continue
		}
		return child, nil
	}
	return nil, nil
}

// LiveCacheSync reconciles localFolder and the remote tree using the sqlite
// sync cache: it downloads remote-only items, uploads local-only items,
// resolves out-of-sync items by timestamp, and propagates deletions in both
// directions. Backs the -liveCacheSync flag. deleteRemoteNode selects how
// a local deletion is propagated to the remote node - DeleteModeTrash (or
// "", the default) moves it to trash, DeleteModeDelete permanently
// removes it; any other value falls back to DeleteModeTrash (see
// resolveDeleteMode). A non-nil error means the caller should abort, the
// message has already been printed.
func LiveCacheSync(endpoint string, session *carbonio.Session, localFolder string, carbonioAuth *carbonio.HTTPAuthenticator, deleteRemoteNode string) (SyncSummary, error) {
	summary := SyncSummary{}

	// Open the existing SQLite cache database
	cacheDb, err := sqlitecache.NewSqliteHelper(appdir.Path("file_sync_cache.db"))
	if err != nil {
		log.Error().Err(err).Msg("Opening cache failed")
		return SyncSummary{}, err
	}
	defer cacheDb.Close()

	graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: endpoint, AuthToken: session.Token(), TokenRefresher: session.Reauthenticate}

	// Build a path → node_id map from every record that already has a remote presence
	allRecords, err := cacheDb.QueryAll()
	if err != nil {
		log.Error().Err(err).Msg("Querying cache failed")
		return SyncSummary{}, err
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
		log.Error().Err(err).Msg("Querying remote_only failed")
		return SyncSummary{}, err
	}
	log.Info().Int("count", len(remoteOnly)).Msg("Found remote_only items to download")

	// Process shallowest paths first so parent directories are created before children
	sort.Slice(remoteOnly, func(i, j int) bool {
		return strings.Count(remoteOnly[i].RemotePath, "/") < strings.Count(remoteOnly[j].RemotePath, "/")
	})

	for _, rec := range remoteOnly {
		if rec.RemoteDeleted != 0 {
			continue
		}
		if rec.IsDirectory {
			localRelPath := localfs.SanitizeRelativePath(rec.RemotePath)
			localDirPath := filepath.Join(localFolder, filepath.FromSlash(localRelPath))
			if err := os.MkdirAll(localDirPath, 0755); err != nil {
				log.Error().Err(err).Str("path", localDirPath).Msg("Creating local dir failed")
				continue
			}
			log.Info().Str("path", localDirPath).Msg("Created local dir")
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"local_path":      localRelPath,
				"local_path_hash": localfs.PathHash(localRelPath),
				"sync_status":     "synced",
				"last_synced":     now,
			}); updateErr != nil {
				log.Warn().Err(updateErr).Str("path", rec.RemotePath).Msg("DB update failed")
			}
			summary.New = append(summary.New, SyncChange{Path: rec.RemotePath, IsDirectory: true})
		} else {
			localRelPath := localfs.SanitizeRelativePath(rec.RemotePath)
			dirPart := path.Dir(localRelPath)
			fileName := path.Base(localRelPath)
			destPath := localFolder
			if dirPart != "." {
				destPath = filepath.Join(localFolder, filepath.FromSlash(dirPart))
			}
			if err := os.MkdirAll(destPath, 0755); err != nil {
				log.Error().Err(err).Str("path", destPath).Msg("Creating local dir failed")
				continue
			}
			var wg sync.WaitGroup
			sem := make(chan struct{}, 1)
			wg.Add(1)
			sem <- struct{}{}
			exitStat, downErr := carbonioAuth.DownloadFile(session.Token(), rec.NodeID, destPath, fileName, rec.RemoteSize, maxRetries, &wg, sem)
			wg.Wait()
			if downErr != nil {
				log.Error().Err(downErr).Str("path", rec.RemotePath).Msg("Downloading failed")
				continue
			} else if exitStat != nil {
				log.Info().Str("path", rec.RemotePath).Str("status", *exitStat).Msg("Download status")
			}
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"local_path":      localRelPath,
				"local_path_hash": localfs.PathHash(localRelPath),
				"local_size":      rec.RemoteSize,
				"local_digest":    rec.RemoteDigest,
				"sync_status":     "synced",
				"last_synced":     now,
			}); updateErr != nil {
				log.Warn().Err(updateErr).Str("path", rec.RemotePath).Msg("DB update failed")
			}
			summary.New = append(summary.New, SyncChange{Path: rec.RemotePath, IsDirectory: false})
		}
	}

	// --- Upload local_only items to remote ---
	localOnly, err := cacheDb.QueryBySyncStatus("local_only")
	if err != nil {
		log.Error().Err(err).Msg("Querying local_only failed")
		return SyncSummary{}, err
	}
	log.Info().Int("count", len(localOnly)).Msg("Found local_only items to upload")

	// Process shallowest paths first so parent folders are created on remote before their children
	sort.Slice(localOnly, func(i, j int) bool {
		return strings.Count(localOnly[i].LocalPath, "/") < strings.Count(localOnly[j].LocalPath, "/")
	})

	for _, rec := range localOnly {
		if rec.LocalDeleted != 0 {
			continue
		}
		parentPath := path.Dir(rec.LocalPath)
		log.Debug().Str("path", rec.LocalPath).Str("parentPath", parentPath).Msg("Processing local_only item")
		parentNodeID := "LOCAL_ROOT"
		if parentPath != "." {
			if id, ok := pathToNodeID[parentPath]; ok {
				parentNodeID = id
				log.Debug().Str("parentPath", parentPath).Str("parentNodeId", parentNodeID).Msg("Found parent node ID in cache")
			} else {
				// Fall back to a direct DB lookup for an existing folder at that path
				parentRec, dbErr := cacheDb.QueryFolderByPath(parentPath)
				if dbErr != nil {
					log.Error().Err(dbErr).Str("parentPath", parentPath).Msg("Querying parent folder failed")
				}
				if parentRec != nil && parentRec.NodeID != "" {
					parentNodeID = parentRec.NodeID
					pathToNodeID[parentPath] = parentRec.NodeID
					log.Debug().Str("parentPath", parentPath).Str("parentNodeId", parentNodeID).Msg("Found parent node ID in DB")
					if parentRec.RemotePath != parentPath {
						log.Warn().Str("parentPath", parentPath).Str("cachedPath", parentRec.RemotePath).Msg("Remote path mismatch for parent folder")
					}
					if parentRec.LocalDeleted != 0 || parentRec.RemoteDeleted != 0 {
						log.Warn().Str("parentPath", parentPath).Str("path", rec.LocalPath).Msg("Parent folder marked deleted in cache, using LOCAL_ROOT as parent")
						parentNodeID = "LOCAL_ROOT"
					}
				} else {
					log.Warn().Str("parentPath", parentPath).Str("path", rec.LocalPath).Msg("Remote parent folder not found in cache, using LOCAL_ROOT")
				}
			}
		}
		if rec.IsDirectory {
			folderName := path.Base(rec.LocalPath)
			newFolder, err := graphqlAuthenticator.CreateFolder(parentNodeID, folderName)
			if err != nil {
				log.Error().Err(err).Str("path", rec.LocalPath).Msg("Creating remote folder failed")
				existing, lookupErr := findExistingRemoteChild(graphqlAuthenticator, parentNodeID, folderName, true, 0, "")
				if lookupErr != nil {
					log.Warn().Err(lookupErr).Str("path", rec.LocalPath).Msg("Existence check after failed folder creation failed")
					continue
				}
				if existing == nil {
					continue
				}
				log.Info().Str("path", rec.LocalPath).Str("nodeId", existing.ID).Msg("Remote folder already existed despite the create error; adopting it")
				newFolder = &graphql.Folder{ID: existing.ID, Name: existing.Name}
			}
			if newFolder != nil {
				pathToNodeID[rec.LocalPath] = newFolder.ID
				log.Info().Str("path", rec.LocalPath).Str("nodeId", newFolder.ID).Msg("Created remote folder")
				if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
					"node_id":          newFolder.ID,
					"remote_path":      rec.LocalPath,
					"remote_path_hash": localfs.PathHash(rec.LocalPath),
					"sync_status":      "synced",
					"last_synced":      now,
				}); updateErr != nil {
					log.Warn().Err(updateErr).Str("path", rec.LocalPath).Msg("DB update failed")
				}
			}
		} else {
			filePath := filepath.Join(localFolder, filepath.FromSlash(rec.LocalPath))
			uploadedNodeID, uploadErr := carbonioAuth.UploadFile(session.Token(), parentNodeID, filePath, false, false, nil)
			if uploadErr != nil {
				log.Error().Err(uploadErr).Str("path", rec.LocalPath).Msg("Uploading failed")
				existing, lookupErr := findExistingRemoteChild(graphqlAuthenticator, parentNodeID, path.Base(rec.LocalPath), false, rec.LocalSize, rec.LocalDigest)
				if lookupErr != nil {
					log.Warn().Err(lookupErr).Str("path", rec.LocalPath).Msg("Existence check after failed upload failed")
					continue
				}
				if existing == nil {
					continue
				}
				log.Info().Str("path", rec.LocalPath).Str("nodeId", existing.ID).Msg("Remote file already existed despite the upload error; adopting it")
				uploadedNodeID = existing.ID
			}
			log.Info().Str("path", rec.LocalPath).Str("nodeId", uploadedNodeID).Msg("Uploaded")
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
				log.Warn().Err(updateErr).Str("path", rec.LocalPath).Msg("DB update failed")
			}
		}
	}

	// --- Resolve out_of_sync items ---
	outOfSync, err := cacheDb.QueryBySyncStatus("out_of_sync")
	if err != nil {
		log.Error().Err(err).Msg("Querying out_of_sync failed")
		return SyncSummary{}, err
	}
	log.Info().Int("count", len(outOfSync)).Msg("Found out_of_sync items to process")

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
				log.Warn().Err(updateErr).Str("path", rec.RemotePath).Msg("DB update failed")
			}
			continue
		}

		// Parse timestamps so we can decide which side is more recent.
		var remoteTs, localTs int64
		if rec.RemoteLastModified != "" {
			var parseErr error
			remoteTs, parseErr = strconv.ParseInt(rec.RemoteLastModified, 10, 64)
			if parseErr != nil {
				log.Warn().Err(parseErr).Str("path", rec.RemotePath).Msg("Could not parse remote_last_modified")
			}
		}
		if rec.LocalLastModified != "" {
			var parseErr error
			localTs, parseErr = strconv.ParseInt(rec.LocalLastModified, 10, 64)
			if parseErr != nil {
				log.Warn().Err(parseErr).Str("path", rec.LocalPath).Msg("Could not parse local_last_modified")
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
				log.Warn().Err(updateErr).Str("path", rec.RemotePath).Msg("DB update failed")
			}
			continue
		}

		// convert remoteTs and localTs to time.Time for better readability in logs
		remoteTime := utils.EpochToTime(remoteTs)
		localTime := utils.EpochToTime(localTs)

		if remoteTime.Equal(localTime) || remoteTime.After(localTime) {
			// Remote is more recent (or timestamps are equal): download the remote version.
			log.Info().Str("path", rec.RemotePath).Msg("Remote version is more recent; downloading update")
			localRelPath := localfs.SanitizeRelativePath(rec.RemotePath)
			dirPart := path.Dir(localRelPath)
			fileName := path.Base(localRelPath)
			destPath := localFolder
			if dirPart != "." {
				destPath = filepath.Join(localFolder, filepath.FromSlash(dirPart))
			}
			if err := os.MkdirAll(destPath, 0755); err != nil {
				log.Error().Err(err).Str("path", destPath).Msg("Creating local dir failed")
				continue
			}
			var wg sync.WaitGroup
			sem := make(chan struct{}, 1)
			wg.Add(1)
			sem <- struct{}{}
			exitStat, downErr := carbonioAuth.DownloadFile(session.Token(), rec.NodeID, destPath, fileName, rec.RemoteSize, maxRetries, &wg, sem)
			wg.Wait()
			if downErr != nil {
				log.Error().Err(downErr).Str("path", rec.RemotePath).Msg("Downloading updated file failed")
				continue
			} else if exitStat != nil {
				log.Info().Str("path", rec.RemotePath).Str("status", *exitStat).Msg("Download status")
			}
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"local_path":          localRelPath,
				"local_path_hash":     localfs.PathHash(localRelPath),
				"local_size":          rec.RemoteSize,
				"local_digest":        rec.RemoteDigest,
				"local_last_modified": rec.RemoteLastModified,
				"sync_status":         "synced",
				"last_synced":         now,
			}); updateErr != nil {
				log.Warn().Err(updateErr).Str("path", rec.RemotePath).Msg("DB update failed")
			}
			summary.Modified = append(summary.Modified, SyncChange{Path: rec.RemotePath, IsDirectory: false})
		} else {
			// Local is more recent: upload a new version to remote.
			log.Info().Str("path", rec.LocalPath).Msg("Local version is more recent; uploading update")
			parentPath := path.Dir(rec.LocalPath)
			parentNodeID := "LOCAL_ROOT"
			if parentPath != "." {
				if id, ok := pathToNodeID[parentPath]; ok {
					parentNodeID = id
				} else {
					parentRec, dbErr := cacheDb.QueryFolderByPath(parentPath)
					if dbErr != nil {
						log.Error().Err(dbErr).Str("parentPath", parentPath).Msg("Querying parent folder failed")
					}
					if parentRec != nil && parentRec.NodeID != "" {
						parentNodeID = parentRec.NodeID
						pathToNodeID[parentPath] = parentRec.NodeID
					}
				}
			}
			filePath := filepath.Join(localFolder, filepath.FromSlash(rec.LocalPath))
			nodeIDStr := rec.NodeID
			uploadedNodeID, uploadErr := carbonioAuth.UploadFile(session.Token(), parentNodeID, filePath, true, false, &nodeIDStr)
			if uploadErr != nil {
				log.Error().Err(uploadErr).Str("path", rec.LocalPath).Msg("Uploading new version failed")
				continue
			}
			log.Info().Str("path", rec.LocalPath).Str("nodeId", uploadedNodeID).Msg("Uploaded new version")
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"remote_size":          rec.LocalSize,
				"remote_digest":        rec.LocalDigest,
				"remote_last_modified": rec.LocalLastModified,
				"sync_status":          "synced",
				"last_synced":          now,
			}); updateErr != nil {
				log.Warn().Err(updateErr).Str("path", rec.LocalPath).Msg("DB update failed")
			}
		}
	}

	// --- Delete local items whose remote counterpart has been deleted ---
	remoteDeleted, err := cacheDb.QueryRemoteDeleted()
	if err != nil {
		log.Error().Err(err).Msg("Querying remote deleted failed")
		return SyncSummary{}, err
	}
	log.Info().Int("count", len(remoteDeleted)).Msg("Found remote deleted items to clean up locally")

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
				log.Error().Err(removeErr).Str("path", localItemPath).Msg("Removing local item failed")
				continue
			}
			// File already absent locally – still update the DB record.
		} else {
			log.Info().Str("path", localItemPath).Msg("Deleted local item (remote was deleted)")
		}
		if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
			"local_deleted": 1,
			"sync_status":   "remote_deleted",
			"last_synced":   now,
		}); updateErr != nil {
			log.Warn().Err(updateErr).Str("path", rec.LocalPath).Msg("DB update failed")
		}
		summary.Deleted = append(summary.Deleted, SyncChange{Path: rec.LocalPath, IsDirectory: rec.IsDirectory})
	}

	// --- Trash or permanently delete remote items whose local counterpart
	// has been deleted, per resolveDeleteMode(deleteRemoteNode) ---
	resolvedDeleteMode := resolveDeleteMode(deleteRemoteNode)
	localDeleted, err := cacheDb.QueryLocalDeleted()
	if err != nil {
		log.Error().Err(err).Msg("Querying local deleted failed")
		return SyncSummary{}, err
	}
	log.Info().Int("count", len(localDeleted)).Str("deleteRemoteNode", resolvedDeleteMode).Msg("Found locally deleted items to remove from remote")

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
		// Re-verify right before an irreversible remote action. local_deleted
		// was set by an earlier updateCacheSync scan (possibly a while ago:
		// this loop runs last in LiveCacheSync, after every download/upload/
		// out-of-sync step above has already spent time on the network), and
		// - see updateCacheSync's matching os.Lstat fallback - the flag can
		// also be wrong outright for a path that only ever appeared to
		// disappear because of a byte-exact map-key mismatch (e.g. a
		// case-only rename on Windows/macOS' case-insensitive, case-
		// preserving filesystems). os.Lstat resolves the path the same way
		// the OS itself does, so it isn't fooled by that mismatch. Acting on
		// a stale/incorrect flag here trashes or permanently deletes a
		// remote node whose local counterpart is actually still present, so
		// this check is mandatory, not optional hardening.
		if _, statErr := os.Lstat(filepath.Join(localFolder, filepath.FromSlash(rec.LocalPath))); statErr == nil {
			log.Warn().Str("path", rec.LocalPath).Msg("Local item still present; clearing stale local_deleted flag instead of removing remote item")
			if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
				"local_deleted": 0,
				"last_synced":   now,
			}); updateErr != nil {
				log.Warn().Err(updateErr).Str("path", rec.RemotePath).Msg("DB update failed")
			}
			continue
		}
		var opErr error
		if resolvedDeleteMode == DeleteModeDelete {
			_, opErr = graphqlAuthenticator.DeleteNodes([]string{rec.NodeID})
		} else {
			_, opErr = graphqlAuthenticator.TrashNodes([]string{rec.NodeID})
		}
		if opErr != nil {
			log.Error().Err(opErr).Str("path", rec.RemotePath).Str("nodeId", rec.NodeID).Str("deleteRemoteNode", resolvedDeleteMode).Msg("Removing remote item failed")
			continue
		}
		log.Info().Str("path", rec.RemotePath).Str("deleteRemoteNode", resolvedDeleteMode).Msg("Removed remote item (local was deleted)")
		if updateErr := cacheDb.UpdateFileSync("id", rec.ID, map[string]interface{}{
			"remote_deleted": 1,
			"sync_status":    "local_deleted",
			"last_synced":    now,
		}); updateErr != nil {
			log.Warn().Err(updateErr).Str("path", rec.RemotePath).Msg("DB update failed")
		}
	}

	log.Info().Msg("liveCacheSync completed")
	return summary, nil
}

// FullCacheSync runs UpdateCacheSync followed by LiveCacheSync against
// localFolder: it first (re)builds the sqlite sync cache from the current
// local/remote state, then performs the bidirectional sync using that
// freshly updated cache. Backs the -fullCacheSync flag. deleteRemoteNode
// is forwarded to LiveCacheSync verbatim - see its doc comment. A non-nil
// error means the caller should abort, the message has already been
// printed.
func FullCacheSync(endpoint string, session *carbonio.Session, localFolder string, carbonioAuth *carbonio.HTTPAuthenticator, deleteRemoteNode string) (SyncSummary, error) {
	if err := UpdateCacheSync(endpoint, session, localFolder); err != nil {
		return SyncSummary{}, err
	}

	summary, err := LiveCacheSync(endpoint, session, localFolder, carbonioAuth, deleteRemoteNode)
	if err != nil {
		return SyncSummary{}, err
	}

	log.Info().Msg("fullCacheSync completed")
	return summary, nil
}

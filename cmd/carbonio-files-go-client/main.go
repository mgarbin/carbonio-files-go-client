package main

import (
	"carbonio-files-go-client/pkg/carbonio"
	"carbonio-files-go-client/pkg/graphql"
	"carbonio-files-go-client/pkg/localfs"
	sqlitecache "carbonio-files-go-client/pkg/sqlite"
	"flag"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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

func recursiveListNodeItems(graphqlAuthenticator *graphql.GraphQLAuthenticator, id string, folderPath string) (map[string]localfs.ItemInfo, error) {

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
			newFolderPath := ""
			if folderPath == "" {
				newFolderPath = child.Name
			} else {
				newFolderPath = folderPath + "/" + child.Name
			}
			newNodeItems, err := recursiveListNodeItems(graphqlAuthenticator, child.ID, newFolderPath)
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
				fmt.Printf("%s.%s (%s) - DIGEST [%s] \n", child.Name, *child.Extension, child.Type, *child.Digest)
			} else {
				fmt.Printf("%s (%s) - DIGEST [%s]\n", child.Name, child.Type, *child.Digest)
			}
		}
	}
}

func createLocalFolder(path string) error {
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
			err := createLocalFolder(folderPath)
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
	createFolder := flag.String("createFolder", "", "Use this flag to create a folder (specify Name) then specify a parentId where to create it")
	printFlagInfo := flag.Bool("v", false, "output helper with all flags")
	uploadFile := flag.String("uploadFile", "", "Use this flag to upload a specific file into files, specify also parentId")
	uploadNewVersionFile := flag.String("uploadNewVersionFile", "", "Use this flag to upload a specific file into files, specify also nodeId and parentId")
	overwriteVersion := flag.Bool("overwriteVersion", false, "Use this flag to overwrite a file during the uploadNewVersionFile")
	nodeId := flag.String("nodeId", "", "Use this flag to specify NodeId")
	parentId := flag.String("parentId", "", "Use this flag to specify ParentId")
	liveSyncCheck := flag.Bool("liveSyncCheck", false, "Use this flag to check differences between local folder and remote folder")
	cacheSync := flag.Bool("cacheSync", false, "Use this flag to enable sqlite cache for liveSyncCheck")
	initCacheSync := flag.Bool("initCacheSync", false, "Use this flag to initialize sqlite cache for liveSyncCheck")
	liveCacheSync := flag.Bool("liveCacheSync", false, "Use this flag to sync local and remote files using the sqlite cache db (downloads remote_only items, uploads local_only items)")

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

	if *uploadFile != "" && *parentId != "" {
		//parentId := "e5174e4d-7b01-4510-a56b-30075e84cd8f"
		newNodeID, uploadErr := carbonio.UploadFile(*zmAuthToken, *parentId, *uploadFile, false, false, nodeId)
		if uploadErr != nil {
			fmt.Println("[ERROR]:", uploadErr)
		} else {
			fmt.Println("[INFO] Uploaded file, nodeId:", newNodeID)
		}
	}

	if *uploadNewVersionFile != "" && *nodeId != "" && *parentId != "" {
		//base_folder := "e5174e4d-7b01-4510-a56b-30075e84cd8f"
		newNodeID, uploadErr := carbonio.UploadFile(*zmAuthToken, *parentId, *uploadNewVersionFile, true, *overwriteVersion, nodeId)
		if uploadErr != nil {
			fmt.Println("[ERROR]:", uploadErr)
		} else {
			fmt.Println("[INFO] Uploaded new version, nodeId:", newNodeID)
		}
	}

	if *createFolder != "" && *parentId != "" {
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		newFolder, err := graphqlAuthenticator.CreateFolder(*parentId, *createFolder)
		if err != nil {
			fmt.Println("[ERROR]: ", err)
		} else {
			fmt.Println("[INFO] New folder id ", newFolder.ID)
		}
	}

	if *liveSyncCheck {

		if *cacheSync {
			fmt.Println("Cache sync not yet implemented")
		}

		localFolder := "./files"

		localMapItems, err := localfs.ReadFolderRecursive(localFolder)
		if err != nil {
			fmt.Println("Error:", err)
			return
		}

		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		base_folder := "LOCAL_ROOT"

		remoteMapItems, err := recursiveListNodeItems(graphqlAuthenticator, base_folder, "")
		if err != nil {
			fmt.Println("Error:", err)
			return
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

	}

	if *initCacheSync {

		// Initialize SQLite database
		newdb, err := sqlitecache.NewSqliteHelper("./file_sync_cache.db")
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		defer newdb.Close()
		fmt.Println("Sqlite cache initialized successfully:", newdb.DB)

		// Clear existing data and reset auto-increment
		err = newdb.DeleteAllAndResetAutoIncrement()
		if err != nil {
			fmt.Println("Error clearing cache:", err)
			return
		}

		// Read local filesystem items
		localFolder := "./files"
		localMapItems, err := localfs.ReadFolderRecursive(localFolder)
		if err != nil {
			fmt.Println("Error reading local folder:", err)
			return
		}
		fmt.Printf("Found %d local items\n", len(localMapItems))

		// Fetch remote items from GraphQL
		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}
		baseFolder := "LOCAL_ROOT"
		remoteMapItems, err := recursiveListNodeItems(graphqlAuthenticator, baseFolder, "")
		if err != nil {
			fmt.Println("Error fetching remote items:", err)
			return
		}
		fmt.Printf("Found %d remote items\n", len(remoteMapItems))

		// Build union of all paths from local and remote
		allPaths := make(map[string]struct{})
		for path := range localMapItems {
			allPaths[path] = struct{}{}
		}
		for path := range remoteMapItems {
			allPaths[path] = struct{}{}
		}

		// Insert each item into SQLite
		now := time.Now().Format(time.RFC3339)
		insertCount := 0
		for path := range allPaths {
			localItem, hasLocal := localMapItems[path]
			remoteItem, hasRemote := remoteMapItems[path]

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
			deleted := false

			if hasRemote {
				remotePath = path
				remotePathHash = localfs.PathHash(path)
				nodeID = remoteItem.NodeId
				isDirectory = !remoteItem.IsFile
				remoteLastModified = strconv.FormatInt(remoteItem.ModifyTimestamp, 10)
				remoteSize = int64(remoteItem.Size)
				remoteDigest = remoteItem.Digest
				deleted = remoteItem.DeleteTimestamp != 0
			}

			if hasLocal {
				localPath = path
				localPathHash = localfs.PathHash(path)
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
				deleted,
			)
			if insertErr != nil {
				fmt.Printf("Error inserting %s: %v\n", path, insertErr)
			} else {
				insertCount++
			}
		}

		fmt.Printf("Cache sync initialized with %d items\n", insertCount)
	}

	if *liveCacheSync {

		// Open the existing SQLite cache database
		cacheDb, err := sqlitecache.NewSqliteHelper("./file_sync_cache.db")
		if err != nil {
			fmt.Println("Error opening cache:", err)
			return
		}
		defer cacheDb.Close()

		graphqlAuthenticator := &graphql.GraphQLAuthenticator{Endpoint: cfg.Main.Endpoint, AuthToken: *zmAuthToken}

		// Build a path → node_id map from every record that already has a remote presence
		allRecords, err := cacheDb.QueryAll()
		if err != nil {
			fmt.Println("Error querying cache:", err)
			return
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
			return
		}
		fmt.Printf("[INFO] Found %d remote_only items to download\n", len(remoteOnly))

		// Process shallowest paths first so parent directories are created before children
		sort.Slice(remoteOnly, func(i, j int) bool {
			return strings.Count(remoteOnly[i].RemotePath, "/") < strings.Count(remoteOnly[j].RemotePath, "/")
		})

		for _, rec := range remoteOnly {
			if rec.Deleted {
				continue
			}
			if rec.IsDirectory {
				localDirPath := filepath.Join("./files", filepath.FromSlash(rec.RemotePath))
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
				destPath := "./files"
				if dirPart != "." {
					destPath = filepath.Join("./files", filepath.FromSlash(dirPart))
				}
				if err := os.MkdirAll(destPath, 0755); err != nil {
					fmt.Printf("[ERROR] creating local dir %s: %v\n", destPath, err)
					continue
				}
				var wg sync.WaitGroup
				sem := make(chan struct{}, 1)
				wg.Add(1)
				sem <- struct{}{}
				exitStat, downErr := carbonio.DownloadFile(*zmAuthToken, rec.NodeID, destPath, fileName, rec.RemoteSize, maxRetries, &wg, sem)
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
			return
		}
		fmt.Printf("[INFO] Found %d local_only items to upload\n", len(localOnly))

		// Process shallowest paths first so parent folders are created on remote before their children
		sort.Slice(localOnly, func(i, j int) bool {
			return strings.Count(localOnly[i].LocalPath, "/") < strings.Count(localOnly[j].LocalPath, "/")
		})

		for _, rec := range localOnly {
			if rec.Deleted {
				continue
			}
			parentPath := path.Dir(rec.LocalPath)
			parentNodeID := "LOCAL_ROOT"
			if parentPath != "." {
				if id, ok := pathToNodeID[parentPath]; ok {
					parentNodeID = id
				} else {
					fmt.Printf("[WARN] remote parent folder %s not found in cache, using LOCAL_ROOT for %s\n", parentPath, rec.LocalPath)
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
				filePath := filepath.Join("./files", filepath.FromSlash(rec.LocalPath))
				uploadedNodeID, uploadErr := carbonio.UploadFile(*zmAuthToken, parentNodeID, filePath, false, false, nil)
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

		fmt.Println("[INFO] liveCacheSync completed.")
	}

}

package sync

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirectorySync 目录同步器
type DirectorySync struct {
	storage    CloudStorage
	config     *Config
	localPath  string // 本地data目录路径
	remotePath string // 远程路径前缀
}

// NewDirectorySync 创建目录同步器
func NewDirectorySync(storage CloudStorage, config *Config, localPath string) *DirectorySync {
	// 根据本地路径确定远程路径
	var remotePath string
	if strings.Contains(localPath, "favicon") {
		// favicon目录的文件直接存储在远程根目录
		remotePath = config.ConfigPath
	} else {
		// data目录的文件存储在远程根目录
		remotePath = config.ConfigPath
	}

	return &DirectorySync{
		storage:    storage,
		config:     config,
		localPath:  localPath,
		remotePath: remotePath,
	}
}

// SyncDirectory 同步整个目录
func (ds *DirectorySync) SyncDirectory(ctx context.Context) error {
	log.Printf("[DirectorySync] Starting directory sync: %s", ds.localPath)

	// 1. 扫描本地文件
	localFiles, err := ds.scanLocalFiles()
	if err != nil {
		return fmt.Errorf("failed to scan local files: %w", err)
	}

	// 2. 扫描远程文件
	remoteFiles, err := ds.scanRemoteFiles(ctx)
	if err != nil {
		log.Printf("[DirectorySync] Failed to scan remote files (will upload all): %v", err)
		remoteFiles = make(map[string]FileInfo)
	}

	// 3. 比较并同步文件
	uploadCount := 0
	downloadCount := 0

	for relativePath, localFile := range localFiles {
		remoteFile, exists := remoteFiles[relativePath]

		if !exists {
			if err := ds.uploadFile(ctx, relativePath, localFile); err != nil {
				log.Printf("[DirectorySync] Failed to upload %s (new file): %v", relativePath, err)
			} else {
				log.Printf("[DirectorySync] Uploaded new file: %s", relativePath)
				uploadCount++
			}
		} else if ds.shouldUpload(localFile, remoteFile) {
			if err := ds.uploadFile(ctx, relativePath, localFile); err != nil {
				log.Printf("[DirectorySync] Failed to upload %s (updated): %v", relativePath, err)
			} else {
				log.Printf("[DirectorySync] Uploaded updated file: %s (local: %v, remote: %v)",
					relativePath, localFile.ModTime.UTC().Format(time.RFC3339), remoteFile.ModTime.UTC().Format(time.RFC3339))
				uploadCount++
			}
		}
	}

	// 4. 下载远程有但本地没有的文件
	for relativePath, remoteFile := range remoteFiles {
		if _, exists := localFiles[relativePath]; !exists {
			if err := ds.downloadFile(ctx, relativePath, remoteFile); err != nil {
				log.Printf("[DirectorySync] Failed to download %s: %v", relativePath, err)
			} else {
				downloadCount++
			}
		}
	}

	log.Printf("[DirectorySync] Directory sync completed: uploaded %d, downloaded %d files", uploadCount, downloadCount)
	return nil
}

// FileInfo 文件信息
type FileInfo struct {
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	ModTime      time.Time `json:"mod_time"`
	Hash         string    `json:"hash,omitempty"`
	RelativePath string    `json:"relative_path"`
}

// scanLocalFiles 扫描本地文件
func (ds *DirectorySync) scanLocalFiles() (map[string]FileInfo, error) {
	files := make(map[string]FileInfo)

	err := filepath.Walk(ds.localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		// 同步JSON文件和favicon目录下的文件
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".json" && ext != ".ico" && ext != ".png" && ext != ".svg" {
			return nil
		}

		// 检查是否在需要同步的目录中
		if !ds.shouldSyncFile(path) {
			return nil
		}

		// 计算相对路径
		var relativePath string
		if strings.Contains(ds.localPath, "favicon") {
			// favicon目录：保留完整路径（包含favicon目录）
			relativePath = strings.TrimPrefix(path, "./")
			relativePath = filepath.ToSlash(relativePath)
		} else {
			// data目录：计算相对路径
			var err error
			relativePath, err = filepath.Rel(ds.localPath, path)
			if err != nil {
				return err
			}
			relativePath = filepath.ToSlash(relativePath)
		}

		files[relativePath] = FileInfo{
			Path:         path,
			Size:         info.Size(),
			ModTime:      info.ModTime(),
			RelativePath: relativePath,
		}

		return nil
	})

	return files, err
}

// shouldSyncFile 检查文件是否需要同步
func (ds *DirectorySync) shouldSyncFile(filePath string) bool {
	// 计算相对路径
	relativePath, err := filepath.Rel(ds.localPath, filePath)
	if err != nil {
		return false
	}

	// 标准化路径分隔符
	relativePath = filepath.ToSlash(relativePath)

	// 获取文件扩展名
	ext := strings.ToLower(filepath.Ext(relativePath))

	// 只同步有特定后缀的文件，避免同步无后缀的缓存项
	allowedExtensions := []string{".json", ".ico", ".png", ".svg"}
	hasAllowedExtension := false
	for _, allowedExt := range allowedExtensions {
		if ext == allowedExt {
			hasAllowedExtension = true
			break
		}
	}

	if !hasAllowedExtension {
		return false
	}

	// 根据扫描的目录确定同步模式
	if strings.Contains(ds.localPath, "favicon") {
		// favicon目录：只同步图标文件
		allowedFaviconFiles := []string{"favicon.ico"}
		for _, allowedFile := range allowedFaviconFiles {
			if relativePath == allowedFile {
				return true
			}
		}
	} else {
		// data目录：同步配置和统计文件
		syncDirPatterns := []string{
			"config.json",   // 主配置文件
			"cache/",        // 缓存配置目录（只会同步.json文件）
			"mirror_cache/", // 镜像缓存配置目录（只会同步.json文件）
			"metrics/",      // 统计数据目录（只会同步.json文件）
		}

		// 检查是否在允许的目录或文件中
		for _, pattern := range syncDirPatterns {
			if relativePath == strings.TrimSuffix(pattern, "/") || // 精确匹配文件
				strings.HasPrefix(relativePath, pattern) { // 匹配目录下的文件
				return true
			}
		}
	}

	return false
}

// scanRemoteFiles 扫描远程文件
func (ds *DirectorySync) scanRemoteFiles(ctx context.Context) (map[string]FileInfo, error) {
	files := make(map[string]FileInfo)

	// 使用S3的ListObjectsV2来列出所有文件
	if s3Client, ok := ds.storage.(*S3Client); ok {
		return ds.scanRemoteFilesS3(ctx, s3Client)
	}

	return files, nil
}

// scanRemoteFilesS3 使用S3 API扫描远程文件
func (ds *DirectorySync) scanRemoteFilesS3(ctx context.Context, s3Client *S3Client) (map[string]FileInfo, error) {
	files := make(map[string]FileInfo)

	// 使用ListObjectsV2动态列出远程文件
	var prefix string
	if strings.Contains(ds.localPath, "favicon") {
		prefix = ds.remotePath + "/favicon"
	} else {
		prefix = ds.remotePath
	}

	remoteFiles, err := s3Client.ListObjects(ctx, prefix)
	if err != nil {
		log.Printf("[DirectorySync] Failed to list remote objects: %v", err)
		return files, nil // 返回空列表，而不是错误
	}

	// 过滤和转换文件列表
	for _, file := range remoteFiles {
		// 跳过目录标记文件（以/结尾）
		if strings.HasSuffix(file.RelativePath, "/") {
			continue
		}

		// 应用文件过滤逻辑
		if ds.shouldSyncRemoteFile(file.RelativePath) {
			files[file.RelativePath] = file
		}
	}

	return files, nil
}

// shouldSyncRemoteFile 检查远程文件是否需要同步
func (ds *DirectorySync) shouldSyncRemoteFile(relativePath string) bool {
	// 获取文件扩展名
	ext := strings.ToLower(filepath.Ext(relativePath))

	// 只同步有特定后缀的文件，避免同步无后缀的缓存项
	allowedExtensions := []string{".json", ".ico", ".png", ".svg"}
	hasAllowedExtension := false
	for _, allowedExt := range allowedExtensions {
		if ext == allowedExt {
			hasAllowedExtension = true
			break
		}
	}

	if !hasAllowedExtension {
		return false
	}

	// 根据扫描的目录确定同步模式
	if strings.Contains(ds.localPath, "favicon") {
		// favicon目录：只同步图标文件（现在包含完整路径）
		allowedFaviconFiles := []string{"favicon/favicon.ico"}
		for _, allowedFile := range allowedFaviconFiles {
			if relativePath == allowedFile {
				return true
			}
		}
		return false
	} else {
		// data目录：同步配置和统计文件
		syncDirPatterns := []string{
			"config.json",   // 主配置文件
			"cache/",        // 缓存配置目录（只会同步.json文件）
			"mirror_cache/", // 镜像缓存配置目录（只会同步.json文件）
			"metrics/",      // 统计数据目录（只会同步.json文件）
		}

		// 检查是否在允许的目录或文件中
		for _, pattern := range syncDirPatterns {
			if relativePath == strings.TrimSuffix(pattern, "/") || // 精确匹配文件
				strings.HasPrefix(relativePath, pattern) { // 匹配目录下的文件
				return true
			}
		}
	}

	return false
}

// shouldUpload 判断是否应该上传文件
func (ds *DirectorySync) shouldUpload(local, remote FileInfo) bool {
	// 如果远程文件不存在，上传
	if remote.RelativePath == "" {
		return true
	}

	// 统一转换为UTC时间并截断到秒级精度
	localTime := local.ModTime.UTC().Truncate(time.Second)
	remoteTime := remote.ModTime.UTC().Truncate(time.Second)

	// 如果本地文件更新，上传
	return localTime.After(remoteTime)
}

// uploadFile 上传文件
func (ds *DirectorySync) uploadFile(ctx context.Context, relativePath string, fileInfo FileInfo) error {
	data, err := os.ReadFile(fileInfo.Path)
	if err != nil {
		return fmt.Errorf("failed to read local file: %w", err)
	}

	// 构建远程路径（现在relativePath已经包含完整路径）
	remotePath := ds.remotePath + "/" + relativePath

	if err := ds.storage.Upload(ctx, remotePath, data); err != nil {
		return fmt.Errorf("failed to upload to remote: %w", err)
	}

	log.Printf("[DirectorySync] Uploaded: %s", relativePath)
	return nil
}

// downloadFile 下载文件
func (ds *DirectorySync) downloadFile(ctx context.Context, relativePath string, fileInfo FileInfo) error {
	// 构建远程路径（现在relativePath已经包含完整路径）
	remotePath := ds.remotePath + "/" + relativePath

	data, err := ds.storage.Download(ctx, remotePath)
	if err != nil {
		return fmt.Errorf("failed to download from remote: %w", err)
	}

	localPath := filepath.Join(ds.localPath, relativePath)

	// 确保目录存在
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(localPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write local file: %w", err)
	}

	log.Printf("[DirectorySync] Downloaded: %s", relativePath)
	return nil
}

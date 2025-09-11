package sync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Client S3客户端实现
type S3Client struct {
	client *s3.Client
	config *Config
}

// NewS3Client 创建新的S3客户端
func NewS3Client(cfg *Config) (*S3Client, error) {
	// 创建AWS配置
	awsCfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		)),
		config.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// 创建S3客户端选项
	options := func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	}

	client := s3.NewFromConfig(awsCfg, options)

	return &S3Client{
		client: client,
		config: cfg,
	}, nil
}

// Upload 上传数据到S3
func (c *S3Client) Upload(ctx context.Context, key string, data []byte) error {
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
		// 不再需要自定义metadata，直接使用S3的LastModified时间
	})

	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// Download 从S3下载数据
func (c *S3Client) Download(ctx context.Context, key string) ([]byte, error) {
	result, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}

	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return data, nil
}

// GetVersion 获取文件版本信息
func (c *S3Client) GetVersion(ctx context.Context, key string) (string, time.Time, error) {
	result, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.config.Bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to get object metadata: %w", err)
	}

	timestamp := time.Time{}
	if result.LastModified != nil {
		timestamp = *result.LastModified
	}

	// 版本号直接使用LastModified的Unix时间戳
	version := fmt.Sprintf("%d", timestamp.Unix())

	return version, timestamp, nil
}

// ListVersions 列出对象版本
func (c *S3Client) ListVersions(ctx context.Context, key string) ([]VersionInfo, error) {
	result, err := c.client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
		Bucket: aws.String(c.config.Bucket),
		Prefix: aws.String(key),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list object versions: %w", err)
	}

	var versions []VersionInfo

	for _, version := range result.Versions {
		if version.Key != nil && *version.Key == key {
			info := VersionInfo{
				Version:  aws.ToString(version.VersionId),
				ETag:     strings.Trim(aws.ToString(version.ETag), "\""),
				Size:     aws.ToInt64(version.Size),
				IsLatest: aws.ToBool(version.IsLatest),
			}

			if version.LastModified != nil {
				info.Timestamp = *version.LastModified
			}

			versions = append(versions, info)
		}
	}

	return versions, nil
}

// ListObjects 列出对象（使用ListObjectsV2）
func (c *S3Client) ListObjects(ctx context.Context, prefix string) ([]FileInfo, error) {
	var allFiles []FileInfo
	
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(c.config.Bucket),
		Prefix: aws.String(prefix),
	}
	
	// 处理分页
	for {
		result, err := c.client.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}
		
		// 转换结果
		for _, obj := range result.Contents {
			if obj.Key != nil {
				fileInfo := FileInfo{
					RelativePath: strings.TrimPrefix(*obj.Key, prefix+"/"),
					Size:         aws.ToInt64(obj.Size),
				}
				
				if obj.LastModified != nil {
					fileInfo.ModTime = *obj.LastModified
				}
				
				allFiles = append(allFiles, fileInfo)
			}
		}
		
		// 检查是否还有更多页
		if !aws.ToBool(result.IsTruncated) {
			break
		}
		
		// 设置下一页的令牌
		input.ContinuationToken = result.NextContinuationToken
	}
	
	return allFiles, nil
}

// TestConnection 测试S3连接
func (c *S3Client) TestConnection(ctx context.Context) error {
	_, err := c.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(c.config.Bucket),
	})

	if err != nil {
		return fmt.Errorf("failed to connect to S3 bucket: %w", err)
	}

	return nil
}


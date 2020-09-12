package certmagic_s3

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bsm/redislock"
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/certmagic"
	"github.com/go-redis/redis/v7"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
	"io/ioutil"
	"strings"
	"time"
)

type S3 struct {
	Logger *zap.Logger

	// S3
	Client    *minio.Client
	Host      string `json:"host"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Prefix    string `json:"prefix"`

	RedisClient   *redis.Client
	RedisLocker   *redislock.Client
	RedisLocks    map[string]*redislock.Lock
	RedisAddress  string `json:"redis_address"`  // localhost:6379
	RedisPassword string `json:"redis_password"` // empty
	RedisDB       int    `json:"redis_db"`       // 0
}

func init() {
	caddy.RegisterModule(new(S3))
}

func (s3 *S3) Provision(context caddy.Context) error {
	s3.Logger = context.Logger(s3)

	// S3 Client
	client, _ := minio.New(s3.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(s3.AccessKey, s3.SecretKey, ""),
		Secure: true,
	})

	s3.Client = client

	// Redis Client
	s3.RedisClient = redis.NewClient(&redis.Options{
		Network:  "tcp",
		Addr:     s3.RedisAddress,
		Password: s3.RedisPassword,
		DB:       s3.RedisDB,
	})
	s3.RedisLocker = redislock.New(s3.RedisClient)
	s3.RedisLocks = make(map[string]*redislock.Lock)

	return nil
}

func (s3 *S3) Cleanup() error {
	if s3.Logger != nil {
		s3.Logger.Info("S3 Cleanup")
	}

	for _, lock := range s3.RedisLocks {
		s3.Logger.Info(fmt.Sprintf("Release Redis Lock: %v", lock))

		_ = lock.Release()
	}

	_ = s3.RedisClient.Close()

	return nil
}

func (s3 *S3) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID: "caddy.storage.s3",
		New: func() caddy.Module {
			return new(S3)
		},
	}
}

func (s3 *S3) CertMagicStorage() (certmagic.Storage, error) {
	return s3, nil
}

func (s3 *S3) Lock(_ context.Context, key string) error {
	s3.Logger.Info(fmt.Sprintf("Lock: %v", key))

	lock, err := s3.RedisLocker.Obtain(key, time.Minute, nil)

	s3.RedisLocks[key] = lock

	if err == redislock.ErrNotObtained {
		s3.Logger.Error(fmt.Sprintf("Cannot lock key: %v", key))
	} else if err != nil {
		s3.Logger.Error(fmt.Sprintf("Lock error: %v", err))
	}

	return err
}

func (s3 *S3) Unlock(key string) error {
	if lock, exists := s3.RedisLocks[key]; exists {
		s3.Logger.Info(fmt.Sprintf("Release lock: %v", key))

		err := lock.Release()

		delete(s3.RedisLocks, key)

		if err != nil {
			return err
		}
	}

	return nil
}

func (s3 *S3) Store(key string, value []byte) error {
	key = s3.KeyPrefix(key)

	s3.Logger.Info(fmt.Sprintf("Store: %v, %v bytes", key, len(value)))

	_, err := s3.Client.PutObject(context.Background(), s3.Bucket, key, bytes.NewReader(value), int64(len(value)), minio.PutObjectOptions{})

	return err
}

func (s3 *S3) Load(key string) ([]byte, error) {
	key = s3.KeyPrefix(key)

	s3.Logger.Info(fmt.Sprintf("Load: %v", key))

	object, err := s3.Client.GetObject(context.Background(), s3.Bucket, key, minio.GetObjectOptions{})

	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(object)

	return content, err
}

func (s3 *S3) Delete(key string) error {
	key = s3.KeyPrefix(key)

	s3.Logger.Info(fmt.Sprintf("Delete: %v", key))

	err := s3.Client.RemoveObject(context.Background(), s3.Bucket, key, minio.RemoveObjectOptions{})

	return err
}

func (s3 *S3) Exists(key string) bool {
	key = s3.KeyPrefix(key)

	s3.Logger.Info(fmt.Sprintf("Exists: %v", key))

	_, err := s3.Client.StatObject(context.Background(), s3.Bucket, key, minio.StatObjectOptions{})

	return err == nil
}

func (s3 *S3) List(prefix string, recursive bool) ([]string, error) {
	prefix = s3.KeyPrefix(prefix)

	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	var keys []string

	objects := s3.Client.ListObjects(context.Background(), s3.Bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: recursive,
	})

	for object := range objects {
		if !strings.HasSuffix(object.Key, "/") {
			keys = append(keys, object.Key)
		}
	}

	return keys, nil
}

func (s3 *S3) Stat(key string) (certmagic.KeyInfo, error) {
	key = s3.KeyPrefix(key)

	s3.Logger.Info(fmt.Sprintf("Stat: %v", key))

	object, err := s3.Client.StatObject(context.Background(), s3.Bucket, key, minio.StatObjectOptions{})

	if err != nil {
		return certmagic.KeyInfo{}, nil
	}

	return certmagic.KeyInfo{
		Key:        object.Key,
		Modified:   object.LastModified,
		Size:       object.Size,
		IsTerminal: strings.HasSuffix(object.Key, "/"),
	}, err
}

func (s3 S3) KeyPrefix(prefix string) string {
	if strings.HasPrefix(prefix, s3.Prefix) {
		return prefix
	} else {
		return strings.Join([]string{s3.Prefix, prefix}, "/")
	}
}

var _ caddy.Provisioner = (*S3)(nil)

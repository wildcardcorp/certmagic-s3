package certmagic_s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/certmagic"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
)

type FASMSLockerClient struct {
	endpoint string
	apiKey   string
}

type FASMSLocker struct {
	client       *FASMSLockerClient
	resourceName string
	uuid         string
}

type FASMSObtainMutexResponse struct {
	Obtained bool   `json:"obtained"`
	UUID     string `json:"uuid"`
}

type FASMSReleaseMutexResponse struct {
	Released bool `json:"released"`
}

func (client *FASMSLockerClient) baseMutexUrl() string {
	return client.endpoint + "/api/v1/mutex?api_key=" + url.QueryEscape(client.apiKey)
}

func (locker *FASMSLocker) resourceUrl() string {
	return locker.client.baseMutexUrl() + "&resource_name=" + url.QueryEscape(locker.resourceName)
}

func (locker *FASMSLocker) obtainMutexUrl(ttl time.Duration) string {
	return locker.resourceUrl() + "&ttl=" + url.QueryEscape(strconv.Itoa(int(ttl.Seconds())))
}

func (locker *FASMSLocker) releaseMutexUrl() string {
	return locker.resourceUrl() + "&uuid=" + url.QueryEscape(locker.uuid)
}

func (locker *FASMSLocker) Lock(ctx context.Context, ttl time.Duration) error {
	lockerErrChan := make(chan error)
	go func() {
		// keep trying to obtain the lock
		for {
			resp, err := locker.lock(ttl)
			if err != nil {
				lockerErrChan <- err
				return
			}
			if resp.Obtained == true {
				locker.uuid = resp.UUID
				lockerErrChan <- nil
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
	for {
		// either obtain the lock or context expires
		select {
		case lockErr := <-lockerErrChan:
			return lockErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (locker *FASMSLocker) lock(ttl time.Duration) (*FASMSObtainMutexResponse, error) {
	if resp, err := http.Get(locker.obtainMutexUrl(ttl)); err == nil {
		if resp.StatusCode == 200 {
			if body, err := ioutil.ReadAll(resp.Body); err == nil {
				var jsonResp FASMSObtainMutexResponse
				if err := json.Unmarshal(body, &jsonResp); err == nil {
					return &jsonResp, nil
				} else {
					return nil, err
				}
			} else {
				return nil, err
			}
		} else {
			err := errors.New("FASMSLocker.Lock: got status code " + strconv.Itoa(resp.StatusCode) + ", but expected 200 on endpoint " + locker.client.endpoint)
			return nil, err
		}
	} else {
		return nil, err
	}
}

func (locker *FASMSLocker) Unlock(ctx context.Context) error {
	unlockerErrChan := make(chan error)
	go func() {
		resp, err := locker.unlock()
		if err != nil {
			unlockerErrChan <- err
			return
		}
		if resp.Released == true {
			unlockerErrChan <- nil
			return
		} else {
			unlockerErrChan <- errors.New("FASMSLocker.Unlock: could not unlock resource '" + locker.resourceName + "' with uuid '" + locker.uuid + "'")
			return
		}
	}()
	for {
		// either release the lock or context expires
		select {
		case lockErr := <-unlockerErrChan:
			return lockErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (locker *FASMSLocker) unlock() (*FASMSReleaseMutexResponse, error) {
	client := http.Client{}
	if req, err := http.NewRequest(http.MethodDelete, locker.releaseMutexUrl(), nil); err == nil {
		if resp, err := client.Do(req); err == nil {
			if resp.StatusCode == 200 {
				if body, err := ioutil.ReadAll(resp.Body); err == nil {
					var jsonResp FASMSReleaseMutexResponse
					if err := json.Unmarshal(body, &jsonResp); err == nil {
						return &jsonResp, nil
					} else {
						return nil, err
					}
				} else {
					return nil, err
				}
			} else {
				err := errors.New("FASMSLocker.Unlock: got status code " + strconv.Itoa(resp.StatusCode) + ", but expected 200 on endpoint " + locker.client.endpoint)
				return nil, err
			}
		} else {
			return nil, err
		}
	} else {
		return nil, err
	}
}

type S3 struct {
	Logger *zap.Logger

	// S3
	Client    *minio.Client
	Host      string `json:"host"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Prefix    string `json:"prefix"`

	// FASMS
	FASMSClient   *FASMSLockerClient
	FASMSLocks    map[string]*FASMSLocker
	FASMSEndpoint string `json:"fasms_endpoint"`
	FASMSApiKey   string `json:"fasms_api_key"`
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

	// FASMS Client
	s3.FASMSClient = &FASMSLockerClient{endpoint: s3.FASMSEndpoint, apiKey: s3.FASMSApiKey}
	s3.FASMSLocks = make(map[string]*FASMSLocker)

	return nil
}

func (s3 *S3) Cleanup() error {
	if s3.Logger != nil {
		s3.Logger.Info("S3 Cleanup")
	}

	for _, lock := range s3.FASMSLocks {
		s3.Logger.Info(fmt.Sprintf("Release FASMS Lock: %v", lock.resourceName))

		_ = lock.Unlock(context.Background())
	}

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

func (s3 *S3) Lock(ctx context.Context, key string) error {
	s3.Logger.Info(fmt.Sprintf("Lock: %v", key))

	lock := &FASMSLocker{client: s3.FASMSClient, resourceName: key}
	err := lock.Lock(ctx, time.Minute)

	s3.FASMSLocks[key] = lock

	if err != nil {
		s3.Logger.Error(fmt.Sprintf("Lock error: %v", err))
	}

	return err
}

func (s3 *S3) Unlock(key string) error {
	if lock, exists := s3.FASMSLocks[key]; exists {
		s3.Logger.Info(fmt.Sprintf("Release lock: %v", key))

		err := lock.Unlock(context.Background())

		delete(s3.FASMSLocks, key)

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

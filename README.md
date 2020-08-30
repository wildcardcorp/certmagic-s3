## Certmagic-S3

Certmagic S3-compatible driver written in Go, using single node Redis as the lock.

### Guide

Run

    xcaddy run --config caddy.json
    
Config example

    {
      "apps": {
        "tls": {
          "automation": {
            "policies": [
              {
                "storage": {
                  "module": "s3",
                  "host": "Host",
                  "bucket": "Bucket",
                  "access_key": "AccessKey",
                  "secret_key": "SecretKey",
                  "prefix": "ssl",
                  "redis_address": "127.0.0.1:6379"，
                  "redis_password": "",
                  "redis_db": 0
                }
              }
            ]
          }
        }
      }
    }

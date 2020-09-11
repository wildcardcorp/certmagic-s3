## Certmagic-S3

Certmagic S3-compatible driver written in Go, using single node Redis as the lock.

Test passed on:

 - Vultr Objects
 - DigitalOcean Spaces

### Guide
    
Build

    go get -u github.com/caddyserver/xcaddy/cmd/xcaddy
    
    xcaddy build --output ./caddy --with github.com/ss098/certmagic-s3

Build container

    FROM caddy:builder AS builder
    RUN caddy-builder github.com/ss098/certmagic-s3
    
    FROM caddy
    COPY --from=builder /usr/bin/caddy /usr/bin/caddy

Run

    caddy run --config caddy.json

Config example

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
      "app": {
        ...
      }
    }

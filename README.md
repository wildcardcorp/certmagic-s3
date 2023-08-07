## Certmagic-S3

Certmagic S3-compatible driver written in Go, using [FastAPI Simple Mutex Server](https://github.com/wildcardcorp/fastapi-simple-mutex-server) as the lock.

Test passed on:

 - Vultr Objects
 - DigitalOcean Spaces

### Guide
    
Build

    go get -u github.com/caddyserver/xcaddy/cmd/xcaddy
    
    xcaddy build --output ./caddy --with github.com/daxxog/certmagic-s3

Build container

    FROM caddy:builder AS builder
    RUN caddy-builder github.com/daxxog/certmagic-s3
    
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
        "fasms_endpoint": "https://my-fastapi-simple-mutex-server.example.com",
        "fasms_api_key": "APIKey"
      }
      "app": {
        ...
      }
    }

#!/bin/bash

GOARCH=arm GOOS=linux GOARM=7 CGO_ENABLED=0 go build -v \
-ldflags="-w -s" -trimpath \
-o pana-init ./cmd/pana-init
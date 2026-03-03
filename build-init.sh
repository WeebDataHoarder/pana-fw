#!/bin/bash

GOARCH=arm GOOS=linux GOARM=7 CGO_ENABLED=0 go build -v -o pana-init ./cmd/pana-init
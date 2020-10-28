#!/bin/bash
#
# Copyright (c) 2020 The Decred developers
# Use of this source code is governed by an ISC
# license that can be found in the LICENSE file.
#
# The script does automatic checking on a Go package and its sub-packages, including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. golint        (https://github.com/golang/lint)
# 3. go vet        (http://golang.org/cmd/vet)
# 4. gosimple      (https://github.com/dominikh/go-simple)
# 5. unconvert     (https://github.com/mdempsky/unconvert)
# 6. race detector (http://blog.golang.org/race-detector)
# 7. test coverage (http://blog.golang.org/cover)

set -ex

# run `go mod download` and `go mod tidy` and fail if the git status of
# go.mod and/or go.sum changes
MOD_STATUS=$(git status --porcelain go.mod go.sum)
go mod download
go mod tidy
UPDATED_MOD_STATUS=$(git status --porcelain go.mod go.sum)
if [ "$UPDATED_MOD_STATUS" != "$MOD_STATUS" ]; then
    echo "Running `go mod tidy` modified go.mod and/or go.sum"
    exit 1
fi

# run tests
env GORACE="halt_on_error=1" go test -race ./...

# golangci-lint (github.com/golangci/golangci-lint) is used to run each each
# static checker.

# set output format for linter
if [[ -v CI ]]; then
    OUT_FORMAT="github-actions"
else
    OUT_FORMAT="colored-line-number"
fi

# check linters
golangci-lint run --disable-all --deadline=10m \
  --out-format=$OUT_FORMAT \
  --enable=gofmt \
  --enable=golint \
  --enable=vet \
  --enable=gosimple \
  --enable=unconvert \
  --enable=ineffassign

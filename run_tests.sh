#!/bin/bash
#
# Copyright (c) 2020-2021 The Decred developers
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

go version

# run tests
env GORACE="halt_on_error=1" go test -race ./...

# golangci-lint (github.com/golangci/golangci-lint) is used to run each each
# static checker.

# check linters
golangci-lint run

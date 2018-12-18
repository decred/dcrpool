#!/usr/bin/env bash
set -ex

# The script does automatic checking on a Go package and its sub-packages,
# including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. go vet        (http://golang.org/cmd/vet)
# 3. gosimple      (https://github.com/dominikh/go-simple)
# 4. unconvert     (https://github.com/mdempsky/unconvert)
# 5. ineffassign   (https://github.com/gordonklaus/ineffassign)
# 6. race detector (http://blog.golang.org/race-detector)

# gometalinter (github.com/alecthomas/gometalinter) is used to run each each
# static checker.

# To run on docker on windows, symlink /mnt/c to /c and then execute the script
# from the repo path under /c.  See:
# https://github.com/Microsoft/BashOnWindows/issues/1854
# for more details.

testrepo () {

  # TODO: rewrite this for go.sum
  # Check lockfile
  # TMPFILE=$(mktemp)
  # cp Gopkg.lock $TMPFILE && dep ensure && diff Gopkg.lock $TMPFILE >/dev/null
  # if [ $? != 0 ]; then
  #   echo 'lockfile must be updated with dep ensure'
  #   exit 1
  # fi

  go clean -modcache
  # Test application install
  # go install ./cmd/...

  # Check linters
  # (currently disabled)
  # --enable=unconvert \
  #  --enable=varcheck \
  #  --enable=structcheck \
  #  --enable=interfacer \
  #  --enable=megacheck \
  #  --enable=vetshadow \
  gometalinter \
    --disable-all --vendor --deadline=10m \
    --enable=gofmt \
    --enable=vet \
    --enable=ineffassign \
    --enable=golint \
    --enable=deadcode \
    --enable=goconst \
    ./...
  if [ $? != 0 ]; then
    echo 'gometalinter has some complaints'
    exit 1
  fi

  if [ $? != 0 ]; then
    echo 'go install failed'
    exit 1
  fi

  # Check tests
  env GORACE='halt_on_error=1' go test ./...
  if [ $? != 0 ]; then
    echo 'go tests failed'
    exit 1
  fi

  echo "------------------------------------------"
  echo "Tests completed successfully!"
}

testrepo

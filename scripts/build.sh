#!/bin/bash

set -o nounset
set -o errexit
set -o pipefail

. scripts/fmt.sh
. scripts/lint.sh

echo "./node_modules/.bin/gulp default"
./node_modules/.bin/gulp default

go build -o quicknotes
#go build -race

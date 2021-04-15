#!/bin/sh
if [ "$IS_CONTAINER" != "" ]; then
  go vet "${@}"
else
  docker run --rm \
    --env GO111MODULE="$GO111MODULE" \
    --env GOFLAGS="$GOFLAGS" \
    --env GOPROXY="$GOPROXY" \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/go/src/github.com/openshift/cluster-api-provider-equinix-metal:z" \
    --workdir /go/src/github.com/openshift/cluster-api-provider-equinix-metal \
    openshift/origin-release:golang-1.15 \
    ./hack/go-vet.sh "${@}"
fi;

# OpenShift cluster-api-provider-equinix-metal

This repository hosts an implementation of a provider for Equinix Metal for the
OpenShift [machine-api](https://github.com/openshift/cluster-api).

This provider runs as a machine-controller deployed by the
[machine-api-operator](https://github.com/openshift/machine-api-operator)

Basic Testing:
```sh
export EQUINIX_METAL_API_KEY=`<YOUR API KEY>`
export EQUINIX_METAL_PROJECT_ID=`<YOUR PROJECT ID>`
export SSH_KEY_CONTENTS=$(cat ~/.ssh/id_rsa.pub)

# Create the ignition config
cat << EOF > example.fcc
variant: fcos
version: 1.1.0
passwd:
  users:
    - name: core
      ssh_authorized_keys:
        - ${SSH_KEY_CONTENTS}
EOF
docker run -i --rm quay.io/coreos/fcct:release --pretty --strict < example.fcc > example.ign

# Create the kind cluster for testing
kind create cluster

# Deploy the CRDs/RBAC
kustomize build config | kubectl apply -f -

# Create the referenced credentials secret
kubectl create secret generic equinixmetal-credentials-secret --from-literal=api_key=${EQUINIX_METAL_API_KEY}

# Create the referenced user data secret
kubectl create secret generic worker-user-data-secret --from-file=userData=./example.ign

# Create the example machine resource
envsubst < example/worker-machine.yaml | kubectl create -f -

# Build and run the controller
go build -o bin/manager ./cmd/manager
./bin/manager --logtostderr -v 5 -alsologtostderr
```

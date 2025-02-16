#!/usr/bin/env bash
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
source "${script_dir}/shlib/prelude"
set -x # Use helper scripts (not functions) to keep set -x output meaningful

cluster=pod-tolerations-webhook-test-cluster
image=devnev/pod-tolerations-webhook:latest
context=kind-$cluster
selector=app.kubernetes.io/name=pod-tolerations-webhook
namespace=pod-tolerations

## Cluster setup

if ! kind get clusters | grep --quiet $cluster; then
  kind create cluster --name $cluster --config="${script_dir}/manifests/cluster.yaml"
fi

kubectl \
  --context $context \
  apply \
  --filename https://github.com/cert-manager/cert-manager/releases/download/v1.17.0/cert-manager.yaml

# Option `wait --for=create` unavailable in CI
# Even with `wait --for=create`, we can get `error: no matching resources found`
sleep 5
run_if_ci sleep 10
run_if_not_ci \
  kubectl \
  --context $context \
  wait \
  pod \
  --namespace cert-manager \
  --selector=app.kubernetes.io/instance=cert-manager \
  --for=create

kubectl \
  --context $context \
  wait \
  pod \
  --namespace cert-manager \
  --selector=app.kubernetes.io/instance=cert-manager \
  --for=condition=ready

## Service (re)deployment

docker build --quiet --tag $image .

kind load docker-image $image --name $cluster

kubectl \
  --context $context \
  apply \
  --kustomize "${script_dir}/deploy.kustomize"

# Make sure pod actually restarts
kubectl \
  --context $context \
  delete \
  --ignore-not-found=true \
  pod \
  --namespace $namespace \
  --selector=$selector

# Option `wait --for=create` unavailable in CI
# Even with `wait --for=create`, we can get `error: no matching resources found`
sleep 5
run_if_ci sleep 10
run_if_not_ci \
  kubectl \
  --context $context \
  wait \
  pod \
  --namespace $namespace \
  --selector=$selector \
  --for=create

kubectl \
  --context $context \
  wait \
  pod \
  --namespace $namespace \
  --selector=$selector \
  --for=condition=ready

## Check

kubectl \
  --context $context \
  delete \
  --ignore-not-found=true \
  namespace \
  test

kubectl \
  --context $context \
  apply \
  --kustomize "${script_dir}/test.kustomize"

expect_output \
  --expected '{"effect":"NoSchedule","key":"MyTaint","operator":"Equal","value":"Tainted"}' \
  kubectl \
  --context $context \
  get \
  --namespace test \
  pod \
  test-pod \
  --output 'jsonpath={.spec.tolerations[?(@.key=="MyTaint")]}'

log_success Success!

## Cleanup

kind delete cluster --name $cluster

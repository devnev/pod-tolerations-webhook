# Kubernetes mutating admission webhook for adding tolerations to pods

For example, mutate all pods in namespaces prefixed with `dev-` to tolerate
running on spot instance.

## Usage

```sh
mkdir add-toleration
cat >add-toleration/kustomization.yaml <<EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - https://github.com/cert-manager/cert-manager/releases/download/v1.17.0/cert-manager.yaml
  - https://github.com/devnev/pod-tolerations-webhook//kustomize/base?ref=main

patches:
  - patch: |-
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        namespace: pod-tolerations
        name: pod-tolerations-webhook
      spec:
        template:
          containers:
            - name: webhook
              args:
                  - --toleration=Equal:MyTaint:Tainted:NoSchedule
EOF
kubectl apply -k ./add-toleration
```

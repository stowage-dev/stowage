---
type: how-to
---

# Image pull secrets

If you mirror the Stowage images into a private registry that
requires authentication, attach the dockerconfigjson Secret to every
Pod via `image.pullSecrets`.

## Configure

```yaml
image:
  registry: my-registry.example.com/stowage
  tag: v1.0.0
  pullPolicy: IfNotPresent
  pullSecrets:
    - my-registry-creds
```

The chart **does not create the Secret**. Create it ahead of time:

```sh
kubectl -n stowage-system create secret docker-registry my-registry-creds \
  --docker-server=my-registry.example.com \
  --docker-username=robot \
  --docker-password=$REGISTRY_TOKEN
```

The chart attaches `imagePullSecrets:` to the Stowage and operator
Deployments using whatever names you list.

## Multiple secrets

```yaml
image:
  pullSecrets:
    - my-registry-creds
    - backup-registry-creds
```

Kubernetes tries each in order until one works.

## Check it's wired

```sh
kubectl -n stowage-system get deploy stowage -o jsonpath='{.spec.template.spec.imagePullSecrets}'
kubectl -n stowage-system get deploy stowage-operator -o jsonpath='{.spec.template.spec.imagePullSecrets}'
```

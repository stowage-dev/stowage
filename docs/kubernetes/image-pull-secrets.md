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

The chart attaches `imagePullSecrets:` to the Stowage Deployment using
whatever names you list. (There is no separate operator Pod — the
operator manager runs inside the stowage container when
`operator.enabled` is true.)

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
```

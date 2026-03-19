# Run `make build` to generate dist/artifacts.json and dist/metadata.json
# before running this Tiltfile.
artifacts = read_json('dist/artifacts.json')
metadata = read_json('dist/metadata.json')

goarch = metadata['runtime']['goarch']
for x in artifacts:
    if 'goarch' in x:
        if x['goarch'] == goarch:
            TARGETPLATFORM = "dist/build_{}".format(x['target'])
            break

docker_build(
    'ghcr.io/tinkerbell/cluster-api-provider-tinkerbell',
    '.',
    dockerfile='Dockerfile',
    build_args={
        'TARGETPLATFORM': TARGETPLATFORM,
    },
    only=[TARGETPLATFORM, 'Dockerfile'],
)
# kustomize build bypasses clusterctl, so we must substitute ${VAR:=default}
# placeholders that clusterctl would normally handle.
manifests = kustomize('./config/default')
manifests = blob(
    str(manifests)
    .replace("${TINKERBELL_IP:=''}", os.getenv('TINKERBELL_IP', ''))
    .replace("${REMOTE_TINKERBELL_KUBECONFIG:=remote-tinkerbell-kubeconfig}", os.getenv('REMOTE_TINKERBELL_KUBECONFIG', 'remote-tinkerbell-kubeconfig'))
    .replace("${REMOTE_TINKERBELL_WATCH_NAMESPACE:=''}", os.getenv('REMOTE_TINKERBELL_WATCH_NAMESPACE', ''))
)
k8s_yaml(manifests)

default_registry('ttl.sh/meohmy-dghentld')
allow_k8s_contexts('capt-playground-admin@capt-playground')

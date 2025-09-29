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
)
k8s_yaml(kustomize('./config/default'))
default_registry('ttl.sh/meohmy-dghentld')
allow_k8s_contexts('capt-playground-admin@capt-playground')

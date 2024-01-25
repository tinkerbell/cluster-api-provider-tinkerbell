# CAPT Playground

## Usage

The CAPT playground can be run as a standalone binary or via Docker.

### Standalone

```bash
capt-playground -h
```

### Docker

```bash
docker build -t capt-playground .
docker run -it --rm --network host -v /tmp:/tmp -v /var/run/docker.sock:/var/run/docker.sock -v /var/run/libvirt/libvirt-sock-ro:/var/run/libvirt/libvirt-sock-ro -v /var/run/libvirt/libvirt-sock:/var/run/libvirt/libvirt-sock --name capt-playground capt-playground
capt-playground -h
```

## Known Issues

### DNS issue

KinD on Ubuntu has a known issue with DNS resolution in KinD pod containers. This affect the Download of HookOS in the Tink stack helm deployment. There are a few [known workarounds](https://github.com/kubernetes-sigs/kind/issues/1594#issuecomment-629509450). The recommendation for the CAPT playground is to add DNS nameservers to Docker's `daemon.json` file. This can be done by adding the following to `/etc/docker/daemon.json`:

```json
{
  "dns": ["1.1.1.1"]
}
```

Then restart Docker:

```bash
sudo systemctl restart docker
```

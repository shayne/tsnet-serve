# tsnet-serve

[![CI](https://github.com/shayne/tsnet-serve/actions/workflows/ci.yml/badge.svg)](https://github.com/shayne/tsnet-serve/actions/workflows/ci.yml)

It's like `tailscale serve` but a standalone app that lives on your tailnet.

A tsnet app that runs a lightweight reverse proxy on your tailnet.
Give it a hostname, backend, and provide an auth key.
The app will appear on your tailnet as a machine.

Tailscale provides connectivity, a TLS cert, and
runs the same reverse proxy as `tailscale serve`.

Open a web browser and point it to `https://machine.your-tcd.ts.net`.

Expose your service to the internet via [Tailscale Funnel](https://tailscale.com/kb/1223/funnel/)
using the `-funnel` flag.

## Usage

To run `tsnet-serve`, you need to provide a hostname and a backend URL
to proxy requests to.
The hostname is a subdomain of your Tailscale Tailnet.
The backend is exposed on that hostname in your Tailnet.
Set the hostname using the `-hostname` flag or the `TSNS_HOSTNAME` environment variable.
Set the backend URL using the `-backend` flag or the `TSNS_BACKEND` environment variable.

You can listen on any port on your hostname by specifying the `-listen-port` flag
or by setting the `TSNS_LISTEN_PORT` environment variable.

When [Tailscale Funnel](https://tailscale.com/kb/1223/funnel/) is enabled using 
`-funnel` or the `TSNS_FUNNEL` environment variable, only ports 443, 8443 and 10000 are allowed.

You can restrict access to specific paths using the `-allowed-paths` and `-denied-paths` flags.
When using these flags, you can specify regular expressions to match the paths.

If allowed paths are specified, only those paths will be accessible.
Unmatched paths will return a 404 Not Found error.

If denied paths are specified, those paths will be blocked.
Matched paths will return a 403 Forbidden error.

You can specify a state directory using the `-state-dir` flag.
This directory is used to store the Tailscale state.

You can also specify a custom Tailscale control URL using the `-control-url` flag.
You can point this to your Headscale instance if you are using it.

```shell
tsnet-serve \
  -[hostname <hostname=tsnet-serve>] \
  -backend <backend> \
  [-listen-port <port=443>]
  [-funnel] \
  [-allowed-paths <regexp>] \
  [-denied-paths <regexp>] \
  [-state-dir <state-dir=./state>] \
  [-control-url <control-url>]
  [-version]
```

## Releases

[![Release](https://github.com/shayne/tsnet-serve/actions/workflows/release.yml/badge.svg)](https://github.com/shayne/tsnet-serve/actions/workflows/release.yml)

Pre-built binaries are available on the [releases page](https://github.com/shayne/tsnet-serve/releases).

Container images are available on [GitHub Container Registry](https://ghcr.io/shayne/tsnet-serve)
at `ghcr.io/shayne/tsnet-serve`.

Also available on [Homebrew](https://formulae.brew.sh/formula/tsnet-serve).

## Build and run

To build from source and run:

```shell
# Run a binary
go run . -hostname myapp -backend https://localhost:3000

# Run a container image
docker run $(ko build --local .)
```

## Container Images

OCI Container images are available on [GitHub Container Registry](https://ghcr.io/shayne/tsnet-serve).
You can run the container with Podman or Docker.

```shell
podman run -d \
    --name=tsnet-serve \
    -v /path/to/state:/state \
    -e TSNS_HOSTNAME=<hostname> \
    -e TSNS_BACKEND=<backend> \
    # optional, enables Tailscale Funnel
    # -e TSNS_FUNNEL=true \
    # optional, set to your control URL
    # leave empty for default
    # -e TS_CONTROL_URL=https://your.headscale.com \
    -e TS_AUTHKEY=<auth key> \
    ghcr.io/shayne/tsnet-serve:latest
```

Initial registration requires an [auth key](https://tailscale.com/kb/1085/auth-keys/)
set as the `TS_AUTHKEY` env var. If an auth key is not provided,
the app will print a log with a link to create one.

## Contributing

Contributions to this project are welcome.
Please feel free to open an issue or submit a pull request
if you have any improvements or bug fixes to suggest.

## License

Licensed under the [MIT License](LICENSE).

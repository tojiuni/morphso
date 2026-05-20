# morphso

AI service marketplace CLI — search, install, and manage AI-ready services with automatic strategy selection and dependency resolution.

> Default hub: **https://morphso.toji.homes**  
> To use your own hub: set `hub_url` in `~/.morphso/config.yaml`.

## Install

### One-liner (recommended)

```sh
curl -fsSL https://morphso.toji.homes/install | sh
```

Downloads a pre-built binary for your OS/arch (macOS/Linux, amd64/arm64). Falls back to `go install` if no binary is available, and installs Go automatically if needed. PATH is registered for bash/zsh/fish.

### Pre-built binary

Download the latest binary from [Releases](https://github.com/tojiuni/morphso/releases), extract, and place it in your PATH:

```sh
# Example for Linux amd64 — replace version and platform as needed
VERSION=$(curl -fsSL https://api.github.com/repos/tojiuni/morphso/releases/latest | grep '"tag_name"' | sed 's/.*"\(v[^"]*\)".*/\1/')
curl -fsSL "https://github.com/tojiuni/morphso/releases/download/${VERSION}/morphso_${VERSION}_linux_amd64.tar.gz" | tar -xz
sudo mv morphso /usr/local/bin/
```

### go install

```sh
go install github.com/tojiuni/morphso@latest
```

Requires Go 1.21+.

## Quick Start

```sh
morphso login                  # authenticate (ZITADEL Device Flow)
morphso search gopedia         # find packages
morphso info gopedia           # details + resource requirements
morphso install gopedia        # install — deps resolved automatically
morphso list                   # installation history
morphso remove gopedia         # uninstall
```

## Commands

| Command | Description |
|---------|-------------|
| `login` / `logout` | Authenticate via ZITADEL Device Flow |
| `whoami` | Show the current authenticated user |
| `search <query>` | Search packages |
| `info <package>` | Package details, dependencies, resource summary |
| `install <package[@version]>` | Install with AI-recommended strategy; resolves deps |
| `remove <package>` | Uninstall a package |
| `list` | Show installation history (hub-tracked) |

## Configuration

`~/.morphso/config.yaml` is created on first login:

```yaml
hub_url: https://morphso.toji.homes  # override with --hub-url flag
token:   <set automatically by login>
```

Override for a single command: `morphso search foo --hub-url http://localhost:18080`

## Docs

- [Commands reference](docs/commands.md)
- [Install strategies](docs/install-strategies.md)
- [Dependency pipeline](docs/dependencies.md)
- [Configuration](docs/configuration.md)

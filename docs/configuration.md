# Configuration

morphso reads `~/.morphso/config.yaml` at startup. The file is created automatically on first `morphso login`.

## Fields

| Field | Default | Description |
|-------|---------|-------------|
| `hub_url` | `https://morphso.toji.homes` | Base URL of the morphso-hub API |
| `token` | _(set by `login`)_ | Bearer token for authenticated requests; refreshed on re-login |

**Example:**

```yaml
hub_url: https://morphso.toji.homes
token: eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...
```

## Overrides

| Method | Scope | Example |
|--------|-------|---------|
| `--hub-url <url>` flag | Single command | `morphso search foo --hub-url http://localhost:18080` |
| `MOSO_CONFIG` env var | Install script execution | Set automatically when `--config <file>` is used |

## Self-Hosted Hub

To connect to your own morphso-hub instance, update `hub_url`:

```yaml
hub_url: http://localhost:18080
```

See the [morphso-hub repository](https://github.com/tojiuni/morphso-hub) for hub setup instructions.

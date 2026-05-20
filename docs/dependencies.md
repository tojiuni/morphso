# Dependency Pipeline

When you install a package, morphso resolves its full dependency tree before installing the main package. You are shown the full dependency list and summed resource requirements before anything runs.

## Required Dependencies

Required dependencies are installed automatically, in dependency order, using the same strategy selection flow as a top-level install.

```
gopedia depends on:
  postgresql  1.0+    RAM 1.0 GB  Disk 5.0 GB
  qdrant      1.7+    RAM 2.0 GB  Disk 3.0 GB
  typedb      3.10+   (no resource data)
  redis       7.0+    RAM 0.5 GB  Disk 1.0 GB

Total: RAM 3.5 GB  Disk 9.0 GB
Proceed? [y/N]
```

If any required dependency fails to install, the main package installation is aborted.

## Optional Dependencies

Optional dependencies are not required for the package to function but unlock additional capabilities. morphso prompts for each one interactively:

```
[optional] ollama — LLM backend for gopedia. Choose:
  [1] Install Ollama via Docker
  [2] Use existing Ollama URL
  [3] Enter API token (OpenAI / Anthropic / other)
  [4] Skip
Choice [1-4]:
```

| Choice | What happens |
|--------|-------------|
| `1` Install via Docker | Runs Ollama's install script; injects `OLLAMA_URL=http://localhost:11434` |
| `2` Existing URL | Prompts for URL; injects `OLLAMA_URL=<url>` |
| `3` API token | Prompts for token; injects it as the relevant env var |
| `4` Skip | Optional dep is skipped; its env vars are not set |

Injected env vars are passed to the main package's install script so it can connect to the configured backend.

## Skipping Dependency Resolution

Use `--no-deps` to install the main package only, without resolving or installing any dependencies:

```sh
morphso install gopedia --no-deps
```

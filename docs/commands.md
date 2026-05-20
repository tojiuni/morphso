# Commands Reference

## login

Authenticate using ZITADEL Device Flow. Opens a browser URL; token is saved to `~/.morphso/config.yaml`.

```sh
morphso login
```

No flags.

---

## logout

Clear the saved token from `~/.morphso/config.yaml`.

```sh
morphso logout
```

No flags.

---

## whoami

Print the currently authenticated user's ID and email.

```sh
morphso whoami
```

Returns an error if not logged in.

---

## search

Search packages by keyword. Returns up to 20 results.

```sh
morphso search <query>
```

**Example:**

```sh
morphso search vector database
```

Output columns: `NAME`, `TYPE`, `PRICE`, `DESCRIPTION`. Verified packages are marked `✓`.

---

## info

Show package details: description, type, tags, dependencies, and summed resource requirements (RAM + Disk).

```sh
morphso info <package>
```

**Example:**

```sh
morphso info gopedia
```

---

## install

Install a package. Resolves and installs required dependencies first, then prompts for optional ones. The hub recommends an install strategy based on your OS and architecture unless overridden.

```sh
morphso install <package[@version]> [flags]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--strategy <s>` | Force strategy: `native`, `docker`, `k8s`, `helm` |
| `--native` | Shorthand for `--strategy=native` |
| `--docker` | Shorthand for `--strategy=docker` |
| `--k8s` | Shorthand for `--strategy=k8s` |
| `--helm` | Shorthand for `--strategy=helm` |
| `--yes` | Non-interactive mode — skip confirmation prompts |
| `--no-deps` | Install main package only; skip dependency resolution |
| `--template` | Save config template to `./<slug>.env` |
| `--config <file>` | Inject a custom config file via `MOSO_CONFIG` env var |

**Examples:**

```sh
morphso install gopedia                  # AI-recommended strategy
morphso install gopedia --docker         # force Docker
morphso install gopedia@1.2.0 --yes      # specific version, non-interactive
morphso install gopedia --template       # also save config template
morphso install gopedia --no-deps        # skip dependency install
```

---

## remove

Uninstall a package and record the removal in hub history. Prompts for confirmation; blocked if other installed packages depend on this one.

```sh
morphso remove <package>
```

**Example:**

```sh
morphso remove gopedia
```

---

## list

Show installation history for the authenticated user, as recorded by the hub.

```sh
morphso list
```

Output columns: `PACKAGE`, `VERSION`, `STRATEGY`, `OS/ARCH`, `INSTALLED AT`.

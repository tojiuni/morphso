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

Show package details: description, type, tags, dependencies, and total RAM and disk requirements.

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
| `--template` | Save config template to `./<package-slug>.env` (e.g., `./gopedia.env`) |
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

Uninstall a package and record the removal in hub history. If other installed packages depend on this one, morphso shows the dependents and prompts for how to proceed:

- **[1] Cascade** — remove this package and all packages that depend on it
- **[2] Force** — remove only this package; dependents remain but lose this dependency
- **[3] Cancel** — abort the removal

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

**Example output:**

```
PACKAGE     VERSION  STRATEGY  OS/ARCH        INSTALLED AT
gopedia     1.2.0    docker    linux/amd64    2026-05-20 10:32:01
postgresql  15.0     docker    linux/amd64    2026-05-20 10:31:55
```

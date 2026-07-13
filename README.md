# Skubell

> *skubell* [/'sky:bɛl/] — "broom" in Breton. A cleanup tool for MediaWiki administrators.

Skubell is a cross-platform desktop application for MediaWiki administrators. It focuses on the
tasks that require administrator (sysop) rights and that existing tools handle poorly at scale.

It talks to a wiki through the standard **MediaWiki Action API** and authenticates with a
**Bot Password**, so it works against Wikipedia and any third-party MediaWiki (Fandom, Miraheze,
wiki.gg, self-hosted) without installing anything server-side.

**License:** GPL-3.0 · **Language:** Go 1.26 · **GUI:** Fyne v2 · **Minimum MediaWiki:** 1.43 LTS

---

## ⚠ Project status

Skubell is in **early development (v0.1-dev, Phase 1 MVP)**. One workflow is fully implemented
and verified end-to-end against live MediaWiki:

- ✅ **Delete pages** — mass deletion with talk-page and redirect handling.

---

## Features (current)

**Delete pages** — select pages, review, and delete them in bulk:

- **Talk pages** — optionally delete each page's associated talk page in the same operation.
- **Redirects** — optionally delete the *transitive closure* of redirects pointing at the pages
  being removed, so no broken double-redirects are left behind.
- **Verification screen** — before anything is executed, Skubell shows the *full expanded list* of
  what will be deleted (selected pages, redirects, and talk pages), grouped by root, with an
  accurate operation and page count. Deletion is sensitive, so you see the exact blast radius first.
- **Dry-run** — simulate the whole plan without performing any write.
- **Rights-aware** — the workflow is enabled only if your Bot Password grants the `delete` right,
  and individual `MediaWiki:*.css/.js/.json` pages are gated on the specific site-CSS/JS/JSON rights.
- **Throttled** — write requests are rate-limited (default 1/second, per-wiki configurable) and
  honor MediaWiki's `maxlag` / `Retry-After`.
- **Activity journal** — every action is recorded to an in-app session log and to a persistent
  per-wiki JSONL file, preserving the verbatim MediaWiki error message on any failure.

**Across the app:**

- **Bot Password authentication** — no main-account password is ever stored; the secret lives in
  your operating system's credential store, never in the config file.
- **Known-wiki registry** — pick a Wikimedia project + language, a farm slug (Fandom, Miraheze,
  wiki.gg), or enter a custom URL; the API endpoint is inferred for you.
- **Internationalized** — English, French, and Breton are built into the app; any language can be added
  or overridden at runtime, without recompiling, by dropping a JSON file next to your config file.
  Switchable in Preferences.

---

## Download and install

Pre-built binaries are attached to each [GitHub Release](https://github.com/Hunvreus-wiki/skubell/releases),
alongside a `SHA256SUMS` file to verify them. Pick the file for your platform:

| Platform | File |
|---|---|
| Linux (x86-64) | `skubell-<version>-linux-amd64.tar.xz` |
| Windows (x86-64) | `skubell-<version>-windows-amd64.exe` |
| macOS (Apple Silicon) | `skubell-<version>-darwin-arm64.zip` |

After installing, jump to [First use](#first-use) to connect a wiki.

### Linux (x86-64)

```bash
# Extract (binary + icon + .desktop launcher, under a usr/local/ tree)
tar -xf skubell-<version>-linux-amd64.tar.xz

# Option A — install system-wide (adds an application-menu entry):
sudo tar -xf skubell-<version>-linux-amd64.tar.xz -C /

# Option B — run in place:
./usr/local/bin/Skubell
```

Needs a working OpenGL/X11 stack (standard on desktop Linux) and a Secret Service provider
(GNOME Keyring, KWallet, …) for the credential store — see [Requirements](#requirements).

### Windows (x86-64)

Download the `.exe` and double-click it — it is a single portable executable, no installer. Because
the build is unsigned, **SmartScreen** may show *"Windows protected your PC"*: click **More info →
Run anyway**.

### macOS (Apple Silicon)

Unzip and move `Skubell.app` to `/Applications`. The app is **unsigned and un-notarised**, so the
first launch needs one extra step (Gatekeeper blocks unidentified developers):

- **Right-click** (or Control-click) `Skubell.app` → **Open** → **Open** in the dialog. macOS
  remembers the choice, so later launches are ordinary double-clicks.
- Or clear the quarantine flag from Terminal: `xattr -dr com.apple.quarantine Skubell.app`.

### Verify a download (optional)

```bash
sha256sum -c SHA256SUMS        # Linux
shasum -a 256 -c SHA256SUMS    # macOS
```

---

## Requirements

**To run:**

- **Windows, macOS, or Linux** (desktop).
- A target wiki running **MediaWiki 1.43 or newer**.
- A **Bot Password** on that wiki (created at `Special:BotPasswords`) with the grants your task
  needs — for deletion, include the *Delete pages* grant (and *High-volume editing* if you plan to
  act on many pages).
- **Linux only:** a working **Secret Service** provider (GNOME Keyring, KWallet, …). This is included in most
  distributions. On minimal window managers you can run `gnome-keyring-daemon` standalone.

**To build:**

- **Go 1.26+**.
- A **C compiler** and the usual Fyne system dependencies (OpenGL / X11 / Wayland headers). See the
  [Fyne Getting Started](https://docs.fyne.io/started/) guide for the packages required on your
  platform (on Debian/Ubuntu:
  `gcc libgl1-mesa-dev libegl1-mesa-dev libgles2-mesa-dev libwayland-dev libxkbcommon-dev xorg-dev`).

---

## Build and run

```bash
# Build the binary to ./bin/skubell
make build

# Run it (translations are embedded, so it runs from any directory)
make run
```

Or directly with the Go toolchain:

```bash
go build -o bin/skubell ./cmd/skubell
./bin/skubell
```

To launch in another shipped language:

```bash
LANG=fr ./bin/skubell    # French   (or set the language in Preferences)
LANG=br ./bin/skubell    # Breton
```

### First use

1. Launch Skubell and add a wiki (choose a known project/farm or enter a custom URL).
2. Enter your Bot Password username (`YourAccount@BotName`) and the generated password. The password
   is saved to your OS credential store.
3. Connect. Skubell detects the MediaWiki version, your effective rights, and your block status, and
   enables the workflows you're allowed to use.
4. Open **Delete pages**, build your list, review the verification screen, optionally dry-run, then execute.

---

## Configuration and data

Skubell keeps a single JSON config file (wikis + preferences); Bot Passwords are **not** in it —
they live in the OS credential store.

| Platform | Configuration file |
|---|---|
| Linux | `~/.config/skubell/config.json` |
| macOS | `~/Library/Application Support/Skubell/config.json` |
| Windows | `%APPDATA%\Skubell\config.json` |

Per-wiki activity journals are written as append-only JSONL under the platform data directory
(Linux: `~/.local/share/skubell/journal/`), one subdirectory per wiki.

To add a language or override a shipped one, drop an `active.<lang>.json` file into a `locales/`
folder next to your config file (e.g. `~/.config/skubell/locales/` on Linux). These files are loaded
on top of the built-in translations, so you only need to include the keys you want to change. See
[`locales/README.md`](locales/README.md) for the format.

---

## Development

```bash
make test          # unit tests (go test ./internal/...)
golangci-lint run  # lint (config in .golangci.yml; 120-char line limit)
```

Integration tests run against real MediaWiki in Docker (versions 1.43 and 1.46). See
[`README.integration.md`](README.integration.md) for the compose environment, provisioning, and
credentials, then:

```bash
docker compose -f docker-compose.test.yml up -d
./scripts/provision-test-wiki.sh localhost:8081
make test-integration     # runs against both 1.43 (:8081) and 1.46 (:8082)
```

### Architecture

Business logic never touches the API directly. Work flows through three decoupled levels, which
keeps planners unit-testable and isolates MediaWiki API changes to a single layer:

1. **Semantic operations** (`internal/ops`) — domain vocabulary (`DeletePage{Title, Reason}`),
   independent of any API version. This is also what the journal and dry-run display.
2. **Translator** (`internal/api`) — converts each operation into concrete MediaWiki API calls,
   accounting for the wiki's detected version and capabilities.
3. **Executor** (`internal/api`) — performs the HTTP requests with throttling, retries, and CSRF
   token handling. Mock and dry-run executors back the tests and simulation mode.

Layout:

```
cmd/skubell/      Entry point, app icon, FyneApp.toml packaging metadata
internal/
  ops/            Level 1 — semantic operations, execution plans, journal
  api/            Levels 2 & 3 — translator, executor, capability detection
  deletion/       Deletion planner (the implemented workflow)
  ui/             Fyne interface (welcome, wiki settings, deletion, …)
  config/         JSON config: wikis + preferences
  security/       OS credential store (keyring)
  registry/       Known-wiki URL inference (Wikimedia, Fandom, Miraheze, …)
  i18n/           go-i18n wrapper (T/Td/Tp/Tpd helpers)
  integration/    Live-wiki integration suite (build tag: integration)
  merge/ blocking/ protect/ revdel/ augeas/   Scaffolded — future workflows
locales/          Translations (en, fr, br) embedded into the binary + translator guide
```

Contributor guidance lives in [`CLAUDE.md`](CLAUDE.md) and [`guidelines.md`](guidelines.md);
translation help is in [`locales/README.md`](locales/README.md).

---

## License

Skubell is free software licensed under the **GNU General Public License v3.0**. See
[`LICENSE`](LICENSE).

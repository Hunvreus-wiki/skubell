# Integration Test Environment

This repository provides a dual-MediaWiki integration environment for Skubell Track N.

## What Gets Started

`docker-compose.test.yml` starts:
- MediaWiki 1.43 LTS (current long-term support) on `http://localhost:8081`
- MediaWiki 1.46 (latest stable) on `http://localhost:8082`
- One MariaDB instance per wiki

Ports are intentionally version-neutral and fixed:
- `8081`: current LTS
- `8082`: latest stable version

## Quick Start (Both Versions)

```bash
docker compose -f docker-compose.test.yml up -d
./scripts/provision-test-wiki.sh localhost:8081
./scripts/provision-test-wiki.sh localhost:8082
```

After provisioning, the following accounts are available. Credentials are identical on both
wikis (`8081`/`8082`) except where a per-wiki value is shown.

### Web-login accounts (normal browser / API `action=login` with an account password)

| Username | Password | Groups | Notes |
| --- | --- | --- | --- |
| `admin` | `otempssuspendstonvol` | `sysop`, `bureaucrat` | convenience account for manual browser testing |
| `TestAdmin` | `TestAdminPass!` | `sysop`, `bureaucrat`, `suppress` | in `suppress` so revdel rights reach the bot logins (see below) |
| `TestEditor` | `TestEditorPass!` | (default) | |
| `TestBlocked` | `TestBlockedPass!` | `sysop` | **sitewide blocked** |
| `TestPartial` | `TestPartialPass!` | `sysop` | **partial block** (namespace 0) |
| `Shiva` | `ShivaPass!` | `suppress` | **non-admin suppressor**: revdel + suppression rights without the admin bundle |
| `Vishnu` | `VishnuPass!` | `sysop`, `suppress` | **admin+suppressor**: the admin bundle and suppression together |
| `WikiSysop` | `WikiSysopPass143!` (8081) / `WikiSysopPass146!` (8082) | `sysop`, `bureaucrat` | created by the installer; the provisioning script logs in as this to seed pages/blocks |

### Bot-password logins (API login; username is `Account@AppId`)

| Login | Password | Grants |
| --- | --- | --- |
| `TestAdmin@SkubellTest` | `ovgj07dt13opeuti773d17i96hamrg7g` (8081) / `r7elmkikmc1mqehngiqo8rrqcs2kktpu` (8082) | `basic`, `highvolume`, `delete`, `protect`, `createeditmovepage` |
| `TestEditor@SkubellTest` | `testeditor00botpass00skubell0002` | `basic`, `highvolume`, `delete`, `protect`, `createeditmovepage` |
| `TestBlocked@SkubellTest` | `testblocked0botpass00skubell0003` | `basic`, `highvolume`, `delete`, `protect`, `createeditmovepage` |
| `TestPartial@SkubellTest` | `testpartial0botpass00skubell0004` | `basic`, `highvolume`, `delete`, `protect`, `createeditmovepage` |
| `Shiva@SkubellTest` | `shiva0000000botpass00skubell0005` | `basic`, `highvolume`, `delete`, `oversight`, `viewrestrictedlogs` |
| `Vishnu@SkubellTest` | `vishnu000000botpass00skubell0006` | `basic`, `highvolume`, `delete`, `protect`, `createeditmovepage`, `oversight`, `viewrestrictedlogs` |
| `TestAdmin@SkubellIface` | `iface0edit0grant0testadmin0mw143` (8081) / `iface0edit0grant0testadmin0mw146` (8082) | `basic`, `highvolume`, `delete`, `editinterface`, `editsiteconfig` |

A bot session's rights are the intersection of the account's rights and the bot password's grants. The
`delete` grant carries `deleterevision`/`deletelogentry`, but vanilla MediaWiki gives those rights to the
`suppress` group only (not `sysop`), so `TestAdmin` is added to `suppress` — this makes revdel effective on
`TestAdmin@SkubellTest`. Suppression (`suppressrevision`) is deliberately NOT effective there: it would need
the `oversight` grant on top of the account right. Use `Shiva@SkubellTest` for the suppressor persona —
`Shiva` is in `suppress` only (no sysop), and its grants expose revdel, suppression, and the suppression log —
and `Vishnu@SkubellTest` for the admin+suppressor persona (everything effective at once).

`TestAdmin@SkubellIface` is the interface-edit bot password: it can edit/delete `MediaWiki:`
namespace pages (effective rights `editinterface` + `delete`). Sitewide `MediaWiki:*.css`/`*.js`
pages additionally require the account to be in the `interface-admin` group, which `TestAdmin`
is not by default, so `editsitecss`/`editsitejs` are not effective.

> **Note:** Bot passwords must match `^[0-9a-w]{32,}$` (digits `0-9`, lowercase `a`–`w`, ≥32 chars).
> MediaWiki (`BotPassword::canonicalizeLoginData`) only recognizes a login as a bot-password
> login when the password is in this charset; anything with uppercase, `x`/`y`/`z`, or
> underscores is stored but silently rejected at login as "Incorrect username or password".

## Single-Version Setup

Current LTS only:

```bash
docker compose -f docker-compose.test.yml up -d mw143-db mw143
./scripts/provision-test-wiki.sh localhost:8081
```

Latest stable only:

```bash
docker compose -f docker-compose.test.yml up -d mw146-db mw146
./scripts/provision-test-wiki.sh localhost:8082
```

## Teardown

```bash
docker compose -f docker-compose.test.yml down
```

To remove all test data volumes too:

```bash
docker compose -f docker-compose.test.yml down -v
```

## Provisioning Details

`scripts/provision-test-wiki.sh` performs:
- user creation via MediaWiki maintenance scripts (`createAndPromote`)
- bot-password creation via MediaWiki maintenance script (`createBotPassword`)
- seed page creation (main, talk, project namespaces) via API
- sitewide block of `TestBlocked` via API
- partial block of `TestPartial` (namespace 0 restriction) via API

`scripts/wait-for-wiki.sh` polls `api.php` until the wiki is responsive.

## Running Tests Against A Target Wiki

Integration tests should set `SKUBELL_TEST_API`:

```bash
SKUBELL_TEST_API=http://localhost:8081/api.php go test -tags integration ./...
SKUBELL_TEST_API=http://localhost:8082/api.php go test -tags integration ./...
```

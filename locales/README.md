# Translations

Skubell's interface strings live in `active.<lang>.json` files in this directory, where `<lang>` is an
ISO 639 code (`en`, `fr`, `br`, …). The files shipped here are **embedded into the binary** at build time
(`go:embed`, see [`locales.go`](locales.go)), so the app is fully translated out of the box with no extra
files to install. English (`active.en.json`) is the source language; every other file translates its keys.

## File format

Each file is a flat JSON object of message IDs. A message is either a single string (via `other`) or a set
of [CLDR plural forms](https://cldr.unicode.org/index/cldr-spec/plural-rules) (`one`, `two`, `few`, `many`,
`other`) for messages that vary with a count. Placeholders use Go template syntax (`{{.Name}}`).

```json
{
  "common_delete": { "other": "Delete" },
  "del_results_count": {
    "one":   "{{.Count}} result",
    "other": "{{.Count}} results"
  }
}
```

Rules of thumb:

- Keep the message IDs and every `{{.Placeholder}}` exactly as they appear in `active.en.json`; only translate
  the surrounding text.
- Provide the plural categories your language actually uses. A count that hits a category you omit falls back
  to this file's `other` form (never to English), so `one`/`other` alone is a safe minimum.
- Any key you leave out falls back to the English default, so a partial translation is always valid.

## Overriding translations without rebuilding

You don't need to recompile to add or tweak a language. Drop an `active.<lang>.json` file into a `locales/`
folder next to your config file and it is loaded **on top of** the embedded translations at startup:

| Platform | User locales folder |
|---|---|
| Linux | `~/.config/skubell/locales/` |
| macOS | `~/Library/Application Support/Skubell/locales/` |
| Windows | `%APPDATA%\Skubell\locales\` |

Because these files layer over the built-ins, you only include the keys you want to change. The language then
appears in **Preferences → Language**, switchable at runtime.

## Contributing a language

To have a language shipped with Skubell, add or update the `active.<lang>.json` file in this directory and open
a pull request — it will be embedded in the next release. Use `active.en.json` as the list of keys to cover.

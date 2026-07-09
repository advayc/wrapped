# Wrapped

Wrapped is a simple local iMessage Wrapped for macOS written in Go. It reads your Messages database, lets you choose a timeframe in the terminal, then opens a Spotify-style wrapped website to view your stats. All data is local and nothing is uploaded anywhere

## Use

### Fastest way

Paste this directly into Terminal on macOS. It does not require Go or a cloned repo:

```bash
curl -fsSL https://raw.githubusercontent.com/advayc/wrapped/main/imsgwrap.sh | bash
```

### If you want to keep the script locally

```bash
curl -fsSL https://raw.githubusercontent.com/advayc/wrapped/main/imsgwrap.sh -o imsgwrap.sh
chmod +x imsgwrap.sh
./imsgwrap.sh
```

### If you already cloned the repo

```bash
./imsgwrap.sh
```

The default behavior is binary-first: if the script cannot find a local executable, it downloads the prebuilt macOS binary into your cache and runs that.

If you want the local Go development path, opt in explicitly:

```bash
IMSGWRAP_USE_GO=1 ./imsgwrap.sh
```

For local development in this repo:

```bash
go run ./cmd/imsgwrap
```

## Notes

- Requires macOS Messages data and Full Disk Access for your terminal.
- Output is written to `imsgwrap-output/` and opened automatically.
- Nothing is uploaded; `index.html` and `data.json` are local files.
- Tapbacks are counted as reactions, not normal messages.

## Flags

```bash
./imsgwrap --year 2025
./imsgwrap --years 2022,2023,2024,2025
./imsgwrap --last 90d
./imsgwrap --from 2024-01-01 --to 2024-06-01
./imsgwrap --all
./imsgwrap --redact
./imsgwrap --no-open
```

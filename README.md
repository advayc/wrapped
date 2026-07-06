# imsgwrap

Local iMessage Wrapped for macOS. It reads your Messages database, lets you choose a timeframe in the terminal, then opens a Spotify-style local website.

## Use

```bash
curl -fsSL https://raw.githubusercontent.com/AdvayChandorkar/imsgwrap/main/imsgwrap.sh -o imsgwrap
chmod +x imsgwrap
./imsgwrap
```

For local development in this repo:

```bash
./imsgwrap.sh
```

## Notes

- Requires macOS Messages data and Full Disk Access for your terminal.
- Output is written to `imsgwrap-output/`.
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

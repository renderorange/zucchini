# zucchini

Spams Bluetooth connection attempts at Meta glasses until they accept one. Runs
on a Raspberry Pi as a single binary with no runtime dependencies.

Built for glasses that are flaky to connect to. No backoff, no retry limit, no
giving up. If the glasses are around, zucchini keeps trying until they answer.

## How it works

1. Scans for nearby Bluetooth devices.
2. Matches them against known Meta glasses signatures. The match is by Bluetooth
   SIG company ID, which is stable across MAC randomization, so it survives
   firmware changes and covers new pairs without a code change.
3. Once a device matches, fires connection attempts back-to-back from several
   workers at once. Failures do not slow the next attempt.
4. If a connection holds, workers go idle and watch for it to drop.
5. If it drops, hammering resumes immediately.
6. If the signature stops broadcasting for a grace period, zucchini stops
   hammering and goes back to scanning. When the glasses return, so does the
   hammering.

Multiple devices are handled at once. Each matched device gets its own set of
workers.

## Signatures

Shipped in `zucchini.json`:

| Field | Value | Source |
|---|---|---|
| Company ID | `0x0D53` | Luxottica (Ray-Ban Meta, Oakley Meta) |
| Company ID | `0x01AB` | Meta Platforms |
| Company ID | `0x058E` | Meta Platforms Technologies |
| Service UUID | `0xFD5F` | Oculus VR |

Name patterns (`Ray-Ban`, `Oakley Meta`, `Meta Glasses`) are a weak fallback.
Company ID is the reliable signal.

A device matches if it hits any configured field. To target a new device family,
add a row to `signatures` in the config. No rebuild needed.

## Configuration

`zucchini.json`:

```json
{
    "adapter": "hci0",
    "grace_seconds": 15,
    "workers": 4,
    "call_timeout_ms": 5000,
    "attempt_gap_ms": 20,
    "signatures": [
        {
            "name": "meta-glasses",
            "company_ids": ["0x0D53", "0x01AB", "0x058E"],
            "service_uuids": ["0xFD5F"],
            "name_patterns": ["Ray-Ban", "Oakley Meta", "Meta Glasses"]
        }
    ]
}
```

| Setting | Default | Meaning |
|---|---|---|
| `adapter` | `hci0` | Which Bluetooth adapter to use |
| `grace_seconds` | `15` | How long the signature can go unseen before zucchini stops hammering |
| `workers` | `4` | Concurrent connection attempts per device |
| `call_timeout_ms` | `5000` | How long one connection attempt may block before giving up |
| `attempt_gap_ms` | `0` | Delay between attempts. Zero means no pause. This is not a backoff and it never grows. |

## Build

```sh
go build -o zucchini ./cmd/zucchini
```

Cross-compile for a Pi from another machine:

```sh
GOOS=linux GOARCH=arm64 go build -o zucchini ./cmd/zucchini   # Pi 3/4/5 (64-bit)
GOOS=linux GOARCH=arm GOARM=7 go build -o zucchini ./cmd/zucchini   # 32-bit
```

The binary is statically linked Go. The Pi needs BlueZ installed and running,
which any Raspberry Pi OS with Bluetooth has.

## Install

```sh
scp zucchini pi@your-pi:/usr/local/bin/zucchini
scp zucchini.json pi@your-pi:/etc/zucchini.json
scp zucchini.service pi@your-pi:/etc/systemd/system/
ssh pi@your-pi 'sudo systemctl enable --now zucchini'
```

Follow the logs:

```sh
ssh pi@your-pi 'journalctl -u zucchini -f'
```

## What the logs tell you

| Message | Meaning |
|---|---|
| `target acquired: ...` | A matching device was found and hammering began |
| `connect failed: ...` | One attempt failed. Normal while hammering. |
| `target released: ...` | Hammering stopped. Either the grace window expired or the device left. |

If you see `connect failed` repeating and never `target acquired` followed by a
held connection, the glasses are refusing the Pi. See the risk note below.

## Testing

```sh
go test ./...
go test ./... -race
```

The retry loop is tested against a fake Bluetooth backend rather than a real
radio, so the state machine is covered without hardware: starting and stopping
hammering, idling while connected, resuming after a drop, releasing after the
grace window, and issuing concurrent attempts. The BlueZ message parsing is
tested against synthetic signals with no live bus involved.

## Known risk

It is not yet confirmed that Meta glasses accept a connection from a non-phone
device. They are designed as a phone accessory, and the initial pairing may be
gated by Meta's app. If pairing is refused, zucchini will hammer forever and
connect nothing.

zucchini doubles as the test rig for that question. If it runs and the glasses
eventually connect, the risk is cleared. If it does not, that is a hard answer
about the hardware, not a bug in this tool.

## Status

Phase one: connection loop only. Audio playback is deliberately out of scope for
now and may come later.

## License

See repository license.

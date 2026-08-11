# anker-solix-monitor

Monitor an Anker Solix F2000 / 767 PowerHouse over Bluetooth Low Energy using a Raspberry Pi.

## Contents

- [`pkg/solix`](#pkgsolix-sdk) — reusable Go BLE SDK for Solix devices
- [`cmd/solix-cli`](#solix-cli) — command-line diagnostic tool
- [`cmd/solix-monitor`](#solix-monitor) — self-contained monitoring service

---

## solix-monitor

`solix-monitor` is a single binary that:

- Connects to the F2000 over BLE and polls telemetry continuously
- Stores readings in a local SQLite database
- Serves a web dashboard and REST API
- Requires **no** external runtime dependencies other than Bluetooth hardware

### Quick start (development)

```bash
# Clone and build
git clone https://github.com/nicksantamaria/anker-solix-monitor
cd anker-solix-monitor
go build -o solix-monitor ./cmd/solix-monitor/

# Run (requires root / CAP_NET_RAW for BLE on Linux)
sudo SOLIX_ADDRESS=E8:EE:CC:7C:0A:2A ./solix-monitor
```

Open `http://localhost:8080` in a browser to view the dashboard.

### Configuration

Settings are read from **environment variables** (highest precedence), then **command-line flags**, then built-in defaults.

| Environment variable    | Flag               | Default               | Description                        |
|-------------------------|--------------------|-----------------------|------------------------------------|
| `SOLIX_ADDRESS`         | `-addr`            | `E8:EE:CC:7C:0A:2A`  | BLE MAC address of the F2000       |
| `SOLIX_DB_PATH`         | `-db`              | `./solix.db`          | SQLite database file path          |
| `SOLIX_LISTEN`          | `-listen`          | `0.0.0.0:8080`        | HTTP server bind address           |
| `SOLIX_POLL_INTERVAL`   | `-poll-interval`   | `30s`                 | Telemetry polling interval         |
| `SOLIX_LOG_LEVEL`       | `-log-level`       | `info`                | Log level: debug/info/warn/error   |
| `SOLIX_CONNECT_TIMEOUT` | `-connect-timeout` | `30s`                 | BLE connection timeout             |
| `SOLIX_SCAN_TIMEOUT`    | `-scan-timeout`    | `10s`                 | BLE scan timeout                   |

Example using flags:

```bash
sudo ./solix-monitor \
  -addr E8:EE:CC:7C:0A:2A \
  -db /var/lib/solix-monitor/solix.db \
  -listen 0.0.0.0:8080 \
  -poll-interval 30s \
  -log-level info
```

### Building for Raspberry Pi Model B Rev 2 (ARMv6)

```bash
GOOS=linux GOARCH=arm GOARM=6 go build -o solix-monitor ./cmd/solix-monitor/
```

The binary is self-contained (~15 MB) and includes the web dashboard. No Node.js, npm, nginx, or separate frontend server required on the Pi.

### Deploying to the Raspberry Pi

```bash
# Build the ARMv6 binary on your development machine
GOOS=linux GOARCH=arm GOARM=6 go build -o solix-monitor ./cmd/solix-monitor/

# Copy to the Pi
scp solix-monitor pi@raspberrypi:/home/pi/

# SSH in and run
ssh pi@raspberrypi
sudo SOLIX_ADDRESS=E8:EE:CC:7C:0A:2A ./solix-monitor
```

To run as a systemd service, create `/etc/systemd/system/solix-monitor.service`:

```ini
[Unit]
Description=Anker Solix Monitor
After=network.target bluetooth.target

[Service]
ExecStart=/home/pi/solix-monitor
Environment=SOLIX_ADDRESS=E8:EE:CC:7C:0A:2A
Environment=SOLIX_DB_PATH=/var/lib/solix-monitor/solix.db
Restart=on-failure
RestartSec=10
User=root

[Install]
WantedBy=multi-user.target
```

```bash
sudo mkdir -p /var/lib/solix-monitor
sudo systemctl enable --now solix-monitor
```

### Accessing the dashboard

Open `http://<pi-ip>:8080` in a browser (e.g. on your iPad).

The dashboard:
- Refreshes current status every 30 seconds automatically
- Shows battery %, solar input, AC input/output, DC output, temperature
- Displays historical charts for 1h / 6h / 24h / 7d ranges
- Clearly indicates when BLE communication has been lost

### HTTP API

| Endpoint             | Description                                      |
|----------------------|--------------------------------------------------|
| `GET /`              | Web dashboard                                    |
| `GET /api/status`    | Most recent telemetry snapshot (JSON)            |
| `GET /api/history`   | Historical data — `?hours=24` (default 24, max 168) |
| `GET /api/health`    | Service health: uptime, BLE status, DB status   |

#### `/api/status` example response

```json
{
  "id": 42,
  "timestamp": "2026-08-11T11:30:00Z",
  "device_addr": "E8:EE:CC:7C:0A:2A",
  "battery_percent": 82,
  "solar_power_w": 435,
  "ac_power_in_w": 0,
  "ac_power_out_w": 126,
  "dc1_power_out_w": 8,
  "temperature_c": 28,
  "serial_number": "A17809XXXXXXXX"
}
```

#### `/api/health` example response

```json
{
  "status": "ok",
  "uptime_seconds": 3612,
  "ble_connected": true,
  "last_poll": "2026-08-11T11:29:55Z",
  "last_error": null,
  "db_ok": true
}
```

### Database

SQLite file at the configured `SOLIX_DB_PATH` (default `./solix.db`).

#### Schema — `telemetry` table

| Column                 | Type    | Description                        |
|------------------------|---------|------------------------------------|
| `id`                   | INTEGER | Auto-increment primary key         |
| `timestamp`            | DATETIME| UTC RFC3339                        |
| `device_addr`          | TEXT    | BLE MAC address                    |
| `battery_percent`      | INTEGER | Battery level (%)                  |
| `battery_percent_exp`  | INTEGER | Expansion battery (%)              |
| `battery_health`       | INTEGER | Battery health (%)                 |
| `solar_power_w`        | INTEGER | Solar input (W)                    |
| `ac_power_in_w`        | INTEGER | AC input (W)                       |
| `ac_power_out_w`       | INTEGER | AC output (W)                      |
| `ac_to_battery_w`      | INTEGER | AC charging to battery (W)         |
| `ac_out_sockets_w`     | INTEGER | AC socket output (W)               |
| `dc1_power_out_w`      | INTEGER | DC output port 1 (W)               |
| `dc2_power_out_w`      | INTEGER | DC output port 2 (W)               |
| `usbc1_power_w`        | INTEGER | USB-C port 1 (W)                   |
| `usbc2_power_w`        | INTEGER | USB-C port 2 (W)                   |
| `usbc3_power_w`        | INTEGER | USB-C port 3 (W)                   |
| `usba1_power_w`        | INTEGER | USB-A port 1 (W)                   |
| `usba2_power_w`        | INTEGER | USB-A port 2 (W)                   |
| `temperature_c`        | INTEGER | Temperature (°C)                   |
| `time_remaining_hours` | REAL    | Time to full/empty (hours)         |
| `serial_number`        | TEXT    | Device serial number               |
| `software_version`     | TEXT    | Firmware version string            |

Indexes on `timestamp` and `device_addr`.

---

## solix-cli

Command-line diagnostic tool for ad-hoc interaction with a Solix device.

```bash
go build -o solix-cli ./cmd/solix-cli/

# Scan for nearby devices
sudo ./solix-cli scan

# Print a single telemetry snapshot
sudo ./solix-cli status -addr E8:EE:CC:7C:0A:2A

# Stream telemetry updates
sudo ./solix-cli monitor -addr E8:EE:CC:7C:0A:2A
```

---

## pkg/solix SDK

Reusable Go package for connecting to Anker Solix devices over BLE.

```go
import "github.com/nicksantamaria/anker-solix-monitor/pkg/solix"

client, err := solix.NewClient(solix.Config{})
device, err := client.Connect(ctx, "E8:EE:CC:7C:0A:2A")
status, err := device.Status(ctx)
```

The SDK has no dependency on HTTP, databases, or application logic.

---

## Development

```bash
# Run all tests
go test ./...

# Lint
go vet ./...

# Format
gofmt -w .

# Build native binary
go build ./cmd/solix-monitor/

# Cross-compile for Raspberry Pi 1 (ARMv6)
GOOS=linux GOARCH=arm GOARM=6 go build ./cmd/solix-monitor/
```

BLE hardware is not required for tests — the monitor and API packages use injectable interfaces and in-memory mocks.

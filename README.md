# mifstat — Multi-switch SNMP Bandwidth Monitor

![mifstat screenshot](img/mifstat-screenshot.png)

`mifstat` is a real-time, terminal-based bandwidth monitor for multiple SNMP-enabled switches. It provides a consolidated view of traffic across all your network devices with live sparklines, performance metrics, and detailed per-port views.

## Features

- **Multi-Switch Monitoring**: Polls dozens of switches concurrently using SNMP BulkWalk.
- **Extreme Memory Efficiency**: 
    - Uses pre-allocated **ring buffers** and `float32` precision.
    - **Streaming State Loader**: Loads data directly from disk into ring buffers, eliminating peak RAM spikes during startup.
    - RAM usage reduced by over 75% compared to v0.1 (verified on production datasets).
- **Near-Instant I/O**: 
    - New **MIFSTAT3** format with timestamp deduplication and zero-allocation loading.
    - State file size reduced by up to 30x (880MB -> 26MB).
- **High-Performance Rendering**: O(N) sparkline logic removes expensive map-based bucketing, resulting in ~60% lower CPU usage during TUI updates.
- **High-Resolution Sparklines**: Vertical history visualization using 10 levels of Unicode characters.
- **Visual Diagnostics**: Directly see issues in the sparkgraph:
    - `!`: SNMP Walk Error at that point in time.
    - `*`: Slow poll (latency exceeded `-slowms` threshold).
- **TUI Interface**: Interactive terminal UI with sorting, zooming, and dynamic table layouts.
- **Real-time Filtering**: Instantly filter switches by Name or IP using the `/` key.
- **Traffic Summary View**: Dedicated numeric view showing Current, 1m Average, and Session Peak traffic.
- **Benchmark Mode**: Diagnoses slow or failing switches with precise timing and "Slow" poll tracking.

## Installation

### Using Makefile (Recommended)

```bash
git clone https://github.com/dpavlin/mifstat-go.git
cd mifstat-go
make build
# Binary 'mifstat' will be created in the current directory
```

### Via Go Install

```bash
go install github.com/dpavlin/mifstat-go@latest
```

## Usage

### Switch List

`mifstat` expects a list of switches (default path `/dev/shm/sw-ip-name-mac`, can be changed with `-f`). The format is:

```text
# IP_ADDRESS NAME [MAC_ADDRESS]
10.20.0.1  core-switch-01
10.20.0.2  edge-switch-02
```

### Basic Commands

```bash
# Start the TUI
./mifstat

# Run a one-shot benchmark to diagnose switch performance
./mifstat -bench

# Enable pprof for live CPU/Memory profiling
./mifstat -pprof :6060

# Use a custom switch list and change history depth
./mifstat -f my_switches.txt -hist 24
```

### Interactive Keys

- `q`: Quit
- `/`: Filter switches by Name/IP (Esc to clear, Enter to apply)
- `d`: Toggle detailed port view
- `p`: Toggle performance metrics (SNMP latency, Errors, MRep)
- `t`: Toggle numeric traffic summary (Current, Average, Peak)
- `v`: Toggle sparklines vs. numeric timeline
- `i` / `o`: Sort by IN / OUT traffic
- `1` / `a`: Sort by IP
- `2` / `n`: Sort by Name
- `3` / `s`: Sort by Status (Down/Error switches at the top)
- `Space`: Toggle auto-sort (freeze view)
- `+` / `-`: Zoom in/out on sparklines
- `Left` / `Right`: Scroll through history
- `PgUp` / `PgDn`: Scroll history by page
- `Enter`: Reset scroll to now

## Configuration Options

- `-c string`: SNMP community string (overrides `~/.config/snmp.community` and default `public`).
- `-f string`: Path to switch list file (default `/dev/shm/sw-ip-name-mac`).
- `-state string`: Path to save history state (default `/tmp/mifstat_go.bin`).
- `-d float`: Poll interval in seconds (default `1.0`).
- `-hist float`: History duration in hours (default `6.0`).
- `-snmptimeout duration`: SNMP timeout per poll (default `3s`).
- `-log string`: Path to log SNMP errors and performance.
- `-pprof string`: Enable pprof profiling on address (e.g. `localhost:6060`).
- `-bench`: Run benchmark mode and exit.
- `-slowms int`: Log polls slower than this many milliseconds (defaults to poll interval `-d * 1000`).
- `-version`: Show version and exit.

## Development

The project uses a strict Red/Green TDD workflow:
- `main.go`: Entry point, TUI loop, and flags.
- `table.go`: Reusable dynamic table layout engine.
- `snmp_poll.go`: Adaptive SNMP polling and OID processing.
- `sparkline.go`: High-resolution visualization logic.
- `state.go`: Fast "streaming" binary state persistence.

## License

MIT

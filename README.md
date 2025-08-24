# wardend

A single-binary process supervisor for Docker containers. No Python, no config files, no dependencies.

## Install

Download the binary for your platform from [releases](https://github.com/kozko2001/wardend/releases).

## Usage

### Basic multi-process container
```bash
./wardend \
  --run "nginx -g 'daemon off;'" --name web --restart always \
  --run "python worker.py" --name worker --restart on-failure
```

### With dependencies
```bash
./wardend \
  --run "redis-server --daemonize no" --name cache --restart always \
  --run "gunicorn app:app" --name app --restart on-failure --depends-on cache
```

### Docker example
```dockerfile
FROM alpine
COPY --from=builder /wardend /usr/local/bin/
CMD ["/usr/local/bin/wardend", \
     "--run", "nginx -g 'daemon off;'", "--name", "nginx", "--restart", "always", \
     "--run", "php-fpm -F", "--name", "php", "--restart", "always"]
```

## Options

### Core Options
- `--run COMMAND` - Process to run (repeatable)
- `--name NAME` - Name for the last --run process
- `--restart POLICY` - Restart policy: `always`, `on-failure`, `never` (default: `always`)
- `--depends-on NAME` - Wait for named process to start
- `--shutdown-timeout DURATION` - Graceful shutdown timeout (default: `10s`)

### Smart Restart System
- `--start-retries COUNT` - Max startup failure attempts (default: `3`)
- `--start-seconds DURATION` - Time to be considered successfully started (default: `60s`)
- `--max-restarts COUNT` - Max runtime restarts, or `infinite` (default: `infinite`)
- `--restart-delay DURATION` - Delay between restart attempts (default: `1s`)

### Logging
- `--log-format FORMAT` - Log format: `json`, `text` (default: `text`)
- `--health-check COMMAND` - Health check command for process
- `--health-interval DURATION` - Health check interval (default: `30s`)

## Why?

Docker containers often need multiple processes, but existing solutions have issues:
- **supervisord**: Requires Python + complex config
- **systemd**: Not available in containers
- **shell scripts**: Poor signal handling

wardend is a single binary that just works.

## Smart Restart System

wardend features a two-phase restart system that handles startup failures differently from runtime crashes:

### Phase 1: Startup Failures
- Processes that exit within `--start-seconds` are considered startup failures
- Limited by `--start-retries` attempts (default: 3)
- Fast failure for broken configurations, missing dependencies, etc.

### Phase 2: Runtime Failures  
- Processes that survive the startup period and then crash
- Controlled by `--max-restarts` (default: infinite)
- Perfect for services that occasionally crash but are generally stable

### Examples

```bash
# Web service that needs time to initialize
./wardend \
  --run "nginx -g 'daemon off;'" \
  --name web \
  --start-seconds 30s \
  --max-restarts infinite

# Database with strict startup limits but flexible runtime restarts
./wardend \
  --run "postgres -D /data" \
  --name db \
  --start-retries 2 \
  --start-seconds 60s \
  --max-restarts 5
```

This approach is superior to supervisord's restart handling and prevents both infinite restart loops from broken configs and premature giving up on occasionally failing services.

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

- `--run COMMAND` - Process to run (repeatable)
- `--name NAME` - Name for the last --run process
- `--restart POLICY` - Restart policy: `always`, `on-failure`, `never` (default: `always`)
- `--depends-on NAME` - Wait for named process to start
- `--shutdown-timeout DURATION` - Graceful shutdown timeout (default: `10s`)
- `--log-format FORMAT` - Log format: `json`, `text` (default: `text`)

## Why?

Docker containers often need multiple processes, but existing solutions have issues:
- **supervisord**: Requires Python + complex config
- **systemd**: Not available in containers
- **shell scripts**: Poor signal handling

wardend is a single binary that just works.

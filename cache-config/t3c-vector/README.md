# NAME

t3c-vector - generate Vector log sink configs from Traffic Ops

# SYNOPSIS

t3c-vector --traffic-ops-url URL --traffic-ops-user USER --traffic-ops-password PASS --cdn-name CDN [options]

# DESCRIPTION

t3c-vector connects to Traffic Ops and generates per-tenant Vector configuration
files for multi-tenant CDN log routing. It writes .yaml files to the output
directory (default: /etc/vector/tenants.d/) and optionally triggers a Vector
reload. Run it via cron or a systemd timer on Vector Central nodes.

# OPTIONS

--traffic-ops-url URL
    Traffic Ops URL (required). Env: TO_URL

--traffic-ops-user USER
    Traffic Ops username (required). Env: TO_USER

--traffic-ops-password PASS
    Traffic Ops password (required). Env: TO_PASS

--traffic-ops-insecure
    Skip TLS certificate verification (default: false)

--traffic-ops-timeout-milliseconds MS
    Request timeout in milliseconds (default: 30000)

--cdn-name CDN
    CDN name to fetch Delivery Services for (required)

--config-file-key KEY
    Traffic Ops Parameter.ConfigFile value used to identify Vector sink
    parameters (default: vector-tenant.yaml)

--output-dir DIR
    Directory for generated tenant config files (default: /etc/vector/tenants.d)

--upstream-transform-id ID
    Vector transform ID that feeds the per-tenant filter transforms
    (default: extract_billing). Must match the transform name in your base vector.yaml.

--reload-command CMD
    Shell command to run after config changes. Leave empty if Vector is started
    with --watch-config (inotify handles reload automatically).

--dry-run
    Print generated config to stdout without writing files.

--log-location-error PATH
    Log destination for error messages (default: stderr)

--log-location-warning PATH
    Log destination for warning messages (default: stderr)

--log-location-info PATH
    Log destination for info messages (default: stderr)

--version
    Print version and exit.

--help
    Print help and exit.

# ENVIRONMENT

TO_URL
    Traffic Ops URL. Overridden by --traffic-ops-url.

TO_USER
    Traffic Ops username. Overridden by --traffic-ops-user.

TO_PASS
    Traffic Ops password. Overridden by --traffic-ops-password.

# TRAFFIC OPS CONFIGURATION

To configure a log sink for a Delivery Service, create a Traffic Ops Parameter:

    ConfigFile: vector-tenant.yaml
    Name:       aws_s3
    Value:      {"bucket":"my-bucket","region":"us-east-1","encoding":{"codec":"json"},"compression":"gzip"}

Assign this Parameter to the Profile of the Delivery Service. t3c-vector
discovers it automatically on the next run.

# VECTOR CONFIGURATION

Start Vector with:

    vector \
      --config /etc/vector/vector.yaml \
      --config-dir /etc/vector/tenants.d/ \
      --watch-config \
      --allow-empty-config

The --allow-empty-config flag is required so Vector can start before
t3c-vector has run for the first time.

# EXIT CODES

0   Success
1   Configuration error
2   Runtime error (Traffic Ops unreachable, file write failed, etc.)

# SEE ALSO

t3c(1), t3c-apply(1)

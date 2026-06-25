# NAME

t3c-vector - generate Vector log sink configs from Traffic Ops

# SYNOPSIS

t3c-vector --traffic-ops-url URL --traffic-ops-user USER --traffic-ops-password PASS --cdn-name CDN [options]

# DESCRIPTION

t3c-vector connects to Traffic Ops and generates per-tenant Vector configuration
files for multi-tenant CDN log routing. It writes .yaml files to the output
directory (default: /etc/vector/tenants.d/) and optionally triggers a Vector
reload. Run it via cron or a systemd timer on Vector Central nodes.

For each Delivery Service that has a matching Traffic Ops Parameter, t3c-vector
generates a Vector pipeline:

    [extract_billing] --> filter_{tenant}__{ds} --> [tier_remap_*] --> ls_{tenant}__{ds}

The `ls_` prefix on sink component IDs is the billing marker: the
`capture_delivery_billing` transform in the base vector.yaml tracks
`component_sent_events_total` for every sink whose ID starts with `ls_`.

On each run, t3c-vector also syncs local database files (MMDB) from URLs
stored in Traffic Ops Parameters. This keeps GeoIP and anonymous-IP databases
up to date without manual intervention.

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

--database-dir DIR
    Directory to store downloaded database files, e.g. MMDB files.
    (default: /etc/vector/database)

--database-config-file-key KEY
    Traffic Ops Parameter.ConfigFile value that identifies database download
    entries. Each such parameter has: Name=filename stem (e.g. geoip_city),
    Value=HTTPS URL to download from.
    (default: vector_database)

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

## Log Sink Parameters (per-Delivery Service)

Create Traffic Ops Parameters with ConfigFile=vector-tenant.yaml and assign
them to the Profile of the Delivery Service.

Example -- S3 sink:

    ConfigFile: vector-tenant.yaml
    Name:       aws_s3
    Value:      {"bucket":"my-bucket","region":"us-east-1","encoding":{"codec":"json"},"compression":"gzip"}

Example -- Splunk HEC sink:

    ConfigFile: vector-tenant.yaml
    Name:       splunk_hec
    Value:      {"endpoint":"https://splunk.example.com","token":"xxx","encoding":{"codec":"json"}}

A Delivery Service may have multiple sink parameters (multiple destinations).

## Log Streaming Tier (per-Delivery Service Profile)

The reserved parameter name `log_streaming_tier` controls which enrichment
fields are included in the customer's log stream.

    ConfigFile: vector-tenant.yaml
    Name:       log_streaming_tier
    Value:      standard   (or: premium)

**standard** (default): GeoIP location data and anonymous-IP threat intel
fields are removed before delivery. Customers receive core access log fields
only. cache_result is normalized to HIT or MISS.

**premium**: All enriched fields are delivered, including:
  - client_country, client_city, client_latitude, client_longitude
  - client_continent, client_timezone, client_registered_country
  - is_vpn, is_tor, is_proxy, is_hosting, is_relay

If the parameter is absent, the tier defaults to **standard**.

## Database Download Parameters (global)

Create Traffic Ops Parameters with ConfigFile=vector_database and assign them
to the GLOBAL profile. Each parameter represents one database file to download:

    ConfigFile: vector_database
    Name:       geoip_city
    Value:      https://internal-storage.example.com/GeoIP2-City.mmdb.gz

    ConfigFile: vector_database
    Name:       anonymous_ip
    Value:      https://internal-storage.example.com/Anonymous-IP.mmdb.gz

t3c-vector downloads each file to {database-dir}/{name}.mmdb. If the URL ends
with `.gz`, the file is automatically decompressed during download -- no manual
extraction needed. Both plain `.mmdb` and compressed `.mmdb.gz` URLs are
supported.

Download behaviour:
  - Files younger than 7 days: skipped entirely.
  - Files older than 7 days: re-checked via HTTP If-Modified-Since.
    Server returns 304: local mtime is touched, content unchanged.
    Server returns 200: file is rewritten atomically via a .tmp rename.

Adding new database types in the future requires only a new Traffic Ops
Parameter -- no source code changes needed.

# VECTOR CONFIGURATION

Start Vector with:

    vector \
      --config /etc/vector/vector.yaml \
      --config-dir /etc/vector/tenants.d/ \
      --watch-config \
      --allow-empty-config

The --allow-empty-config flag is required so Vector can start before
t3c-vector has run for the first time.

# TRAFFIC OPS USER (SERVICE ACCOUNT)

Create a dedicated read-only user for t3c-vector. It does not require write
access to any resource.

Required role permissions (API v4+):

    CDN:READ
    DELIVERY-SERVICE:READ
    TYPE:READ
    PARAMETER:READ

The user must be in the root tenant (or a tenant that can read all CDN
Delivery Services) so that t3c-vector can discover sinks for all customers.

# BILLING

Billing for log streaming is tracked via Vector's internal metrics. Every sink
whose component ID starts with `ls_` is counted by the
`capture_delivery_billing` transform. The billing worker queries:

    SELECT sum(events_delta), sum(bytes_delta)
    FROM cdn_billing.log_streaming_billing
    WHERE tenant = '...' AND delivery_service = '...'

The `ls_` prefix is the coupling contract between t3c-vector and the billing
pipeline. Do not rename sink IDs manually.

# EXIT CODES

0   Success
1   Configuration error
2   Runtime error (Traffic Ops unreachable, file write failed, etc.)

# SEE ALSO

t3c(1), t3c-apply(1)

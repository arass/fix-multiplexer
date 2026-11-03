# Docker setup for development and UAT

This guide runs one FIX Multiplexer container with one upstream FIX 4.4 session and separate downstream sessions for developer clients. The image is a Linux image; on Windows, Docker Desktop must be running in Linux-container mode. The gateway is for nonproduction testing with trusted participants and permitted test data. Its security review is incomplete.

For a shared server, install and run one container on that server. Developers connect from their own machines to the server; they do not each need to run the container. The server can accept connections over the internet when its public address, Docker port mapping, and firewalls are configured as described below.

## Give the image to a developer

From a machine that already has the `fix-multiplexer:latest` image, export it in PowerShell:

```powershell
docker save -o fix-multiplexer.tar fix-multiplexer:latest
```

Send the tar file and a copy of [`config/example.yaml`](../config/example.yaml) to the operator. Exporting saves the image currently in Docker; rebuild it first if source changes need to be included. The recipient loads it with:

```powershell
docker load -i .\fix-multiplexer.tar
```

## Configure the gateway

Copy `example.yaml` to `local.yaml` and edit the copy. Obtain the counterparty's UAT host, port, assigned FIX identities, credentials and TLS requirements. Agree on initial sequence numbers and supported message types with the counterparty.

- Set `upstream.host`, `upstream.port`, `upstream.sender_comp_id`, and `upstream.target_comp_id`. A host of `127.0.0.1` refers to the container itself; for a service on the Docker Desktop host, use `host.docker.internal`.
- Set `downstream.listen: 0.0.0.0:9879` to accept connections through Docker's published port. Set `downstream.target_comp_id` to the gateway identity that clients will use as their TargetCompID.
- Add a distinct `sender_comp_id` and password reference for each enabled client. A password reference is required for every enabled client when the listener is non-loopback. Each client sends its password in FIX Logon tag 554.
- Check the upstream and fanout application message type lists against the messages required for testing. See the [README](../README.md) for defaults and configuration details.
- Optionally set `read_only: true` or a `receive_message_types` list for individual clients; see the [subscriber example](../config/drop-copy.example.yaml). Receive filters do not isolate orders belonging to different clients.
- Keep `upstream.store_dir: ./state/upstream` and `downstream.store_dir: ./state/downstream` for the commands below. Relative paths resolve from `/app` in the container.
- Use environment or mounted file references for credentials; do not put credential values in YAML. Configure TLS as required, and mount referenced certificates, keys, secret files, or custom dictionaries at the paths used in the config. They must be readable by the nonroot container user.

For example, this is a replacement for the `downstream` section, not a complete configuration:

```yaml
downstream:
  fix_version: FIX.4.4
  listen: 0.0.0.0:9879
  target_comp_id: MULTIPLEXER
  clients:
    - sender_comp_id: DEV_A
      password: {env: FIX_DEV_A_PASSWORD}
  store_dir: ./state/downstream
```

For TLS and additional credential examples, see [FIX credentials and TLS configuration](../README.md#fix-credentials-and-tls-configuration). Without TLS, passwords and FIX messages are unencrypted; use an encrypted VPN and restrict access.

## Validate and start

In PowerShell, change to the directory containing `local.yaml`. Set the environment variables referenced by the configuration using your organization's approved secret-handling method. Check the configuration:

```powershell
docker run --rm `
  --mount "type=bind,source=$((Resolve-Path .\local.yaml).Path),target=/app/config/local.yaml,readonly" `
  fix-multiplexer:latest -config /app/config/local.yaml -check-config
```

Validation checks configuration syntax and constraints; it does not prove credentials, certificates, connectivity, or FIX logon.

Start the gateway:

```powershell
docker run -d --name fix-multiplexer `
  --mount "type=bind,source=$((Resolve-Path .\local.yaml).Path),target=/app/config/local.yaml,readonly" `
  --mount "type=volume,source=fix-multiplexer-state,target=/app/state" `
  -p 9879:9879 `
  -e FIX_DEV_A_PASSWORD `
  fix-multiplexer:latest -config /app/config/local.yaml
```

Add an `-e VARIABLE_NAME` for every environment credential referenced by the config. Add read-only mounts for any referenced certificates or secret files. Ensure the named volume is writable by the image's nonroot user. Preserve the volume across restarts: it contains FIX sequence state and replay history. Do not delete or reset it without coordinating session recovery with peers. Protect it as sensitive test data.

Check startup with `docker logs fix-multiplexer`. After editing the mounted config, restart the container with `docker restart fix-multiplexer` to load changes. If environment variables or Docker mount/port options change, recreate the container with updated options while retaining the named volume.

## Network access and clients

The config determines the upstream destination and downstream listener. Docker's port mapping and the host firewall determine how clients reach the container.

| Direction | Network access |
|---|---|
| Gateway to counterparty | Allow outbound TCP to the configured `upstream.host:upstream.port`. The counterparty may need to allowlist the gateway's public source IP. |
| Developer to gateway | Allow authorized clients inbound TCP to the Docker host's published port, `9879` in the example. `-p 9879:9879` maps host port 9879 to container port 9879. |
| Management | Management binds to container loopback and is not published by the command above. |

`-p 9879:9879` publishes on host interfaces, subject to Docker networking and the host firewall. To allow only clients on the Docker host, use `-p 127.0.0.1:9879:9879`. Restrict network access to trusted developers with a firewall or VPN. If changing the port, update the config, Docker mapping, firewall rules and client settings consistently.

For developers connecting over the internet:

1. Give the server a reachable public IP address or DNS name. If it is behind a router, configure the required TCP port forwarding to the Docker host.
2. Set `downstream.listen` to `0.0.0.0:9879` in the container and run it with `-p 9879:9879`, as shown above.
3. Allow inbound TCP port 9879 through the server's firewall and any cloud firewall or security group. Restrict source addresses where practical. Allow the server outbound TCP to the configured counterparty host and port.
4. Give each developer the public address, port, gateway TargetCompID, and their own SenderCompID and password. Configure TLS on both sides if it is being used.

Public accessibility depends on the server and network configuration; starting the container alone does not open host or cloud firewall rules. This project has no per-IP login rate limiting and its security review is incomplete. Without TLS, FIX passwords and messages are exposed on the network path; use TLS or an encrypted VPN.

Each developer connects their FIX 4.4 client to the Docker host's reachable address and published port, with their own configured SenderCompID and password, and the gateway's TargetCompID. Do not reuse a session identity for simultaneous clients. Coordinate sequence numbers rather than automatically resetting them.

By default, logged-on clients receive allowed upstream broadcasts, including messages concerning other clients; per-client type filters can narrow delivery. Fanout is live-only: disconnected clients miss new broadcasts, although FIX resend can recover previously assigned messages still eligible in their own session store. A gateway error during an upstream outage is not a venue execution report or acceptance. For recovery and operational details, see [operations](OPERATIONS.md).

# Hosting Your Own StealthPipe Relay

Hosting a StealthPipe relay yourself can help reduce latency and improve stability.

**Quick Steps**

1. Run
```bash
docker run -d -p YOUR_PORT:YOUR_PORT \
  -e PORT=YOUR_PORT \
  -e SECRET_KEY=your_super_secret_key \
  --name stealth-relay 0999847695359/stealthpipe-relay:latest
  ```

2. Put your relay's URL into your StealthPipe's mod config in Mod Menu/Cloth Config and **every other player that wants to use your relay to join must also modify their own mod config** (Also remove the leading slash!)
3. You're done!

Replace YOUR_PORT with the port required by your service provider. (Default is 7860).

> [!IMPORTANT]
> **Very very important**: Do not forget to set your secret key to something super secret and secure!

### Security Features and config

The server includes some basic security features to prevent bots from abusing the relay. It includes:
- Proof of Work for creating a session
- Packet size limit and bandwidth throttling (most providers have a limited GB/month throughput)

The server config is in **src/core/Types.go** near the bottom. As of right now, you must create your own image if you wish to modify the config (requires recompiling).

## Hosting on Providers

On most providers, you *can* use the docker pull command directly (or use their fancy user interface to browse through the registry). *However*, on some other service providers, you *might* have to upload the files one by one by yourself. Just upload all the files, compile the source code and run the final binary.

StealthPipe relay is completely stateless, so it can be containerized efficiently and is really cheap to host.

> [!WARNING]
> You must host this on a server that is accessible via the Internet! If you host this in your own home, your friends might not be able to connect to it without port forwarding!

## Cool Places to Host

- Render -- Completley free, limited throughput, low-latency. Good for a few friends
- Oracle -- Provides free services, requires identity verification but the hardware is very powerful. High throughput. No domain provided by default.
- HuggingFace -- Free, but you must upload the files manually. Higher latency. No GB/Month limit. Hardware is more powerful compared to Render, although this varies. **Can be subject to moderation.**

Any other service provider you choose must support the following:
  - **HTTPS**
  - **Persistent WebSockets**
  - **Docker Images or custom executables**

## Environment Variables

StealthPipe relay has full support for Grafana. 

```
Supported environment variables:
- Set GRAFANA_METRICS to "true" to post grafana metrics using OTLP
- Set GRAFANA_KEY, GRAFANA_URL, GRAFANA_USER
- Set LIMITED_COMPUTE_MODE to "true" if needed
- Set SECRET_KEY to something super secret
- Set PRODUCTION to "true" in production (controls staging vs. prod)
- Set SERVICE_NAME to anything other than "" for a custom service name suffix in Grafana
```

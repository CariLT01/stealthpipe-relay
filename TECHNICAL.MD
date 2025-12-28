
# Technical overview of StealthPipe

Here's a nice *technical* and very *nerdy* overview of StealthPipe.

> [!NOTE]
> Technical overview may be outdated from time to time!

### A Quick Summary

StealthPipe creates parallel connections to the relay to optimize performance. It establishes one secure WebSocket connection (TLS/HTTPS) per player to the relay for sandboxing and stability, relaying all binary traffic to the other client. It does not (and cannot) discover what is inside each packet, since it only forwards the raw binary and does not manipulate packets. Also, by default, Minecraft traffic is end-to-end encrypted (unless using offline mode), which means StealthPipe cannot decode the traffic anyway.


### Why is the name of this mod StealthPipe?

Let's start with the obvious. Why "StealthPipe"?

- **Stealth**: chosen because it blends in with network traffic, essentially acting as stealthily. However, in most environments where non-standard ports and P2P connections are blocked, they are doing it for security reasons (prevent SSH or other attacks), not to block Minecraft. Read the other sections down below to learn more.
 - **Pipe**: it essentially means "tunnel", forwarding Minecraft data to a relay directly to another client.

### Clarifying the Description of StealthPipe

In the description, it says "can work on networks that block peer-to-peer connections and non-standard ports". What does this actually mean?

- **Works on networks blocking peer-to-peer connections**: Minecraft Open to LAN requires the other client to connect to the host by using the IP address on the current network. However, in many public WiFis, peer isolation is used, which prevents devices from seeing each other on the same network. Why is this used? To prevent viruses from spreading from one device to another on the same network (lateral movement). To fix this, StealthPipe relays all Minecraft traffic to a relay so devices don't need to see each other to play together.
- **Works on networks that block non-standard ports**: Many public WiFis block ports that are considered non-standard to prevent malware and viruses from entering from those ports. Minecraft's allowed ports (25565) are all considered non-standard ports, which is why it is blocked on many public networks even when the purpose is not to block online gaming. To fix this, StealthPipe relays traffic using WebSockets (port 443) which is considered a standard port, since all of web technology runs on it. It's the same technology that Microsoft Word, Snapchat, Discord, Teams, Excel, Powerpoint, and many others use.



### Broad overview of network handshake

```

  Overview of the Network Handshake phases
  ┌─────────────────────────────────────────────────────┐
  │ [HOST]                  [RELAY]           [CLIENT]  │
  ├────────────────────────────┬────────────────────────┤
  │                            │                        │
  │ 1. PING wakeup ───────────>│                        │
  │ 2. PoW Req (salt) ────────>│                        │
  │<────────── 3. Nonce & diff.│                        │
  │ 4. CREATE ROOM ───────────>│                        │
  │<<<<<< 5. OPEN SIGNAL >>>>>>│                        │
  │                            │                        │
  │               [ Client joins the room ]             │
  │                            │<──── 6. JOIN (Room ID) │
  │<─────── 7. SIGNAL REQ CONN │                        │
  │ 8. JOIN with ID ──────────>│                        │
  │                            │                        │
  │            [THREADS LINKED, TUNNEL FORMED]          │
  │<=======================DATA STREAM=================>│
  └─────────────────────────────────────────────────────┘
```

**Summary of each step**

1. **PING wakeup**: wake up the Render instance and get the round-trip time between the host and the relay.
2. **PoW Req (salt)**: request a proof of work salt and token from the relay.
3. **Nonce and difficulty**: Server returns the nonce, the difficulty and a validation token that will be used when creating the room.
4. **Create room**: Using the nonce computed by the host and the proof of work validation token, the host can request the relay to create a new room.
5. **Open SIGNAL**: Host and relay open a SIGNAL socket to communicate information such as new players trying to connect
6. **Join with room ID**: Another client requests the relay to join a room ID
7. **SIGNAL REQ CONN**: Relay forwards this request to the host by sending a REQUESTCONNECTION_[ROOM ID]_[REQUEST ID] where the host has to use this ID to create a DATA connection between the host and the relay.
8. **Join with request ID**: Host creates a websocket connection between the host and relay. Relay links the incoming client DATA connection to the incoming host DATA connection with the ID.
9. **THREADS LINKED**": Threads are successfully linked together and a tunnel is formed.

### Overview of Networking

StealthPipe's networking model is different from other similar mods, such as e4mc. 

In these diagrams, **DATA** refers to sockets that send Minecraft packets, and **SIGNAL** refers to sockets that send signals needed for state management (such as detecting a new player trying to connect). However, in other implementations, DATA  is used as a signaling socket AND as a data socket.

In StealthPipe, the DATA sockets are used strictly for Minecraft packets only, and the SIGNAL soocket is used for relay to host signaling only.

I have illustrated both models here:

```
Traditional Networking Model of Other Similar Mods

Host ───────────────────── Relay ──────────────────── Clients

                         ┌───────┐
                         │       │ <─── DATA ────> [ Client 1 ]
┌──────┐                 │ RELAY │ <─── DATA ────> [ Client 2 ]
│ HOST │ <─── DATA ────> │       │ <─── DATA ────> [ Client 3 ]
└──────┘                 │       │ <─── DATA ────> [ Client 4 ]
                         └───────┘

* May differ from mod to mod. Some mods use P2P instead, which doesn't need a relay.
```

Here's the networking model used in StealthPipe (version 3.0.0 and higher):

```

StealthPipe Networking Model (Parallel Connections >=v3.0.0)

Host ───────────────────── Relay ──────────────────── Clients

┌──────┐                 ┌───────┐
│      │ <─── DATA ────> │       │ <─── DATA ────> [ Client 1 ]
│      │ <─── DATA ────> │       │ <─── DATA ────> [ Client 2 ]
│ HOST │ <─── DATA ────> │ RELAY │ <─── DATA ────> [ Client 3 ]
│      │ <─── DATA ────> │       │ <─── DATA ────> [ Client 4 ]
│      │ <== SIGNAL ===> │       │
└──────┘                 └───────┘

```

This allows the relay to process each client independently in multiple threads. Here's why this is likely more efficient:

Minecraft sends around 500 packets per second per player. If one player is connected to the room, 500 packets per second go through the DATA channel. However, if 10 players are connected, 5000 packets go through the DATA channel, which is significantly more for the relay to handle in a single thread. Also, WebSockets are hardly thread-safe, so all the clients sending 50 packets/second must also compete for a single lock to write to that single DATA socket going to the relay. Finally, when the HOST sends a message, it also needs to know WHERE to send this packet, which most likely requires extra parsing and header to determine which socket should receive this data.

In this architecture, the SIGNAL is used to notify the host when a new player joins, and asks it to create a DATA connection to the relay. Then, we link the host's DATA connection to the client's DATA connection. This means that even if we have 10 players sending 500 packets each, a single thread won't be handling them all, since each client has a corresponding host -> relay connection and a relay -> client connection, allowing each thread to only handle 500 packets each, improving performance. Also, there is no need for extra metadata, parsing or header since each thread already knows where to forward the packets.

In the traditional single-connection model, if one player experiences a packet loss, the TCP window stalls for all players (Head-of-Line blocking). StealthPipe V3's parallel architecture ensures that one player's lag does not affect anyone else in the room.



### Handshake Protocol Questions

I know nothing's in order here, but yeah.

**Why Proof of Work?**
Since StealthPipe does not require any authentication, and the computational resources are rather limited, we require a proof of work to prevent bots or scripts from easily exceeding our computational budget. By requiring proof of work, the clients must "pay" by doing work before they can create a new room, slowing down scripts and bots tremendously.

Here's how Proof of Work protects our relay:
**Before**: A Python script that is 50 lines long could easily create 1 million connections in a few minutes.
**After**: The same Python script would take multiple hours to create 200 connections, let alone 1 million. They must optimize their code using multi-threading, but the dynamic difficulty system makes it harder the more requests you send.

**How is Proof of Work done?**

It is done by finding a SHA-256 hash using the relay's salt with an X number of zeroes, where X is the difficulty given by the relay. Once it finds one, it reports the nonce to the relay so it can validate it and allow the client to create a room. The difficulty increases depending on the number of create requests received per second, essentially throttling traffic while not using IP addresses (which are sometimes not accurate enough to distinguish between different users). PoW terminates immediately after room creation. There is no background mining.
Source can be found in `RelayConnector.java` in `checkPoW` and `doProofOfWorkChallenge` methods.

**Why not just use IP addresses?**

IP-based limiting is ineffective in airports or hotels where 500 people may share a single Public IP. PoW allows us to rate-limit per-device without needing accounts or tracking user data.

### Data Encapsulation & Relaying Model

A simple diagram showing how data is relayed (nothing special here):

```

┌─────────────────────────────────────────────────┐
│  HOST               RELAY               CLIENT  │
├───────────────────────┬─────────────────────────┤
│                       │                         │
│─── Encrypted data ───>│                         │
│      [Payload]        │─── Encrypted data ─────>│
│                       │       [Payload]         │
│                       │                         │
│                       │<─── Encrypted data ─────│
│<── Encrypted data ────│       [Payload]         │
│      [Payload]        │                         │
└───────────────────────┴─────────────────────────┘

* Data is only encrypted by Minecraft if the client is using Online mode. 
  But StealthPipe does not attempt to read, decrypt or decode the payload
  wether it is encrypted or not.
```

Most tunneling mods that rely on relays use this model.

### Hooking into the Netty Pipeline

Each connection (a real one) has a Netty Channel associated with it. It is basically an abstraction of an underlying network socket, to put it simply. And each channel has a pipeline, a series of steps that a packet must go through to get serialized. To make StealthPipe work, it injects a custom `DuplexHandler` at the end of the pipeline to intercept the binary data before it proceeds to send it. Later, you'll also learn that we make use of Embedded Channels (basically fake channels) to trick Minecraft into think that it is a real network connection and route data through it. Embedded Channels are meant for testing, but for StealthPipe, they are used to pipe Minecraft packets to the relay.

```

Sending a packet:

   [ External class ]
           │
           │                                         
           V                                             
Netty: [ Beginning ] ─────> [...] ─────> [ StealthPipe ] ─────> Sent
      ( Packet class )                      ( Binary )          nowhere
                                                │
                                                │
                                                V
                                           Sent to Relay
                                              via WSS       

* [...] represents the series of steps to get the Packet class serialized, such as
  encryption, packet encoding, and compression. So by the time StealthPipe gets
  the binary, it is already encrypted by Minecraft, which means the data is
  unreadable by StealthPipe.

```

StealthPipe injects its own `DuplexHandler` at the end of the pipeline to intercept the binary data and send it off to the relay. At that point, the data is already fully serialized, encrypted and ready to be sent off. StealthPipe clones the data and sends it to the relay. The original pipeline continues to its conclusion, but because the channel is 'fake' (Embedded), the data is simply discarded by the system after leaving the pipeline. Why fake channels? Because without them, Minecraft refuses to accept the connection or initiate its handshake.

Receiving is basically the same thing, except the steps are reversed and StealthPipe injects data at the first step instead of intercepting at the end.

On the server, one new fake channel is created per player, tricking Minecraft into believing they are actually connected to your LAN world. On the client, we trick Minecraft to use our fake `Connection` object which internally also uses a fake channel.
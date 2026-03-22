<div align="center"><center>

# StealthPipe Relay

![LoC](https://img.shields.io/endpoint?url=https://ghloc.vercel.app/api/CariLT01/stealthpipe-relay/badge?style=flat&logoColor=white&color=c78aff&style=for-the-badge&v=1)

[StealthPipe Mod Repository](https://github.com/CariLT01/stealthpipe-mod)

</center></div>

**This is the relay-side code for the StealthPipe mod**. It forwards traffic from one client to another, manages routing and room creation. It also manages signaling for WebRTC ICE negotiation.

To host the relay, see [HOSTING.md](https://github.com/CariLT01/stealthpipe-relay/blob/main/HOSTING.md).

To see full technical details, see [TECHNICAL.md](https://github.com/CariLT01/stealthpipe-relay/blob/main/TECHNICAL.md).

### Performance

The StealthPipe relay is small, portable, and efficient. It is able to utilize multiple cores simultaneously and it only uses up to 64 MB of memory (it normally only uses up to 7 MB of RAM). It's a low overhead relay and can be easily hosted on any environment.

### Bugs and Issues

Please report them in the Issue Tracker. If the issue is not related to the relay, but instead is related to the mod, please report them in the [StealthPipe Mod Repository](https://github.com/CariLT01/stealthpipe-mod).

### Frequently Asked Questions

**High latency or ping**  
Latency depends on your location and the server's load. To improve performance, consider hosting your own relay or using a third-party relay. Choosing a server closer to your location may help reduce ping.

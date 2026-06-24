---
id: arbitrary-code-from-network
description: Executing unverified network-sourced code without integrity check.
anticipated-siblings: []
---

# Pattern: arbitrary-code-from-network

`curl ... | sh`, `bash <(curl ...)`, downloading an installer and running it without a checksum - all examples of trusting whatever bytes the network delivers, in shell context, with the same privileges as the user. Compromise of the source server, MITM on the connection, DNS hijack - any of these turns a routine "install this tool" into RCE.

The structural defense: pin a hash or signature when the source is trusted but the channel is not; refuse the unverified-pipe pattern in committed scripts and Dockerfiles. The alternative isn't avoiding network tools - it's binding the bytes to a known identity before executing them.

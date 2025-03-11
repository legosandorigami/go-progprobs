# go-progprobs
A series of interesting Go programming challenges. This repository contains my solutions to the [go-progprobs challenges](https://github.com/arschles/go-progprobs). 

* [x] [Image server](./imgserv): Challenge focusing on implementing an HTTP server that dynamically generates PNG and JPEG images of specified dimensions and tracks generation statistics.
* [x] [Mini database](./minidb): Challenge focusing on building an in-memory key/value store with HTTP endpoints that support lock reservations and controlled updates.
* [x] [Append-only file compaction](./aof): Challenge focusing on processing an append-only file by compacting sequential operations on keys into a streamlined set of commands.
* [x] [LRU Cache](./lrucache): Challenge focusing on implementing an in-memory LRU cache with fixed capacity and TTL-based eviction. Two solutions are provided:
    * One that uses locks for synchronization.
    * Another that leverages the Actor-Model for concurrency control.
* [x] [Peer to peer chatter](./peer-to-peer-chatter): Challenge focusing on creating a peer-to-peer messaging application that connects multiple processes and manages message forwarding using control commands. This challenge involved implementing a handshake mechanism to resolve duplicate connections based on unique IDs to ensure that each pair of servers only maintains a single connection between them. 

#### Aditional challenge
* [x] [ratelimiters](./ratelimiters): Challenge focusing on implementing four different rate-limiting algorithms in Go: Token Bucket, Leaky Bucket, Fixed Window, and Sliding Window.

## Contributions
I would greatly appreciate your suggestions and feedback for improving these solutions.

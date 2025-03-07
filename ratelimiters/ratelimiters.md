# ratelimiters

Rate limiters are essential for controlling the amount of incoming requests to a service. This project implements the following rate-limiting algorithms in Go: Token Bucket, Leaky Bucket, Fixed Window, and Sliding Window algorithms.

## Algorithms

### Token Bucket

The Token Bucket algorithm allows a certain number of tokens to be accumulated. Tokens are added at a specified rate, and requests can be fulfilled if there are enough tokens available.

### Leaky Bucket

The Leaky Bucket algorithm allows requests to be processed at a steady rate. Tokens leak out of the bucket at a defined rate, and if the bucket is full, incoming requests are denied.

### Fixed Window

The Fixed Window algorithm allows a fixed number of requests in a specified time frame. After the time window expires, the count resets.

### Sliding Window

The Sliding Window algorithm keeps track of the timestamps of requests within a given time frame, allowing for a more flexible rate limiting.
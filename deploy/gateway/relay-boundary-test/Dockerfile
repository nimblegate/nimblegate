# Throwaway image for the relay privilege-boundary test. git + bash only; the
# nimblegate binary and the test script are provided by run.sh. Not a production
# artifact - see boundary-test.sh.
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*

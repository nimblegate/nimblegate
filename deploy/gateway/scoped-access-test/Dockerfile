# Throwaway image for the scoped-access boundary test. git + openssh-client
# (ssh-keygen) only; the nimblegate binary + test script are provided by run.sh.
# Not a production artifact - see scoped-test.sh.
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends git openssh-client ca-certificates && rm -rf /var/lib/apt/lists/*

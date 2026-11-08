#!/usr/bin/env bash
# Negative: login and CLI config subcommands touch no environment.
gcloud auth activate-service-account --key-file="$GOOGLE_APPLICATION_CREDENTIALS"
gcloud config set project "$PROJECT_ID"
gcloud auth configure-docker -q
